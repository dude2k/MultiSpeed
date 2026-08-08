package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/dude2k/MultiSpeed/internal/providers/cloudflare"
	"github.com/dude2k/MultiSpeed/internal/scheduler"
)

func TestTaskCreationUsesPersistedDefaultsAndUpdatePreservesThem(t *testing.T) {
	handler, store := taskDefaultsTestHandler(t)
	ctx := context.Background()
	settings, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.DefaultTimezone = "Europe/Berlin"
	settings.DefaultTaskTimeout = 77
	if err := store.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}

	created := performTaskMutation(t, handler, http.MethodPost, "/api/v1/tasks", map[string]any{
		"name": "Persisted defaults", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10",
	}, http.StatusCreated)
	if created.Timezone != "Europe/Berlin" || created.TimeoutSeconds != 77 {
		t.Fatalf("persisted defaults were not applied: timezone=%q timeout=%d", created.Timezone, created.TimeoutSeconds)
	}
	if created.CronExpression != "0 * * * *" || created.ServerSelectionMode != "automatic" || created.IPFamily != "auto" ||
		created.RouteValidation != "required" || !created.PreventOverlap {
		t.Fatalf("declared task defaults were not applied: %+v", created)
	}

	settings.DefaultTimezone = "Asia/Tokyo"
	settings.DefaultTaskTimeout = 900
	if err := store.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	updatePath := "/api/v1/tasks/" + created.ID
	preserved := performTaskMutation(t, handler, http.MethodPut, updatePath, map[string]any{
		"name": "Persisted defaults", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10",
	}, http.StatusOK)
	if preserved.Timezone != "Europe/Berlin" || preserved.TimeoutSeconds != 77 {
		t.Fatalf("omitted update fields did not preserve the task values: timezone=%q timeout=%d", preserved.Timezone, preserved.TimeoutSeconds)
	}

	explicit := performTaskMutation(t, handler, http.MethodPut, updatePath, map[string]any{
		"name": "Persisted defaults", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10",
		"timezone": "America/New_York", "timeoutSeconds": 45,
	}, http.StatusOK)
	if explicit.Timezone != "America/New_York" || explicit.TimeoutSeconds != 45 {
		t.Fatalf("explicit update values were overwritten: timezone=%q timeout=%d", explicit.Timezone, explicit.TimeoutSeconds)
	}
}

func TestValidateTaskFieldsCanonicalizesEquivalentIPv6SourceAddresses(t *testing.T) {
	task := models.Task{
		Name: "Canonical IPv6", InterfaceName: "eth0", SourceIP: "2001:0db8:0:0:0:0:0:1",
		IPFamily: "ipv6", TimeoutSeconds: 30, RouteValidation: "required",
	}
	ip, err := validateTaskFields(&task)
	if err != nil {
		t.Fatal(err)
	}
	if ip.String() != "2001:db8::1" || task.SourceIP != "2001:db8::1" {
		t.Fatalf("parsed source=%q persisted source=%q, want canonical IPv6", ip, task.SourceIP)
	}
}

func TestTaskInputMergePreservesEveryOptionalUpdateField(t *testing.T) {
	routeID := "route-existing"
	existing := models.Task{
		Description: "keep description", CronExpression: "17 */6 * * *", Timezone: "Europe/Berlin",
		RandomJitterSeconds: 23, ServerSelectionMode: "custom", ServerID: "server-7", ServerURL: "https://speed.example.test",
		CustomServerDefinition: map[string]any{"download": "/down"}, IPFamily: "ipv6", RouteProfileID: &routeID,
		TimeoutSeconds: 321, ProviderOptions: map[string]any{"secure": true}, PreventOverlap: false, RouteValidation: "interface-only",
	}
	merged := (taskInput{}).model(&existing)
	if merged.Description != existing.Description || merged.CronExpression != existing.CronExpression || merged.Timezone != existing.Timezone ||
		merged.RandomJitterSeconds != existing.RandomJitterSeconds || merged.ServerSelectionMode != existing.ServerSelectionMode ||
		merged.ServerID != existing.ServerID || merged.ServerURL != existing.ServerURL || merged.IPFamily != existing.IPFamily ||
		merged.TimeoutSeconds != existing.TimeoutSeconds || merged.PreventOverlap != existing.PreventOverlap || merged.RouteValidation != existing.RouteValidation ||
		merged.RouteProfileID == nil || *merged.RouteProfileID != routeID || merged.CustomServerDefinition["download"] != "/down" || merged.ProviderOptions["secure"] != true {
		t.Fatalf("optional update fields were not preserved: %+v", merged)
	}

	var clearRoute taskInput
	if err := json.Unmarshal([]byte(`{"routeProfileId":null}`), &clearRoute); err != nil {
		t.Fatal(err)
	}
	if cleared := clearRoute.model(&existing); cleared.RouteProfileID != nil {
		t.Fatalf("explicit null did not clear route profile: %+v", cleared.RouteProfileID)
	}
}

