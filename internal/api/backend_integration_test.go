package api

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
	"github.com/dude2k/MultiSpeed/internal/statistics"
)

func TestStatisticsHandlerReturnsExactOverallAndGroupedCounts(t *testing.T) {
	fixture := newBackendAPIFixture(t)
	result := seedBackendAPIResult(t, fixture.store)
	from := result.QueuedAt.Add(-time.Minute).Format(time.RFC3339)
	to := result.QueuedAt.Add(time.Minute).Format(time.RFC3339)
	request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/statistics?granularity=raw&groupBy=task&timezone=UTC&from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to), nil)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("statistics status=%d body=%s", response.Code, response.Body.String())
	}
	var report statistics.Report
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.TotalResults != 1 || report.Overall.Counts.Total != 1 || report.Overall.Counts.Successful != 1 || len(report.Groups) != 1 || report.Groups[0].Overall.Counts.Total != 1 {
		t.Fatalf("unexpected statistics report: %+v", report)
	}
}

func TestCSVAndJSONExportsStreamFilteredFullResults(t *testing.T) {
	fixture := newBackendAPIFixture(t)
	result := seedBackendAPIResult(t, fixture.store)

	csvRequest := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/exports/results.csv?status=succeeded", nil)
	csvResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(csvResponse, csvRequest)
	if csvResponse.Code != http.StatusOK || !strings.HasPrefix(csvResponse.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("CSV status=%d headers=%v body=%s", csvResponse.Code, csvResponse.Header(), csvResponse.Body.String())
	}
	rows, err := csv.NewReader(strings.NewReader(csvResponse.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][0] != result.ID || rows[1][16] != "'=diagnostic" {
		t.Fatalf("unexpected CSV rows: %#v", rows)
	}

	jsonRequest := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/exports/results.json?provider=cloudflare", nil)
	jsonResponse := httptest.NewRecorder()
	fixture.handler.ServeHTTP(jsonResponse, jsonRequest)
	if jsonResponse.Code != http.StatusOK || !strings.HasPrefix(jsonResponse.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("JSON status=%d headers=%v body=%s", jsonResponse.Code, jsonResponse.Header(), jsonResponse.Body.String())
	}
	var exported []models.Result
	if err := json.Unmarshal(jsonResponse.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported) != 1 || exported[0].ID != result.ID || exported[0].RawProviderResponse != result.RawProviderResponse || exported[0].RouteValidationSnapshot["success"] != true {
		t.Fatalf("unexpected JSON export: %+v", exported)
	}
}

func TestSSEEndpointDeliversPublishedEventOverHTTP(t *testing.T) {
	fixture := newBackendAPIFixture(t)
	testServer := httptest.NewServer(fixture.handler)
	defer testServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, testServer.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "multispeed.local"
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE status=%d headers=%v", response.StatusCode, response.Header)
	}
	fixture.broker.Publish("test.completed", map[string]string{"resultId": "result-1"})
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	deadline := time.After(2 * time.Second)
	seenEvent, seenData := false, false
	for !seenEvent || !seenData {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("SSE stream closed before the event was delivered")
			}
			seenEvent = seenEvent || line == "event: test.completed"
			seenData = seenData || strings.Contains(line, `"resultId":"result-1"`)
		case <-deadline:
			t.Fatalf("timed out waiting for SSE event: event=%v data=%v", seenEvent, seenData)
		}
	}
}

