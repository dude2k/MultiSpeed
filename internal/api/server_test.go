package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceService(broker)
	_, _ = interfaces.Refresh(context.Background())
	return New(store, nil, nil, interfaces, network.NewRouteValidator(interfaces), providers.NewRegistry(), broker, logger, BuildInfo{Version: "test"},
		HTTPPolicy{ListenAddress: "127.0.0.1:8787", TrustedHosts: []string{"multispeed.local", "proxy.example.test"}}).Handler()
}
func TestHealthAndSecurityHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/healthz", nil)
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("missing security/request headers: %v", response.Header())
	}
}
func TestExactTaskURLHasNoTrailingSlashRequirement(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/tasks/missing", nil)
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestStaticFrontendServesRootAndDeepRoutesWithoutRedirect(t *testing.T) {
	handler := testHandler(t)
	for _, route := range []string{"/", "/index.html", "/tasks/example"} {
		t.Run(route, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://multispeed.local"+route, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
			}
			if location := response.Header().Get("Location"); location != "" {
				t.Fatalf("unexpected redirect location %q", location)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
				t.Fatalf("content type=%q", contentType)
			}
			if body := response.Body.String(); !strings.Contains(body, "<title>MultiSpeed</title>") {
				t.Fatalf("response did not contain the SPA entry point: %s", body)
			} else if !strings.Contains(body, `<script type="module"`) || strings.Contains(strings.ToLower(body), "placeholder") {
				t.Fatalf("response did not contain the production Vite entry point: %s", body)
			}
		})
	}
}

func TestStaticFrontendServesHashedViteAssets(t *testing.T) {
	handler := testHandler(t)
	indexRequest := httptest.NewRequest(http.MethodGet, "http://multispeed.local/", nil)
	indexResponse := httptest.NewRecorder()
	handler.ServeHTTP(indexResponse, indexRequest)

	assetPattern := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+\.(?:js|css))"`)
	matches := assetPattern.FindAllStringSubmatch(indexResponse.Body.String(), -1)
	if len(matches) < 2 {
		t.Fatalf("expected hashed JavaScript and CSS assets in index: %s", indexResponse.Body.String())
	}
	seen := map[string]bool{}
	for _, match := range matches {
		asset := match[1]
		if seen[asset] {
			continue
		}
		seen[asset] = true
		request := httptest.NewRequest(http.MethodGet, "http://multispeed.local"+asset, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("asset %q status=%d bytes=%d", asset, response.Code, response.Body.Len())
		}
		contentType := response.Header().Get("Content-Type")
		if !strings.Contains(contentType, "javascript") && !strings.Contains(contentType, "text/css") {
			t.Fatalf("asset %q content type=%q", asset, contentType)
		}
	}
}
func TestCrossOriginMutationRejectedBeforeHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodDelete, "http://multispeed.local/api/v1/results/missing", nil)
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUntrustedHostRejectedForReadsAndSameOriginMutations(t *testing.T) {
	handler := testHandler(t)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "http://evil.example:8787/api/v1/healthz", nil),
		httptest.NewRequest(http.MethodDelete, "http://evil.example:8787/api/v1/results/missing", nil),
	} {
		request.Header.Set("Origin", "http://evil.example:8787")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMisdirectedRequest {
			t.Fatalf("%s status=%d body=%s", request.Method, response.Code, response.Body.String())
		}
	}
}

func TestHostPolicyAllowsLoopbackAndExplicitProxyOnlyOnListenPort(t *testing.T) {
	handler := testHandler(t)
	for _, authority := range []string{"localhost:8787", "127.0.0.1:8787", "proxy.example.test", "proxy.example.test:8787"} {
		request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/healthz", nil)
		request.Host = authority
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("host %q status=%d body=%s", authority, response.Code, response.Body.String())
		}
	}
	for _, authority := range []string{"localhost:9999", "0.0.0.0:8787", "bad host:8787", "proxy.example.test:443"} {
		request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/healthz", nil)
		request.Host = authority
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMisdirectedRequest {
			t.Fatalf("host %q status=%d body=%s", authority, response.Code, response.Body.String())
		}
	}
}
func TestUnsupportedContentTypeRejectedOnlyForBody(t *testing.T) {
	handler := testHandler(t)
	bodyRequest := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/results/delete-batch", io.NopCloser(io.LimitReader(zeroReader{}, 2)))
	bodyRequest.ContentLength = 2
	bodyResponse := httptest.NewRecorder()
	handler.ServeHTTP(bodyResponse, bodyRequest)
	if bodyResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("body status=%d", bodyResponse.Code)
	}
	emptyRequest := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/backup", nil)
	emptyResponse := httptest.NewRecorder()
	handler.ServeHTTP(emptyResponse, emptyRequest)
	if emptyResponse.Code == http.StatusUnsupportedMediaType {
		t.Fatal("bodyless POST was rejected for content type")
	}
}

func TestJSONPContentTypeAndPersistedMetadataAreRejected(t *testing.T) {
	handler := testHandler(t)
	jsonp := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/results/delete-batch", strings.NewReader(`{"ids":["x"]}`))
	jsonp.Header.Set("Content-Type", "application/jsonp")
	jsonpResponse := httptest.NewRecorder()
	handler.ServeHTTP(jsonpResponse, jsonp)
	if jsonpResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("JSONP status=%d body=%s", jsonpResponse.Code, jsonpResponse.Body.String())
	}

	for _, endpoint := range []string{"/api/v1/tasks", "/api/v1/route-profiles"} {
		request := httptest.NewRequest(http.MethodPost, "http://multispeed.local"+endpoint, strings.NewReader(`{"id":"caller-controlled"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "unknown field") {
			t.Fatalf("%s status=%d body=%s", endpoint, response.Code, response.Body.String())
		}
	}
}

func TestUnknownAPIResourcesUseJSONErrorEnvelope(t *testing.T) {
	handler := testHandler(t)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/does-not-exist", nil),
		httptest.NewRequest(http.MethodPatch, "http://multispeed.local/api/v1/healthz", nil),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("content type=%q body=%s", contentType, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"requestId"`) {
			t.Fatalf("missing error envelope: %s", response.Body.String())
		}
	}
}

type zeroReader struct{}

func (zeroReader) Read(value []byte) (int, error) {
	for i := range value {
		value[i] = 0
	}
	return len(value), nil
}