func TestTransientTaskPreflightValidatesWithoutPersistence(t *testing.T) {
	validator := &recordingRouteValidator{result: models.RouteValidation{Success: true, Reachable: true, DetectedPublicIP: "203.0.113.10", Message: "validated"}}
	handler, store := transientTaskTestHandler(t, validator)
	body := map[string]any{
		"name": "Unsaved preflight", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/tasks/validate", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if validator.calls != 2 || validator.profile.InterfaceName != "wan-test" || validator.profile.SourceIP != "192.0.2.10" || validator.profile.ValidationTarget != "1.1.1.1" {
		t.Fatalf("unexpected exact-path preflight: calls=%d profile=%+v", validator.calls, validator.profile)
	}
	tasks, err := store.ListTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("transient validation persisted %d task(s)", len(tasks))
	}
}

func TestTransientTaskPreflightIsStrictAndReturnsRouteFailure(t *testing.T) {
	validator := &recordingRouteValidator{results: []models.RouteValidation{
		{Success: true, Reachable: true, Message: "provider base route passed"},
		{Success: false, Message: "policy route mismatch"},
	}}
	handler, store := transientTaskTestHandler(t, validator)

	unknown := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/tasks/validate", bytes.NewBufferString(`{"name":"x","unknown":true}`))
	unknown.Header.Set("Content-Type", "application/json")
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest || validator.calls != 0 {
		t.Fatalf("unknown field status=%d calls=%d body=%s", unknownResponse.Code, validator.calls, unknownResponse.Body.String())
	}

	invalidTarget := map[string]any{
		"name": "Bad target", "enabled": false, "provider": "cloudflare", "interfaceName": "wan-test", "sourceIp": "192.0.2.10",
		"serverSelectionMode": "fixed", "serverId": "not-supported",
	}
	_ = performPreflight(t, handler, invalidTarget, http.StatusUnprocessableEntity)
	if validator.calls != 0 {
		t.Fatalf("route validation ran after provider target rejection: calls=%d", validator.calls)
	}

	valid := map[string]any{
		"name": "Route failure", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10",
	}
	validation := performPreflight(t, handler, valid, http.StatusUnprocessableEntity)
	if validation.Success || validation.Message != "policy route mismatch" || validator.calls != 2 {
		t.Fatalf("unexpected failed validation response: calls=%d validation=%+v", validator.calls, validation)
	}
	tasks, err := store.ListTasks(context.Background())
	if err != nil || len(tasks) != 0 {
		t.Fatalf("failed preflight changed persisted tasks: count=%d err=%v", len(tasks), err)
	}
}

func TestTaskPreflightConfirmsFixedServerForTransientAndSavedTasks(t *testing.T) {
	var discoveryCalls atomic.Int32
	provider := &providerStub{
		id:           models.ProviderCloudflare,
		capabilities: providers.Capabilities{FixedServerIDs: true, ServerDiscovery: true},
		list: func(_ context.Context, request providers.ServerListRequest) ([]providers.Server, error) {
			discoveryCalls.Add(1)
			if request.InterfaceName != "wan-test" || request.SourceIP != "192.0.2.10" || request.IPFamily != "ipv4" || request.Search != "42" {
				t.Fatalf("discovery request=%+v", request)
			}
			return []providers.Server{{ID: "42", Name: "Exact bound server"}}, nil
		},
	}
	validator := &recordingRouteValidator{result: models.RouteValidation{Success: true, Reachable: true, Message: "validated"}}
	handler, store := providerTaskTestHandler(t, validator, provider, "192.0.2.10")
	body := map[string]any{
		"name": "Bound fixed target", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10", "ipFamily": "ipv4",
		"serverSelectionMode": "fixed", "serverId": "42",
	}
	_ = performPreflight(t, handler, body, http.StatusOK)
	if discoveryCalls.Load() != 1 {
		t.Fatalf("transient discovery calls=%d", discoveryCalls.Load())
	}

	created := performTaskMutation(t, handler, http.MethodPost, "/api/v1/tasks", body, http.StatusCreated)
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/tasks/"+created.ID+"/validate", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("saved validation status=%d body=%s", response.Code, response.Body.String())
	}
	if discoveryCalls.Load() != 2 {
		t.Fatalf("saved discovery calls=%d", discoveryCalls.Load())
	}
	loaded, err := store.GetTask(context.Background(), created.ID)
	if err != nil || loaded.Enabled {
		t.Fatalf("saved draft changed during validation: task=%+v err=%v", loaded, err)
	}
}

