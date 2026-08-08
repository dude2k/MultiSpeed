package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func TestParseResultFilterAcceptsBoundedComparisonPage(t *testing.T) {
	request := httptest.NewRequest("GET", "http://multispeed.local/api/v1/results?page=1&pageSize=200", nil)
	filter, err := parseResultFilter(request)
	if err != nil {
		t.Fatal(err)
	}
	if filter.Page != 1 || filter.PageSize != 200 {
		t.Fatalf("pagination=(%d, %d), want (1, 200)", filter.Page, filter.PageSize)
	}
	if filter.Sort != "startedAt" || !filter.Descending {
		t.Fatalf("defaults sort=%q descending=%v", filter.Sort, filter.Descending)
	}
}

func TestParseResultFilterRejectsOutOfContractPagination(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{name: "zero page", query: "page=0", wantErr: "page must be at least 1"},
		{name: "negative page", query: "page=-1", wantErr: "page must be at least 1"},
		{name: "zero page size", query: "pageSize=0", wantErr: "pageSize must be between 1 and 200"},
		{name: "negative page size", query: "pageSize=-1", wantErr: "pageSize must be between 1 and 200"},
		{name: "page size above maximum", query: "pageSize=201", wantErr: "pageSize must be between 1 and 200"},
		{name: "maximum integer page size", query: "pageSize=" + strconv.Itoa(maxInt), wantErr: "pageSize must be between 1 and 200"},
		{name: "page integer overflow", query: "page=" + strings.Repeat("9", 100), wantErr: "page must be an integer"},
		{name: "page size integer overflow", query: "pageSize=" + strings.Repeat("9", 100), wantErr: "pageSize must be an integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://multispeed.local/api/v1/results?"+tt.query, nil)
			_, err := parseResultFilter(request)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("parseResultFilter(%q) error = %v, want %q", tt.query, err, tt.wantErr)
			}
		})
	}
}

func TestParseResultFilterAcceptsMaximumIntegerPage(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	request := httptest.NewRequest("GET", "http://multispeed.local/api/v1/results?page="+strconv.Itoa(maxInt)+"&pageSize=200", nil)
	filter, err := parseResultFilter(request)
	if err != nil {
		t.Fatal(err)
	}
	if filter.Page != maxInt || filter.PageSize != 200 {
		t.Fatalf("pagination=(%d, %d), want (%d, 200)", filter.Page, filter.PageSize, maxInt)
	}
}

func TestParseResultFilterRejectsUnknownEnumValuesAndInvalidRange(t *testing.T) {
	for _, query := range []string{
		"provider=unknown", "status=completed", "sort=queuedAt", "direction=sideways", "direction=DESC",
		"from=2026-01-02T00%3A00%3A00Z&to=2026-01-01T00%3A00%3A00Z",
		"from=2026-01-01T00%3A00%3A00Z&to=2026-01-01T00%3A00%3A00Z",
	} {
		request := httptest.NewRequest("GET", "http://multispeed.local/api/v1/results?"+query, nil)
		if _, err := parseResultFilter(request); err == nil {
			t.Fatalf("query %q unexpectedly succeeded", query)
		}
	}
}

func TestCompactResultOmitsHeavyDiagnostics(t *testing.T) {
	queued := time.Now().UTC()
	encoded, err := json.Marshal(compactResult(models.Result{
		ID: "result-1", TaskID: "task-1", QueuedAt: queued, Status: models.StatusFailed,
		RouteValidationSnapshot: map[string]any{"command": strings.Repeat("x", 4096)},
		RawProviderResponse:     strings.Repeat("y", 256<<10), SanitizedError: "bounded failure",
	}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "rawProviderResponse") || strings.Contains(text, "routeValidationSnapshot") {
		t.Fatalf("compact result leaked heavy diagnostics: %s", text)
	}
	if !strings.Contains(text, `"sanitizedError":"bounded failure"`) {
		t.Fatalf("compact result omitted the light diagnostic message: %s", text)
	}
}

func TestDashboardResultsCoversConfiguredPathsAndOmitsHeavyDiagnostics(t *testing.T) {
	handler, store := taskDefaultsTestHandler(t)
	ctx := context.Background()
	tasks := []models.Task{
		apiDashboardTask("Failed", "wan-a", "192.0.2.10"),
		apiDashboardTask("Skipped", "wan-a", "192.0.2.10"),
		apiDashboardTask("Running", "wan-b", "198.51.100.10"),
		apiDashboardTask("Never run", "wan-c", "203.0.113.10"),
	}
	for index := range tasks {
		if err := store.CreateTask(ctx, &tasks[index]); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)
	results := []models.Result{
		apiDashboardResult(tasks[0], models.StatusFailed, base),
		apiDashboardResult(tasks[1], models.StatusSkipped, base.Add(time.Minute)),
		apiDashboardResult(tasks[2], models.StatusRunning, base.Add(2*time.Minute)),
	}
	for index := range results {
		if err := store.CreateResult(ctx, &results[index]); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest("GET", "http://multispeed.local/api/v1/results/dashboard-summary", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "rawProviderResponse") || strings.Contains(response.Body.String(), "routeValidationSnapshot") {
		t.Fatalf("dashboard response leaked heavy diagnostics: %s", response.Body.String())
	}
	var payload struct {
		LatestByTask []struct {
			LatestResult *resultSummary `json:"latestResult"`
		} `json:"latestByTask"`
		LatestByPath []struct {
			LatestResult *resultSummary `json:"latestResult"`
		} `json:"latestByPath"`
		ActiveRuns      []resultSummary `json:"activeRuns"`
		RecentFailures  []resultSummary `json:"recentFailures"`
		FailedTaskCount int             `json:"failedTaskCount"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.LatestByTask) != 4 || len(payload.LatestByPath) != 3 || len(payload.ActiveRuns) != 1 || len(payload.RecentFailures) != 1 || payload.FailedTaskCount != 1 {
		t.Fatalf("unexpected dashboard response: %+v", payload)
	}
	nullTask, nullPath := false, false
	for _, item := range payload.LatestByTask {
		nullTask = nullTask || item.LatestResult == nil
	}
	for _, item := range payload.LatestByPath {
		nullPath = nullPath || item.LatestResult == nil
	}
	if !nullTask || !nullPath {
		t.Fatalf("never-run task/path was not represented: tasks=%+v paths=%+v", payload.LatestByTask, payload.LatestByPath)
	}
}

func apiDashboardTask(name, interfaceName, sourceIP string) models.Task {
	return models.Task{
		Name: name, Enabled: false, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC",
		ServerSelectionMode: "automatic", InterfaceName: interfaceName, SourceIP: sourceIP, IPFamily: "ipv4",
		TimeoutSeconds: 30, PreventOverlap: true, RouteValidation: "required",
		CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{},
	}
}

func apiDashboardResult(task models.Task, status models.ResultStatus, queuedAt time.Time) models.Result {
	result := models.Result{
		TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, QueuedAt: queuedAt, Status: status,
		SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP,
		RouteValidationSnapshot: map[string]any{"large": true}, RawProviderResponse: strings.Repeat("x", 32<<10),
	}
	if status != models.StatusRunning {
		finished := queuedAt
		result.FinishedAt = &finished
	}
	return result
}
