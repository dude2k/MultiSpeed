package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/dude2k/MultiSpeed/internal/api"
	"github.com/dude2k/MultiSpeed/internal/config"
	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/execution"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/dude2k/MultiSpeed/internal/providers/cloudflare"
	"github.com/dude2k/MultiSpeed/internal/providers/librespeed"
	"github.com/dude2k/MultiSpeed/internal/providers/ookla"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
	"github.com/dude2k/MultiSpeed/internal/retention"
	"github.com/dude2k/MultiSpeed/internal/scheduler"
	"github.com/robfig/cron/v3"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "multispeed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configuration, err := config.Load(version, gitCommit, buildTime)
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			return healthcheck(configuration.ListenAddress)
		case "version":
			fmt.Printf("MultiSpeed %s (%s, %s)\n", version, gitCommit, buildTime)
			return nil
		default:
			return fmt.Errorf("unknown command %q", os.Args[1])
		}
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("MultiSpeed has a Linux-only runtime; current OS is %s", runtime.GOOS)
	}

	level := &slog.LevelVar{}
	level.Set(configuration.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	logger.Info("starting MultiSpeed", "version", version, "git_commit", gitCommit, "build_time", buildTime, "listen_address", configuration.ListenAddress)
	if strings.HasPrefix(configuration.ListenAddress, "0.0.0.0:") || strings.HasPrefix(configuration.ListenAddress, "[::]:") {
		logger.Warn("MultiSpeed has no authentication; expose this listener only to a trusted network or behind an authenticating reverse proxy")
	}

	rootContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	store, err := database.Open(rootContext, configuration.DatabasePath, logger)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			logger.Error("close database", "error", closeErr)
		}
	}()
	if err := store.CheckIntegrity(rootContext); err != nil {
		return err
	}
	if count, err := store.RecoverInterruptedResults(rootContext); err != nil {
		return fmt.Errorf("recover interrupted results: %w", err)
	} else if count > 0 {
		logger.Warn("recovered interrupted executions", "count", count)
	}

	broker := events.New()
	defer broker.Close()
	interfaceService := network.NewInterfaceService(broker)
	if _, err := interfaceService.Refresh(rootContext); err != nil {
		return err
	}
	routeValidator := network.NewRouteValidator(interfaceService)
	processRunner := providerprocess.ExecRunner{}
	customServerPolicy, err := providers.NewCustomServerURLPolicy(configuration.AllowedCustomServerURLs)
	if err != nil {
		return fmt.Errorf("custom server URL policy: %w", err)
	}
	registry := providers.NewRegistry(
		ookla.NewWithAcceptanceSourceAndRuntimeDirectory(configuration.OoklaBinary, func(ctx context.Context) (bool, error) {
			if configuration.AcceptOoklaEULA {
				return true, nil
			}
			return store.OoklaEULAAcceptance(ctx)
		}, filepath.Join(configuration.DataDirectory, "providers", "ookla", "runtime"), processRunner),
		librespeed.NewWithCustomServerURLPolicy(configuration.LibreSpeedBinary, processRunner, customServerPolicy),
		cloudflare.New(),
	)
	executionManager := execution.New(store, registry, interfaceService, routeValidator, broker, logger, version)
	executionManager.Start()
	schedule := scheduler.New(store, executionManager, broker, logger)
	if err := schedule.Start(rootContext); err != nil {
		return err
	}

	backgroundContext, cancelBackground := context.WithCancel(rootContext)
	defer cancelBackground()
	go refreshInterfaces(backgroundContext, store, interfaceService, logger)
	cleaner, err := retention.New(store, logger, retention.Config{BatchSize: 500, MaxBatches: 100, RunMaintenance: true})
	if err != nil {
		return err
	}
	go runRetention(backgroundContext, store, cleaner, logger)

	apiServer := api.New(store, schedule, executionManager, interfaceService, routeValidator, registry, broker, logger,
		api.BuildInfo{Version: version, GitCommit: gitCommit, BuildTime: buildTime},
		api.HTTPPolicy{ListenAddress: configuration.ListenAddress, TrustedHosts: configuration.TrustedHosts,
			OoklaEULAEnvironmentAccepted: configuration.AcceptOoklaEULA, DataDirectory: configuration.DataDirectory,
			OoklaBinaryPath: configuration.OoklaBinary, AllowOoklaBinaryUpload: configuration.AllowOoklaBinaryUpload,
			MetricsEnabled: configuration.MetricsEnabled})
	httpServer := &http.Server{Addr: configuration.ListenAddress, Handler: apiServer.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 32 << 10}
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "address", configuration.ListenAddress)
		serveErrors <- httpServer.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
		logger.Info("shutdown signal received")
	case serveErr := <-serveErrors:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server: %w", serveErr)
		}
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), configuration.ShutdownTimeout)
	defer cancelShutdown()
	cancelBackground()
	schedulerErr := schedule.Stop(shutdownContext)
	broker.Close()
	shutdownErrors := make(chan error, 2)
	go func() { shutdownErrors <- httpServer.Shutdown(shutdownContext) }()
	go func() { shutdownErrors <- executionManager.Stop(shutdownContext) }()
	return errors.Join(schedulerErr, <-shutdownErrors, <-shutdownErrors)
}

func runRetention(ctx context.Context, store *database.Store, cleaner *retention.Cleaner, logger *slog.Logger) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	var lastLocalKey string
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			settings, err := store.GetSettings(ctx)
			if err != nil {
				logger.Warn("load retention settings", "error", err)
				continue
			}
			location, err := time.LoadLocation(settings.DefaultTimezone)
			if err != nil {
				logger.Warn("load retention timezone", "timezone", settings.DefaultTimezone, "error", err)
				continue
			}
			schedule, err := parser.Parse(settings.DatabaseMaintenanceSchedule)
			if err != nil {
				logger.Warn("parse database maintenance schedule", "schedule", settings.DatabaseMaintenanceSchedule, "error", err)
				continue
			}
			localNow := now.In(location)
			due := schedule.Next(localNow.Add(-61 * time.Second))
			if due.After(localNow) {
				continue
			}
			localKey := due.Format("2006-01-02T15:04 MST")
			if localKey == lastLocalKey {
				continue
			}
			lastLocalKey = localKey
			policy := retention.Policy{Mode: retention.Mode(settings.RetentionMode), Value: settings.RetentionValue}
			cleanupContext, cancel := context.WithTimeout(ctx, 15*time.Minute)
			_, err = cleaner.Run(cleanupContext, policy, now.UTC())
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.Error("scheduled retention failed", "error", err)
			}
		}
	}
}

func healthcheck(listenAddress string) error {
	host, port, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	endpoint := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/api/v1/healthz"}
	client := &http.Client{Timeout: 4 * time.Second}
	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

func refreshInterfaces(ctx context.Context, store *database.Store, service *network.InterfaceService, logger *slog.Logger) {
	for {
		settings, err := store.GetSettings(ctx)
		interval := 30 * time.Second
		if err == nil && settings.InterfaceRefreshInterval >= 5 {
			interval = time.Duration(settings.InterfaceRefreshInterval) * time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if _, err := service.Refresh(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("interface refresh failed", "error", err)
			}
		}
	}
}
