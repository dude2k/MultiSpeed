package statistics_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/statistics"
)

func TestServiceStreamsFilteredDatabaseResults(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(ctx, filepath.Join(t.TempDir(), "statistics.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	for _, task := range []*models.Task{
		integrationTask("task-a", "wan-a", "192.0.2.10"),
		integrationTask("task-b", "wan-b", "192.0.2.20"),
	} {
		if err := store.CreateTask(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	from := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	createIntegrationResult(t, ctx, store, "result-a", "task-a", "wan-a", "192.0.2.10", "203.0.113.10", from.Add(time.Hour))
	createIntegrationResult(t, ctx, store, "result-b", "task-b", "wan-b", "192.0.2.20", "203.0.113.20", from.Add(2*time.Hour))
	createIntegrationResult(t, ctx, store, "outside", "task-a", "wan-a", "192.0.2.10", "203.0.113.10", from.AddDate(0, 0, 2))

	report, err := statistics.New(store).Query(ctx, statistics.Query{
		From:              from,
		To:                from.AddDate(0, 0, 1),
		Granularity:       statistics.GranularityDay,
		ReportingTimezone: "UTC",
		GroupBy:           statistics.DimensionInterface,
		Filter: statistics.Filter{
			TaskIDs:    []string{"task-a"},
			Interfaces: []string{"wan-a"},
			PublicIPs:  []string{"203.0.113.10"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalResults != 1 || len(report.Groups) != 1 || report.Groups[0].Key != "wan-a" ||
		len(report.Groups[0].Buckets) != 1 || report.Groups[0].Buckets[0].Metrics.DownloadBitsPerSecond.Count != 1 {
		t.Fatalf("unexpected filtered database report: %+v", report)
	}
}

func integrationTask(id, interfaceName, sourceIP string) *models.Task {
	return &models.Task{
		ID:                     id,
		Name:                   id,
		Enabled:                true,
		Provider:               models.ProviderOokla,
		CronExpression:         "0 * * * *",
		Timezone:               "UTC",
		ServerSelectionMode:    "automatic",
		CustomServerDefinition: map[string]any{},
		InterfaceName:          interfaceName,
		SourceIP:               sourceIP,
		IPFamily:               "ipv4",
		TimeoutSeconds:         120,
		ProviderOptions:        map[string]any{},
		PreventOverlap:         true,
		RouteValidation:        "interface-only",
	}
}

func createIntegrationResult(t *testing.T, ctx context.Context, store *database.Store, id, taskID, interfaceName, sourceIP, publicIP string, startedAt time.Time) {
	t.Helper()
	download := int64(100_000_000)
	upload := int64(20_000_000)
	latency := 12.5
	finishedAt := startedAt.Add(10 * time.Second)
	result := &models.Result{
		ID:                    id,
		TaskID:                taskID,
		Trigger:               models.TriggerManual,
		Provider:              models.ProviderOokla,
		StartedAt:             &startedAt,
		FinishedAt:            &finishedAt,
		Status:                models.StatusSucceeded,
		DownloadBitsPerSecond: &download,
		UploadBitsPerSecond:   &upload,
		LatencyMilliseconds:   &latency,
		SelectedInterface:     interfaceName,
		SelectedSourceIP:      sourceIP,
		DetectedPublicIP:      publicIP,
		ServerID:              "server-1",
		RouteValidationSnapshot: map[string]any{
			"success": true,
		},
		ExecutionDurationMS: 10_000,
	}
	if err := store.CreateResult(ctx, result); err != nil {
		t.Fatalf("create result %s: %v", id, err)
	}
}
