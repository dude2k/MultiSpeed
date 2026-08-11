// Package config loads and validates MultiSpeed's environment configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/providers"
)

const (
	defaultListenAddress = "127.0.0.1:8787"
	defaultDataDirectory = "/data"
)

// Config contains process-level settings. User-adjustable settings live in SQLite.
type Config struct {
	ListenAddress           string
	TrustedHosts            []string
	AllowedCustomServerURLs []string
	DataDirectory           string
	DatabasePath            string
	LogLevel                slog.Level
	AcceptOoklaEULA         bool
	AllowOoklaBinaryUpload  bool
	OoklaBinary             string
	LibreSpeedBinary        string
	ShutdownTimeout         time.Duration
	Version                 string
	GitCommit               string
	BuildTime               string
}

// Load reads configuration from environment variables and applies safe defaults.
func Load(version, commit, buildTime string) (Config, error) {
	dataDir := filepath.Clean(env("APP_DATA_DIR", defaultDataDirectory))
	if !filepath.IsAbs(dataDir) || strings.ContainsAny(dataDir, "\x00\r\n") || dataDir == filepath.VolumeName(dataDir)+string(filepath.Separator) {
		return Config{}, errors.New("invalid APP_DATA_DIR: value must be an absolute non-root path")
	}
	listen := env("APP_LISTEN_ADDR", defaultListenAddress)
	if err := validateListenAddress(listen); err != nil {
		return Config{}, fmt.Errorf("invalid APP_LISTEN_ADDR: %w", err)
	}
	trustedHosts, err := parseTrustedHosts(os.Getenv("APP_TRUSTED_HOSTS"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid APP_TRUSTED_HOSTS: %w", err)
	}
	allowedCustomServerURLs, err := parseAllowedCustomServerURLs(os.Getenv("APP_ALLOWED_CUSTOM_SERVER_URLS"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid APP_ALLOWED_CUSTOM_SERVER_URLS: %w", err)
	}

	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(strings.ToUpper(env("APP_LOG_LEVEL", "INFO")))); err != nil {
		return Config{}, fmt.Errorf("invalid APP_LOG_LEVEL: %w", err)
	}

	shutdown, err := time.ParseDuration(env("APP_SHUTDOWN_TIMEOUT", "20s"))
	if err != nil || shutdown <= 0 || shutdown > 5*time.Minute {
		return Config{}, errors.New("invalid APP_SHUTDOWN_TIMEOUT: value must be between 1ns and 5m")
	}

	return Config{
		ListenAddress:           listen,
		TrustedHosts:            trustedHosts,
		AllowedCustomServerURLs: allowedCustomServerURLs,
		DataDirectory:           dataDir,
		DatabasePath:            filepath.Join(dataDir, "multispeed.db"),
		LogLevel:                level,
		AcceptOoklaEULA:         envBool("ACCEPT_OOKLA_EULA", false),
		AllowOoklaBinaryUpload:  envBool("APP_ALLOW_OOKLA_BINARY_UPLOAD", false),
		OoklaBinary:             env("OOKLA_BINARY", filepath.Join(dataDir, "providers", "ookla", "speedtest")),
		LibreSpeedBinary:        env("LIBRESPEED_BINARY", "librespeed-cli"),
		ShutdownTimeout:         shutdown,
		Version:                 version,
		GitCommit:               commit,
		BuildTime:               buildTime,
	}, nil
}

func validateListenAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be an integer between 1 and 65535")
	}
	if host == "" || net.ParseIP(host) != nil || validHostname(strings.ToLower(host)) {
		return nil
	}
	return errors.New("host must be a hostname or IP address")
}

func parseTrustedHosts(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	hosts := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		host := strings.ToLower(strings.TrimSpace(item))
		if host == "" {
			return nil, errors.New("entries must not be empty")
		}
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		} else if !validHostname(host) {
			return nil, fmt.Errorf("%q must be a hostname or IP address without a scheme, path, wildcard, or port", item)
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func parseAllowedCustomServerURLs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	entries := strings.Split(raw, ",")
	for index := range entries {
		entries[index] = strings.TrimSpace(entries[index])
		if entries[index] == "" {
			return nil, errors.New("entries must not be empty")
		}
	}
	policy, err := providers.NewCustomServerURLPolicy(entries)
	if err != nil {
		return nil, err
	}
	return policy.URLs(), nil
}

func validHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < '0' || character > '9') && (character < 'a' || character > 'z') {
				return false
			}
		}
	}
	return true
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}