func TestBackupEndpointReturnsConsistentSQLiteArtifact(t *testing.T) {
	fixture := newBackendAPIFixture(t)
	_ = seedBackendAPIResult(t, fixture.store)
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/backup", nil)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/vnd.sqlite3" || !strings.Contains(response.Header().Get("Content-Disposition"), "multispeed-backup.db") {
		t.Fatalf("backup status=%d headers=%v body-prefix=%q", response.Code, response.Header(), response.Body.Bytes()[:min(response.Body.Len(), 32)])
	}
	if !strings.HasPrefix(response.Body.String(), "SQLite format 3\x00") {
		t.Fatalf("backup did not contain a SQLite header: %q", response.Body.Bytes()[:min(response.Body.Len(), 32)])
	}
	backupPath := filepath.Join(t.TempDir(), "downloaded-backup.db")
	if err := os.WriteFile(backupPath, response.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backup, err := database.Open(context.Background(), backupPath, logger)
	if err != nil {
		t.Fatalf("open downloaded backup: %v", err)
	}
	defer func() { _ = backup.Close() }()
	tasks, results, running, err := backup.Counts(context.Background())
	if err != nil || tasks != 1 || results != 1 || running != 0 {
		t.Fatalf("downloaded backup counts=%d,%d,%d err=%v", tasks, results, running, err)
	}
}

func TestConfigurationExportImportRoundTripExcludesEULAState(t *testing.T) {
	handler, store := taskDefaultsTestHandler(t)
	ctx := context.Background()
	if err := store.SetOoklaEULAAcceptance(ctx, true); err != nil {
		t.Fatal(err)
	}
	profile := models.RouteProfile{Name: "Portable WAN", InterfaceName: "wan-test", SourceIP: "192.0.2.10", ValidationTarget: "1.1.1.1", LastValidationSnapshot: map[string]any{}}
	if err := store.CreateRouteProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	task := models.Task{Name: "Portable task", Enabled: false, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC",
		ServerSelectionMode: "automatic", InterfaceName: "wan-test", SourceIP: "192.0.2.10", IPFamily: "ipv4",
		RouteProfileID: &profile.ID, TimeoutSeconds: 30, PreventOverlap: true, RouteValidation: "required",
		CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}

	exportRequest := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/config/export", nil)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || !strings.Contains(exportResponse.Header().Get("Content-Disposition"), "multispeed-config-") {
		t.Fatalf("export status=%d headers=%v body=%s", exportResponse.Code, exportResponse.Header(), exportResponse.Body.String())
	}
	if strings.Contains(exportResponse.Body.String(), "ooklaEula") || strings.Contains(exportResponse.Body.String(), "lastValidation") || strings.Contains(exportResponse.Body.String(), "nextScheduledAt") {
		t.Fatalf("export leaked non-portable state: %s", exportResponse.Body.String())
	}
	var document models.ConfigurationDocument
	if err := json.Unmarshal(exportResponse.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Format != models.ConfigurationFormat || document.Version != 1 || len(document.Tasks) != 1 || len(document.RouteProfiles) != 1 {
		t.Fatalf("unexpected export: %+v", document)
	}
	document.Settings.GlobalConcurrency = 2
	document.Tasks[0].Name = "Restored task"
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	importRequest := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/config/import", strings.NewReader(string(payload)))
	importRequest.Header.Set("Content-Type", "application/json")
	importResponse := httptest.NewRecorder()
	handler.ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importResponse.Code, importResponse.Body.String())
	}
	var imported models.ConfigurationImportResult
	if err := json.Unmarshal(importResponse.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.TaskCount != 1 || imported.RouteProfileCount != 1 || !imported.SettingsUpdated {
		t.Fatalf("unexpected import result: %+v", imported)
	}
	loadedTask, err := store.GetTask(ctx, task.ID)
	if err != nil || loadedTask.Name != "Restored task" {
		t.Fatalf("imported task=%+v error=%v", loadedTask, err)
	}
	settings, err := store.GetSettings(ctx)
	if err != nil || settings.GlobalConcurrency != 2 || !settings.OoklaEULAAccepted {
		t.Fatalf("imported settings=%+v error=%v", settings, err)
	}
}

func TestConfigurationImportRejectsCustomServerOutsideOperatorAllowlist(t *testing.T) {
	var validationCalls int
	provider := &providerStub{
		id: models.ProviderLibreSpeed,
		validate: func(_ context.Context, target providers.TestTarget) error {
			validationCalls++
			if target.SelectionMode != "custom" || target.ServerURL != "https://unapproved.example.test" {
				t.Fatalf("unexpected imported target: %+v", target)
			}
			return errors.New("custom server URL is not present in the operator allowlist")
		},
	}
	handler, store := providerTaskTestHandler(t, &recordingRouteValidator{}, provider, "192.0.2.10")
	settings, err := store.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	document := models.ConfigurationDocument{
		Format: models.ConfigurationFormat, Version: models.ConfigurationFormatVersion,
		ExportedAt: time.Now().UTC(), ApplicationVersion: "test",
		Settings: models.ConfigurationSettingsFrom(settings), RouteProfiles: []models.ConfigurationRouteProfile{},
		Tasks: []models.ConfigurationTask{{
			ID: "26f8bdda-8ceb-4220-8b2f-c91d22b681f8", Name: "Unapproved custom backend", Enabled: false,
			Provider: models.ProviderLibreSpeed, CronExpression: "0 * * * *", Timezone: "UTC",
			ServerSelectionMode: "custom", ServerURL: "https://unapproved.example.test",
			CustomServerDefinition: map[string]any{}, InterfaceName: "wan-test", SourceIP: "192.0.2.10",
			IPFamily: "ipv4", TimeoutSeconds: 30, ProviderOptions: map[string]any{}, PreventOverlap: true,
			RouteValidation: "required",
		}},
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/config/import", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "operator allowlist") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if validationCalls != 1 {
		t.Fatalf("provider validation calls=%d, want 1", validationCalls)
	}
	if tasks, err := store.ListTasks(context.Background()); err != nil || len(tasks) != 0 {
		t.Fatalf("rejected import persisted tasks=%d err=%v", len(tasks), err)
	}
}