func TestTaskPreflightCustomTargetDoesNotIssueAnHTTPProbe(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	provider := &providerStub{id: models.ProviderCloudflare, capabilities: providers.Capabilities{CustomServerURLs: true}}
	validator := &recordingRouteValidator{result: models.RouteValidation{Success: true, Reachable: true, Message: "validated"}}
	handler, _ := providerTaskTestHandler(t, validator, provider, "127.0.0.1")
	body := map[string]any{
		"name": "Custom TLS target", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "127.0.0.1", "ipFamily": "ipv4",
		"serverSelectionMode": "custom", "serverUrl": target.URL,
	}
	_ = performPreflight(t, handler, body, http.StatusOK)
	if requests.Load() != 0 {
		t.Fatalf("custom target preflight issued %d HTTP requests", requests.Load())
	}
}

func TestEnabledTaskPreflightFailureKeepsDisabledDraftUnscheduled(t *testing.T) {
	var availabilityCalls atomic.Int32
	provider := &providerStub{
		id: models.ProviderCloudflare,
		availability: func(context.Context) providers.Availability {
			availabilityCalls.Add(1)
			return providers.Availability{Available: false, Message: "Ookla CLI missing\x00; accept the EULA"}
		},
	}
	validator := &recordingRouteValidator{result: models.RouteValidation{Success: true}}
	handler, store := providerTaskTestHandler(t, validator, provider, "192.0.2.10")
	draftBody := map[string]any{
		"name": "Unavailable draft", "enabled": false, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10", "ipFamily": "ipv4",
	}
	draft := performTaskMutation(t, handler, http.MethodPost, "/api/v1/tasks", draftBody, http.StatusCreated)
	if availabilityCalls.Load() != 0 || validator.calls != 0 {
		t.Fatalf("disabled draft unexpectedly preflighted: availability=%d routes=%d", availabilityCalls.Load(), validator.calls)
	}

	enabledBody := cloneAnyMap(draftBody)
	enabledBody["enabled"] = true
	response := performTaskMutationRaw(t, handler, http.MethodPut, "/api/v1/tasks/"+draft.ID, enabledBody)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Ookla CLI missing; accept the EULA") || strings.Contains(response.Body.String(), `\u0000`) {
		t.Fatalf("enable response status=%d body=%s", response.Code, response.Body.String())
	}
	loaded, err := store.GetTask(context.Background(), draft.ID)
	if err != nil || loaded.Enabled || loaded.NextScheduledAt != nil {
		t.Fatalf("failed enable changed persisted draft: task=%+v err=%v", loaded, err)
	}

	createResponse := performTaskMutationRaw(t, handler, http.MethodPost, "/api/v1/tasks", enabledBody)
	if createResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("enabled create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	tasks, err := store.ListTasks(context.Background())
	if err != nil || len(tasks) != 1 {
		t.Fatalf("failed enabled create persisted a task: count=%d err=%v", len(tasks), err)
	}
}

func TestEnabledTaskEditPreflightFailurePreservesOldScheduledConfiguration(t *testing.T) {
	var available atomic.Bool
	available.Store(true)
	provider := &providerStub{
		id: models.ProviderCloudflare,
		availability: func(context.Context) providers.Availability {
			if available.Load() {
				return providers.Availability{Available: true, Version: "test"}
			}
			return providers.Availability{Available: false, Message: "provider binary disappeared"}
		},
	}
	validator := &recordingRouteValidator{result: models.RouteValidation{Success: true, Reachable: true}}
	handler, store := providerTaskTestHandler(t, validator, provider, "192.0.2.10")
	body := map[string]any{
		"name": "Original enabled task", "enabled": true, "provider": "cloudflare",
		"interfaceName": "wan-test", "sourceIp": "192.0.2.10", "ipFamily": "ipv4",
	}
	created := performTaskMutation(t, handler, http.MethodPost, "/api/v1/tasks", body, http.StatusCreated)
	if created.NextScheduledAt == nil {
		t.Fatal("enabled task was not scheduled")
	}
	available.Store(false)
	body["name"] = "Rejected replacement"
	response := performTaskMutationRaw(t, handler, http.MethodPut, "/api/v1/tasks/"+created.ID, body)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("enabled edit status=%d body=%s", response.Code, response.Body.String())
	}
	loaded, err := store.GetTask(context.Background(), created.ID)
	if err != nil || !loaded.Enabled || loaded.Name != "Original enabled task" || loaded.NextScheduledAt == nil || !loaded.NextScheduledAt.Equal(*created.NextScheduledAt) {
		t.Fatalf("failed edit replaced the scheduled configuration: task=%+v err=%v", loaded, err)
	}
}

