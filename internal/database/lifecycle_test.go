package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func lifecycleTask(t *testing.T, store *Store, routeID *string) models.Task {
	t.Helper()
	task := models.Task{Name: "Lifecycle", Enabled: true, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *",
		Timezone: "UTC", ServerSelectionMode: "automatic", InterfaceName: "eth0", SourceIP: "192.0.2.10", IPFamily: "ipv4",
		RouteProfileID: routeID, TimeoutSeconds: 30, PreventOverlap: true, RouteValidation: "required",
		CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestSoftDeletionPreservesHistoricalResultsAndRouteSnapshots(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	profile := models.RouteProfile{Name: "WAN", InterfaceName: "eth0", SourceIP: "192.0.2.10", ValidationTarget: "1.1.1.1", LastValidationSnapshot: map[string]any{}}
	if err := store.CreateRouteProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	task := lifecycleTask(t, store, &profile.ID)
	now := time.Now().UTC()
	result := models.Result{TaskID: task.ID, RouteProfileID: &profile.ID, Trigger: models.TriggerManual, Provider: task.Provider,
		StartedAt: &now, FinishedAt: &now, Status: models.StatusSucceeded, SelectedInterface: task.InterfaceName,
		SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{"success": true}}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteRouteProfile(ctx, profile.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete referenced route error=%v", err)
	}
	if err := store.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTask(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted task remained visible: %v", err)
	}
	if err := store.DeleteRouteProfile(ctx, profile.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetResult(ctx, result.ID)
	if err != nil || loaded.TaskID != task.ID || loaded.RouteProfileID == nil || *loaded.RouteProfileID != profile.ID {
		t.Fatalf("historical result=%#v error=%v", loaded, err)
	}
}

func TestActiveExecutionsCannotBeDeleted(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	task := lifecycleTask(t, store, nil)
	result := models.Result{TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, Status: models.StatusRunning,
		SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{}}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteResult(ctx, result.ID); !errors.Is(err, ErrActive) {
		t.Fatalf("delete active result error=%v", err)
	}
	if err := store.DeleteTask(ctx, task.ID); !errors.Is(err, ErrActive) {
		t.Fatalf("delete task with active result error=%v", err)
	}
	if _, err := store.DeleteResults(ctx, []string{result.ID}); !errors.Is(err, ErrActive) {
		t.Fatalf("batch delete active result error=%v", err)
	}
}

func TestFixedWidthTimestampFilteringIncludesFractionalFirstSecond(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	task := lifecycleTask(t, store, nil)
	from := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Second)
	for _, started := range []time.Time{from, from.Add(500 * time.Millisecond)} {
		value := started
		result := models.Result{TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, StartedAt: &value,
			FinishedAt: &value, Status: models.StatusSucceeded, SelectedInterface: task.InterfaceName,
			SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{}}
		if err := store.CreateResult(ctx, &result); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListResults(ctx, ResultFilter{From: &from, To: &to, PageSize: 10})
	if err != nil || page.TotalItems != 2 {
		t.Fatalf("filtered results=%d error=%v", page.TotalItems, err)
	}
}

func TestRetentionDeletesOnlyFinishedTerminalResults(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	task := lifecycleTask(t, store, nil)
	old := time.Now().UTC().Add(-48 * time.Hour)
	active := models.Result{TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, StartedAt: &old,
		Status: models.StatusRunning, SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{}}
	terminal := models.Result{TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, StartedAt: &old, FinishedAt: &old,
		Status: models.StatusFailed, SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{}}
	if err := store.CreateResult(ctx, &active); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateResult(ctx, &terminal); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteResultsBefore(ctx, time.Now().UTC().Add(-24*time.Hour), 100)
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d error=%v", deleted, err)
	}
	if _, err := store.GetResult(ctx, active.ID); err != nil {
		t.Fatalf("active result was removed: %v", err)
	}
}