func TestConfigurationImportRejectsUnknownConsentField(t *testing.T) {
	handler, _ := taskDefaultsTestHandler(t)
	body := `{"format":"multispeed-config","version":1,"exportedAt":"2026-08-07T18:00:00Z","applicationVersion":"test","settings":{"displayUnits":"bits","defaultTimezone":"UTC","globalConcurrency":1,"allowSeparateWanConcurrency":false,"retentionMode":"forever","retentionValue":0,"defaultChartRange":"30d","interfaceRefreshIntervalSeconds":30,"defaultTaskTimeoutSeconds":120,"databaseMaintenanceSchedule":"0 3 * * 0","ooklaEulaAccepted":true},"routeProfiles":[],"tasks":[]}`
	request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/config/import", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "unknown field") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestConfigurationImportRejectsNullAndIncompleteCollections(t *testing.T) {
	handler, _ := taskDefaultsTestHandler(t)
	settings := `"settings":{"displayUnits":"bits","defaultTimezone":"UTC","globalConcurrency":1,"allowSeparateWanConcurrency":false,"retentionMode":"forever","retentionValue":0,"defaultChartRange":"30d","interfaceRefreshIntervalSeconds":30,"defaultTaskTimeoutSeconds":120,"databaseMaintenanceSchedule":"0 3 * * 0"}`
	for name, body := range map[string]string{
		"null tasks":      `{"format":"multispeed-config","version":1,"exportedAt":"2026-08-07T18:00:00Z","applicationVersion":"test",` + settings + `,"routeProfiles":[],"tasks":null}`,
		"incomplete task": `{"format":"multispeed-config","version":1,"exportedAt":"2026-08-07T18:00:00Z","applicationVersion":"test",` + settings + `,"routeProfiles":[],"tasks":[{"id":"26f8bdda-8ceb-4220-8b2f-c91d22b681f8","name":"Missing enabled"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://multispeed.local/api/v1/config/import", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "required") && !strings.Contains(response.Body.String(), "must be an array") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

type backendAPIFixture struct {
	handler http.Handler
	store   *database.Store
	broker  *events.Broker
}

func newBackendAPIFixture(t *testing.T) backendAPIFixture {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "backend-api.db"), logger)
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
	server := New(store, nil, nil, interfaces, &recordingRouteValidator{}, providers.NewRegistry(), broker, logger, BuildInfo{Version: "test"}, HTTPPolicy{ListenAddress: "127.0.0.1:8787", TrustedHosts: []string{"multispeed.local"}})
	return backendAPIFixture{handler: server.Handler(), store: store, broker: broker}
}

func seedBackendAPIResult(t *testing.T, store *database.Store) models.Result {
	t.Helper()
	ctx := context.Background()
	task := models.Task{
		Name: "Backend integration", Enabled: false, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC",
		ServerSelectionMode: "automatic", InterfaceName: "wan-test", SourceIP: "192.0.2.10", IPFamily: "ipv4",
		TimeoutSeconds: 30, PreventOverlap: true, RouteValidation: "required",
		CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{},
	}
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	queued := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	finished := queued.Add(15 * time.Second)
	download, upload, latency := int64(100_000_000), int64(50_000_000), 12.5
	result := models.Result{
		TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, QueuedAt: queued, StartedAt: &queued, FinishedAt: &finished,
		Status: models.StatusSucceeded, DownloadBitsPerSecond: &download, UploadBitsPerSecond: &upload, LatencyMilliseconds: &latency,
		SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, ServerName: "=diagnostic",
		RouteValidationSnapshot: map[string]any{"success": true}, RawProviderResponse: `{"complete":true}`,
	}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
