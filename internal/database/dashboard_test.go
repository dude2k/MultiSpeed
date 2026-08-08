package database

import (
	"context"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func TestGetDashboardResultsIsBoundedCompactAndKeepsAllLatestStatuses(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	tasks := []models.Task{
		dashboardTestTask("Failed task", "wan-a", "192.0.2.10"),
		dashboardTestTask("Skipped task", "wan-a", "192.0.2.10"),
		dashboardTestTask("Running task", "wan-b", "198.51.100.10"),
		dashboardTestTask("Never run", "wan-c", "203.0.113.10"),
	}
	for index := range tasks {
		if err := store.CreateTask(ctx, &tasks[index]); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)
	results := []models.Result{
		dashboardTestResult(tasks[0], models.StatusSucceeded, base),
		dashboardTestResult(tasks[0], models.StatusFailed, base.Add(time.Minute)),
		dashboardTestResult(tasks[1], models.StatusSkipped, base.Add(2*time.Minute)),
		dashboardTestResult(tasks[2], models.StatusRunning, base.Add(3*time.Minute)),
	}
	for index := range results {
		if err := store.CreateResult(ctx, &results[index]); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := store.GetDashboardResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks) != 4 || len(snapshot.LatestByTask) != 3 || len(snapshot.LatestByPath) != 2 || len(snapshot.ActiveRuns) != 1 || len(snapshot.RecentFailures) != 1 {
		t.Fatalf("unexpected dashboard sizes: tasks=%d taskLatest=%d pathLatest=%d active=%d failures=%d", len(snapshot.Tasks), len(snapshot.LatestByTask), len(snapshot.LatestByPath), len(snapshot.ActiveRuns), len(snapshot.RecentFailures))
	}
	statuses := map[models.ResultStatus]bool{}
	for _, result := range snapshot.LatestByTask {
		statuses[result.Status] = true
		if result.RawProviderResponse != "" || len(result.RouteValidationSnapshot) != 0 {
			t.Fatalf("dashboard loaded heavy diagnostics: %+v", result)
		}
	}
	for _, status := range []models.ResultStatus{models.StatusFailed, models.StatusSkipped, models.StatusRunning} {
		if !statuses[status] {
			t.Fatalf("latest dashboard results omitted status %q", status)
		}
	}
	if snapshot.LatestByPath[0].Status != models.StatusRunning && snapshot.LatestByPath[1].Status != models.StatusRunning {
		t.Fatalf("running path missing from latest paths: %+v", snapshot.LatestByPath)
	}
}

func dashboardTestTask(name, interfaceName, sourceIP string) models.Task {
	return models.Task{
		Name: name, Enabled: false, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC",
		ServerSelectionMode: "automatic", InterfaceName: interfaceName, SourceIP: sourceIP, IPFamily: "ipv4",
		TimeoutSeconds: 30, PreventOverlap: true, RouteValidation: "required",
		CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{},
	}
}

func dashboardTestResult(task models.Task, status models.ResultStatus, queuedAt time.Time) models.Result {
	finished := queuedAt
	if status == models.StatusRunning {
		finished = time.Time{}
	}
	result := models.Result{
		TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, QueuedAt: queuedAt, Status: status,
		SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP,
		RouteValidationSnapshot: map[string]any{"large": true}, RawProviderResponse: `{"large":true}`,
	}
	if !finished.IsZero() {
		result.FinishedAt = &finished
	}
	return result
}