func taskDefaultsTestHandler(t *testing.T) (http.Handler, *database.Store) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "task-defaults.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceServiceWithDiscoverer(broker, func(context.Context) ([]models.NetworkInterface, error) {
		return []models.NetworkInterface{{
			Name: "wan-test", Operational: true, OperationalState: "up",
			Addresses: []models.InterfaceAddress{{Address: "192.0.2.10", Family: "ipv4"}},
		}}, nil
	})
	if _, err := interfaces.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	schedule := scheduler.New(store, nil, broker, logger)
	t.Cleanup(func() { _ = schedule.Stop(context.Background()) })
	registry := providers.NewRegistry(cloudflare.New())
	handler := New(store, schedule, nil, interfaces, network.NewRouteValidator(interfaces), registry, broker, logger, BuildInfo{Version: "test"}, HTTPPolicy{ListenAddress: "127.0.0.1:8787", TrustedHosts: []string{"multispeed.local"}}).Handler()
	return handler, store
}

type recordingRouteValidator struct {
	calls   int
	profile models.RouteProfile
	result  models.RouteValidation
	results []models.RouteValidation
}

func (validator *recordingRouteValidator) Validate(_ context.Context, profile models.RouteProfile) models.RouteValidation {
	validator.calls++
	validator.profile = profile
	result := validator.result
	if len(validator.results) > 0 {
		index := validator.calls - 1
		if index >= len(validator.results) {
			index = len(validator.results) - 1
		}
		result = validator.results[index]
	}
	result.InterfaceName = profile.InterfaceName
	result.SourceIP = profile.SourceIP
	result.Destination = profile.ValidationTarget
	return result
}

func transientTaskTestHandler(t *testing.T, validator routeValidator) (http.Handler, *database.Store) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "task-preflight.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceServiceWithDiscoverer(broker, func(context.Context) ([]models.NetworkInterface, error) {
		return []models.NetworkInterface{{
			Name: "wan-test", Operational: true, OperationalState: "up",
			Addresses: []models.InterfaceAddress{{Address: "192.0.2.10", Family: "ipv4"}},
		}}, nil
	})
	if _, err := interfaces.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	schedule := scheduler.New(store, nil, broker, logger)
	t.Cleanup(func() { _ = schedule.Stop(context.Background()) })
	registry := providers.NewRegistry(cloudflare.New())
	return New(store, schedule, nil, interfaces, validator, registry, broker, logger, BuildInfo{Version: "test"}, HTTPPolicy{ListenAddress: "127.0.0.1:8787", TrustedHosts: []string{"multispeed.local"}}).Handler(), store
}

func providerTaskTestHandler(t *testing.T, validator routeValidator, provider providers.Provider, sourceIP string) (http.Handler, *database.Store) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "provider-task-preflight.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	broker := events.New()
	t.Cleanup(broker.Close)
	interfaces := network.NewInterfaceServiceWithDiscoverer(broker, func(context.Context) ([]models.NetworkInterface, error) {
		return []models.NetworkInterface{{
			Name: "wan-test", Operational: true, OperationalState: "up",
			Addresses: []models.InterfaceAddress{{Address: sourceIP, Family: "ipv4"}},
		}}, nil
	})
	if _, err := interfaces.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	schedule := scheduler.New(store, nil, broker, logger)
	t.Cleanup(func() { _ = schedule.Stop(context.Background()) })
	server := New(store, schedule, nil, interfaces, validator, providers.NewRegistry(provider), broker, logger, BuildInfo{Version: "test"}, HTTPPolicy{ListenAddress: "127.0.0.1:8787", TrustedHosts: []string{"multispeed.local"}})
	// Task preflight deliberately reuses the provider checks without consuming
	// or depending on the public provider endpoint's discovery rate gate.
	server.discoveryLimit = newRateGate(0, time.Minute)
	return server.Handler(), store
}

func cloneAnyMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func performPreflight(t *testing.T, handler http.Handler, body map[string]any, wantStatus int) models.RouteValidation {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/tasks/validate", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("preflight status=%d body=%s", response.Code, response.Body.String())
	}
	var validation models.RouteValidation
	if err := json.Unmarshal(response.Body.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	return validation
}

func performTaskMutation(t *testing.T, handler http.Handler, method, path string, body map[string]any, wantStatus int) models.Task {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "http://multispeed.local"+path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status=%d body=%s", method, path, response.Code, response.Body.String())
	}
	var task models.Task
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func performTaskMutationRaw(t *testing.T, handler http.Handler, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "http://multispeed.local"+path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
