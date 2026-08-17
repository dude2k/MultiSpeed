package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
)

func TestPrometheusMetricsAreExplicitlyOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			handler := metricsTestHandler(t, enabled)
			if enabled {
				healthRequest := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/healthz", nil)
				healthResponse := httptest.NewRecorder()
				handler.ServeHTTP(healthResponse, healthRequest)
				if healthResponse.Code != http.StatusOK {
					t.Fatalf("health status=%d", healthResponse.Code)
				}
			}

			request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/metrics", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if !enabled {
				if response.Code != http.StatusNotFound {
					t.Fatalf("disabled metrics status=%d body=%s", response.Code, response.Body.String())
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("enabled metrics status=%d body=%s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			for _, metric := range []string{
				"multispeed_tasks 0",
				"multispeed_results 0",
				"multispeed_active_runs 0",
				`multispeed_http_requests_total{method="GET",route="/api/v1/healthz",status="200"} 1`,
			} {
				if !strings.Contains(body, metric) {
					t.Errorf("metrics response does not contain %q", metric)
				}
			}
		})
	}
}

func metricsTestHandler(t *testing.T, enabled bool) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "metrics.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceService(broker)
	_, _ = interfaces.Refresh(context.Background())
	return New(store, nil, nil, interfaces, network.NewRouteValidator(interfaces), providers.NewRegistry(), broker, logger, BuildInfo{Version: "test"}, HTTPPolicy{
		ListenAddress:  "127.0.0.1:8787",
		TrustedHosts:   []string{"multispeed.local"},
		MetricsEnabled: enabled,
	}).Handler()
}
