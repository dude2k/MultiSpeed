package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func TestReplaceConfigurationPreservesHistoryAndEULAAcceptance(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.SetOoklaEULAAcceptance(ctx, true); err != nil {
		t.Fatal(err)
	}
	profile := models.RouteProfile{Name: "Old WAN", InterfaceName: "eth0", SourceIP: "192.0.2.10", ValidationTarget: "1.1.1.1", LastValidationSnapshot: map[string]any{}}
	if err := store.CreateRouteProfile(ctx, &profile); err != nil {
		t.Fatal(err)
	}
	task := lifecycleTask(t, store, &profile.ID)
	retired := lifecycleTask(t, store, nil)
	retired.Name = "Retired by import"
	if err := store.UpdateTask(ctx, &retired); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	result := models.Result{TaskID: task.ID, RouteProfileID: &profile.ID, Trigger: models.TriggerManual, Provider: task.Provider,
		StartedAt: &now, FinishedAt: &now, Status: models.StatusSucceeded, SelectedInterface: task.InterfaceName,
		SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{"success": true}}
	if err := store.CreateResult(ctx, &result); err != nil {
		t.Fatal(err)
	}

	settings, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.GlobalConcurrency = 3
	profile.Name = "Imported WAN"
	task.Name = "Imported task"
	task.Enabled = false
	if err := store.ReplaceConfiguration(ctx, settings, []models.RouteProfile{profile}, []models.Task{task}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Settings.GlobalConcurrency != 3 || len(snapshot.RouteProfiles) != 1 || snapshot.RouteProfiles[0].Name != "Imported WAN" || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Name != "Imported task" {
		t.Fatalf("unexpected imported snapshot: %+v", snapshot)
	}
	if _, err := store.GetTask(ctx, retired.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("task omitted by replacement remained active: %v", err)
	}
	loadedResult, err := store.GetResult(ctx, result.ID)
	if err != nil || loadedResult.TaskID != task.ID || loadedResult.RouteProfileID == nil || *loadedResult.RouteProfileID != profile.ID {
		t.Fatalf("historical result changed: %+v error=%v", loadedResult, err)
	}
	accepted, err := store.OoklaEULAAcceptance(ctx)
	if err != nil || !accepted {
		t.Fatalf("configuration replacement changed EULA acceptance: accepted=%v error=%v", accepted, err)
	}
}

func TestReplaceConfigurationRejectsActiveResultsWithoutPartialWrites(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	task := lifecycleTask(t, store, nil)
	active := models.Result{TaskID: task.ID, Trigger: models.TriggerManual, Provider: task.Provider, Status: models.StatusQueued,
		SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, RouteValidationSnapshot: map[string]any{}}
	if err := store.CreateResult(ctx, &active); err != nil {
		t.Fatal(err)
	}
	settings, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.GlobalConcurrency = 4
	if err := store.ReplaceConfiguration(ctx, settings, nil, nil); !errors.Is(err, ErrActive) {
		t.Fatalf("ReplaceConfiguration() error=%v", err)
	}
	unchangedSettings, err := store.GetSettings(ctx)
	if err != nil || unchangedSettings.GlobalConcurrency != 1 {
		t.Fatalf("settings changed after rejected import: %+v error=%v", unchangedSettings, err)
	}
	if _, err := store.GetTask(ctx, task.ID); err != nil {
		t.Fatalf("task changed after rejected import: %v", err)
	}
}
