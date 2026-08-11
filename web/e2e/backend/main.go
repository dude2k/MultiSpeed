// Command multispeed-e2e runs the production API, persistence, scheduler,
// execution pipeline, and provider adapters with a deterministic route
// validator. It is intentionally scoped to browser tests and never performs a
// public speed test or public-IP lookup.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/dude2k/MultiSpeed/internal/api"
	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/execution"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/dude2k/MultiSpeed/internal/providers/cloudflare"
	"github.com/dude2k/MultiSpeed/internal/providers/librespeed"
	"github.com/dude2k/MultiSpeed/internal/providers/ookla"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
	"github.com/dude2k/MultiSpeed/internal/scheduler"
)

const testVersion = "e2e-test"

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "multispeed-e2e: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("the MultiSpeed runtime is Linux-only; current OS is %s", runtime.GOOS)
	}
	listenAddress := env("APP_LISTEN_ADDR", "127.0.0.1:18787")
	if _, _, err := net.SplitHostPort(listenAddress); err != nil {
		return fmt.Errorf("invalid APP_LISTEN_ADDR: %w", err)
	}
	dataDirectory := strings.TrimSpace(os.Getenv("APP_DATA_DIR"))
	if dataDirectory == "" {
		return errors.New("APP_DATA_DIR is required for the isolated e2e backend")
	}
	if err := os.MkdirAll(dataDirectory, 0o750); err != nil {
		return fmt.Errorf("create e2e data directory: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	rootContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	store, err := database.Open(rootContext, filepath.Join(dataDirectory, "multispeed.db"), logger)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	if err := store.CheckIntegrity(rootContext); err != nil {
		return err
	}

	broker := events.New()
	defer broker.Close()
	interfaceService := network.NewInterfaceServiceWithDiscoverer(broker, simulatedWANInterfaces)
	if _, err := interfaceService.Refresh(rootContext); err != nil {
		return err
	}
	processRunner := providerprocess.ExecRunner{}
	registry := providers.NewRegistry(
		ookla.NewWithAcceptanceSourceAndRuntimeDirectory(env("OOKLA_BINARY", "speedtest"), store.OoklaEULAAcceptance, filepath.Join(dataDirectory, "providers", "ookla", "runtime"), processRunner),
		librespeed.New(env("LIBRESPEED_BINARY", "librespeed-cli"), processRunner),
		cloudflare.New(),
	)
	executionManager := execution.New(store, registry, interfaceService, deterministicRouteValidator{}, broker, logger, testVersion)
	executionManager.Start()
	schedule := scheduler.New(store, executionManager, broker, logger)
	if err := schedule.Start(rootContext); err != nil {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = executionManager.Stop(shutdownContext)
		return err
	}

	apiServer := api.New(store, schedule, executionManager, interfaceService, deterministicRouteValidator{}, registry, broker, logger, api.BuildInfo{
		Version: testVersion, GitCommit: "deterministic-fixture", BuildTime: "1970-01-01T00:00:00Z",
	}, api.HTTPPolicy{ListenAddress: listenAddress})
	httpServer := &http.Server{
		Addr: listenAddress, Handler: apiServer.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 32 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("e2e HTTP server listening", "address", listenAddress)
		serveErrors <- httpServer.ListenAndServe()
	}()

	var serveErr error
	select {
	case <-rootContext.Done():
	case serveErr = <-serveErrors:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	return errors.Join(serveErr, httpServer.Shutdown(shutdownContext), schedule.Stop(shutdownContext), executionManager.Stop(shutdownContext))
}

func simulatedWANInterfaces(ctx context.Context) ([]models.NetworkInterface, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	definitions := []struct{ name, address, mac string }{
		{"wan-fiber", "192.0.2.10", "02:00:00:00:00:10"},
		{"wan-cable", "198.51.100.10", "02:00:00:00:00:20"},
		{"wan-lte", "203.0.113.10", "02:00:00:00:00:30"},
	}
	items := make([]models.NetworkInterface, 0, len(definitions))
	for index, definition := range definitions {
		items = append(items, models.NetworkInterface{Name: definition.name, Index: index + 10, Operational: true,
			OperationalState: "up", MACAddress: definition.mac, MTU: 1500,
			Addresses: []models.InterfaceAddress{{Address: definition.address, Family: "ipv4"}}})
	}
	return items, nil
}

type deterministicRouteValidator struct{}

func (deterministicRouteValidator) Validate(ctx context.Context, profile models.RouteProfile) models.RouteValidation {
	validatedAt := time.Now().UTC()
	if err := ctx.Err(); err != nil {
		return models.RouteValidation{InterfaceName: profile.InterfaceName, SourceIP: profile.SourceIP, Destination: profile.ValidationTarget, ValidatedAt: validatedAt, Message: err.Error()}
	}
	destination := "1.1.1.1"
	if source := net.ParseIP(profile.SourceIP); source != nil && source.To4() == nil {
		destination = "2606:4700:4700::1111"
	}
	return models.RouteValidation{
		Success: true, InterfaceName: profile.InterfaceName, SourceIP: profile.SourceIP, Gateway: "192.0.2.1", RoutingTable: "main",
		Destination: destination, DetectedPublicIP: "203.0.113.44", Reachable: true, DurationMS: 1, ValidatedAt: validatedAt,
		Message: "deterministic e2e route fixture approved the selected source path",
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
