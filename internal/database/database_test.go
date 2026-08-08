package database

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "multispeed.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func TestMigrationsCRUDAndBackup(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	version, err := store.SchemaVersion(ctx)
	if err != nil || version != 6 {
		t.Fatalf("SchemaVersion() = %d, %v", version, err)
	}
	settings, err := store.GetSettings(ctx)
	if err != nil || settings.GlobalConcurrency != 1 || settings.RetentionMode != "forever" || settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt != nil {
		t.Fatalf("GetSettings() = %#v, %v", settings, err)
	}
	if err := store.SetOoklaEULAAcceptance(ctx, true); err != nil {
		t.Fatal(err)
	}
	settings, err = store.GetSettings(ctx)
	if err != nil || !settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt == nil || settings.OoklaEULAVersion != models.CurrentOoklaEULAVersion {
		t.Fatalf("accepted settings = %#v, %v", settings, err)
	}
	settings.OoklaEULAAccepted = false
	settings.OoklaEULAAcceptedAt = nil
	settings.GlobalConcurrency = 2
	if err := store.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	settings, err = store.GetSettings(ctx)
	if err != nil || !settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt == nil || settings.GlobalConcurrency != 2 {
		t.Fatalf("routine update changed EULA acceptance: %#v, %v", settings, err)
	}
	if err := store.SetOoklaEULAAcceptance(ctx, false); err != nil {
		t.Fatal(err)
	}
	settings, err = store.GetSettings(ctx)
	if err != nil || settings.OoklaEULAAccepted || settings.OoklaEULAAcceptedAt != nil || settings.OoklaEULAVersion != "" {
		t.Fatalf("revoked settings = %#v, %v", settings, err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE settings SET ookla_eula_accepted=1, ookla_eula_accepted_at=?, ookla_eula_version='outdated-review' WHERE singleton=1`, formatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	settings, err = store.GetSettings(ctx)
	effective, effectiveErr := store.OoklaEULAAcceptance(ctx)
	if err != nil || effectiveErr != nil || settings.OoklaEULAAccepted || effective || settings.OoklaEULAAcceptedAt == nil || settings.OoklaEULAVersion != "outdated-review" {
		t.Fatalf("outdated acceptance remained effective: settings=%#v effective=%v errors=%v/%v", settings, effective, err, effectiveErr)
	}
	if err := store.SetOoklaEULAAcceptance(ctx, false); err != nil {
		t.Fatal(err)
	}
	profile := models.RouteProfile{Name: "Primary WAN", InterfaceName: "eth0", SourceIP: "192.0.2.10", ValidationTarget: "1.1.1.1", LastValidationSnapshot: map[string]any{}}
	if err := store.CreateRouteProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	routeID := profile.ID
	task := models.Task{Name: "Hourly test", Enabled: true, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC", ServerSelectionMode: "automatic", InterfaceName: "eth0", SourceIP: "192.0.2.10", IPFamily: "ipv4", RouteProfileID: &routeID, TimeoutSeconds: 120, PreventOverlap: true, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetTask(ctx, task.ID)
	if err != nil || loaded.Name != task.Name || loaded.RouteProfileID == nil || *loaded.RouteProfileID != routeID {
		t.Fatalf("GetTask() = %#v, %v", loaded, err)
	}
	now := time.Now().UTC()
	down := int64(100_000_000)
	latency := 12.5
	result := models.Result{TaskID: task.ID, RouteProfileID: &routeID, Trigger: models.TriggerManual, Provider: task.Provider, StartedAt: &now, FinishedAt: &now, Status: models.StatusSucceeded, DownloadBitsPerSecond: &down, LatencyMilliseconds: &latency, SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{"success": true}, RawProviderResponse: `{"download":100000000}`}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListResults(ctx, ResultFilter{TaskID: task.ID, Page: 1, PageSize: 25})
	if err != nil || page.TotalItems != 1 || len(page.Items) != 1 {
		t.Fatalf("ListResults() = %#v, %v", page, err)
	}
	if page.Items[0].RawProviderResponse != "" || len(page.Items[0].RouteValidationSnapshot) != 0 {
		t.Fatalf("ListResults loaded heavy diagnostics: %+v", page.Items[0])
	}
	detail, err := store.GetResult(ctx, result.ID)
	if err != nil || detail.RawProviderResponse != result.RawProviderResponse || detail.RouteValidationSnapshot["success"] != true {
		t.Fatalf("GetResult() lost full diagnostics: %+v, %v", detail, err)
	}
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := store.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	backup, err := Open(ctx, backupPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = backup.Close() }()
	if taskCount, resultCount, _, err := backup.Counts(ctx); err != nil || taskCount != 1 || resultCount != 1 {
		t.Fatalf("backup counts = %d,%d,%v", taskCount, resultCount, err)
	}
}

func TestRecoverInterruptedResults(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	task := models.Task{Name: "Task", Enabled: true, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC", ServerSelectionMode: "automatic", InterfaceName: "eth0", SourceIP: "192.0.2.2", IPFamily: "ipv4", TimeoutSeconds: 30, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	result := models.Result{TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, Status: models.StatusRunning, SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{}}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}
	count, err := store.RecoverInterruptedResults(ctx)
	if err != nil || count != 1 {
		t.Fatalf("RecoverInterruptedResults()=%d,%v", count, err)
	}
	loaded, err := store.GetResult(ctx, result.ID)
	if err != nil || loaded.Status != models.StatusCancelled || loaded.FinishedAt == nil {
		t.Fatalf("recovered result=%#v,%v", loaded, err)
	}
}
