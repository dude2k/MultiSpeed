package scheduler

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/execution"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/providers"
)

func testStore(t *testing.T) (*database.Store, context.Context, *slog.Logger) {
	t.Helper()
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(ctx, filepath.Join(t.TempDir(), "scheduler.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, ctx, logger
}

func testTask() models.Task {
	return models.Task{
		Name:                   "Scheduled task",
		Enabled:                true,
		Provider:               models.ProviderCloudflare,
		CronExpression:         "* * * * *",
		Timezone:               "UTC",
		RandomJitterSeconds:    10,
		ServerSelectionMode:    "automatic",
		InterfaceName:          "eth0",
		SourceIP:               "192.0.2.10",
		IPFamily:               "ipv4",
		TimeoutSeconds:         60,
		RouteValidation:        "required",
		CustomServerDefinition: map[string]any{},
		ProviderOptions:        map[string]any{},
	}
}

func TestNextRunsUsesTaskTimezone(t *testing.T) {
	service := New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	task := models.Task{CronExpression: "30 2 * * *", Timezone: "Europe/Berlin"}
	runs, err := service.NextRuns(task, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 5 {
		t.Fatalf("len=%d", len(runs))
	}
	location, _ := time.LoadLocation(task.Timezone)
	for _, run := range runs {
		local := run.In(location)
		if local.Hour() != 2 || local.Minute() != 30 {
			t.Fatalf("run %v is %v locally", run, local)
		}
	}
}

func TestReconcileReplacesCompleteInMemoryScheduleSet(t *testing.T) {
	store, ctx, logger := testStore(t)
	first, second := testTask(), testTask()
	first.Name, second.Name = "First", "Second"
	if err := store.CreateTask(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTask(ctx, &second); err != nil {
		t.Fatal(err)
	}
	service := New(store, nil, nil, logger)
	if err := service.Reconcile(ctx, []models.Task{first, second}); err != nil {
		t.Fatal(err)
	}
	if len(service.entries) != 2 {
		t.Fatalf("scheduled entries=%d", len(service.entries))
	}
	if err := service.Reconcile(ctx, []models.Task{second}); err != nil {
		t.Fatal(err)
	}
	if len(service.entries) != 1 {
		t.Fatalf("reconciled entries=%d", len(service.entries))
	}
	if _, present := service.entries[first.ID]; present {
		t.Fatal("omitted task remained scheduled")
	}
	if _, present := service.entries[second.ID]; !present {
		t.Fatal("imported task was not scheduled")
	}
}

func TestStartCalculatesNextRunBeforeCronLoopStarts(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := models.Task{Name: "Future", Enabled: true, Provider: models.ProviderCloudflare, CronExpression: "0 0 1 1 *", Timezone: "UTC", ServerSelectionMode: "automatic", InterfaceName: "eth0", SourceIP: "192.0.2.10", IPFamily: "ipv4", TimeoutSeconds: 60, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	service := New(store, nil, nil, logger)
	if err := service.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	stopCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	defer func() { _ = service.Stop(stopCtx) }()
	loaded, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NextScheduledAt == nil || !loaded.NextScheduledAt.After(time.Now()) {
		t.Fatalf("next run=%v", loaded.NextScheduledAt)
	}
}
func TestNextRunsRejectsInvalidCronAndTimezone(t *testing.T) {
	service := New(nil, nil, nil, slog.Default())
	if _, err := service.NextRuns(models.Task{CronExpression: "not cron", Timezone: "UTC"}, 5); err == nil {
		t.Fatal("expected cron error")
	}
	if _, err := service.NextRuns(models.Task{CronExpression: "0 * * * *", Timezone: "Mars/Olympus"}, 5); err == nil {
		t.Fatal("expected timezone error")
	}
}
func TestNextRunsCrossesDSTWithoutDuplicateUTCInstants(t *testing.T) {
	service := New(nil, nil, nil, slog.Default())
	runs, err := service.NextRuns(models.Task{CronExpression: "30 2 * * *", Timezone: "Europe/Berlin"}, 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[time.Time]bool{}
	for _, run := range runs {
		if seen[run] {
			t.Fatalf("duplicate UTC instant %v", run)
		}
		seen[run] = true
	}
}

func TestRescheduleInvalidatesPendingJitterAndPreservesFreshLastRun(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := testTask()
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}

	service := New(store, nil, nil, logger)
	firedAt := time.Date(2026, time.August, 5, 12, 3, 0, 0, time.UTC)
	service.now = func() time.Time { return firedAt }
	service.delay = func(int) time.Duration { return time.Second }
	waitStarted := make(chan struct{})
	releaseWait := make(chan struct{})
	service.wait = func(ctx context.Context, _ time.Duration) bool {
		close(waitStarted)
		select {
		case <-ctx.Done():
			return false
		case <-releaseWait:
			return true
		}
	}
	queued := make(chan struct{}, 1)
	service.queue = func(context.Context, string, models.TriggerType, *time.Time) (models.Result, error) {
		queued <- struct{}{}
		return models.Result{}, nil
	}

	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	staleGeneration := service.generations[task.ID]
	service.mu.Unlock()
	done := make(chan struct{})
	go func() {
		service.fire(task.ID, staleGeneration)
		close(done)
	}()
	<-waitStarted

	// This is deliberately the stale task snapshot held by an API request that
	// began before the callback persisted firedAt.
	task.CronExpression = "*/5 * * * *"
	if err := store.UpdateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	close(releaseWait)
	<-done

	select {
	case <-queued:
		t.Fatal("stale jitter callback was queued after reschedule")
	default:
	}
	loaded, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastScheduledAt == nil || !loaded.LastScheduledAt.Equal(firedAt) {
		t.Fatalf("last scheduled time was overwritten by stale snapshot: %v", loaded.LastScheduledAt)
	}
	expectedNext := time.Date(2026, time.August, 5, 12, 5, 0, 0, time.UTC)
	if loaded.NextScheduledAt == nil || !loaded.NextScheduledAt.Equal(expectedNext) {
		t.Fatalf("next scheduled time = %v, want %v", loaded.NextScheduledAt, expectedNext)
	}
}

func TestRemoveInvalidatesPendingJitter(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := testTask()
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	service := New(store, nil, nil, logger)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	}
	service.delay = func(int) time.Duration { return time.Second }
	waitStarted := make(chan struct{})
	releaseWait := make(chan struct{})
	service.wait = func(ctx context.Context, _ time.Duration) bool {
		close(waitStarted)
		select {
		case <-ctx.Done():
			return false
		case <-releaseWait:
			return true
		}
	}
	queued := make(chan struct{}, 1)
	service.queue = func(context.Context, string, models.TriggerType, *time.Time) (models.Result, error) {
		queued <- struct{}{}
		return models.Result{}, nil
	}
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	generation := service.generations[task.ID]
	service.mu.Unlock()
	done := make(chan struct{})
	go func() {
		service.fire(task.ID, generation)
		close(done)
	}()
	<-waitStarted
	if err := service.Remove(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	close(releaseWait)
	<-done
	select {
	case <-queued:
		t.Fatal("stale jitter callback was queued after removal")
	default:
	}
}

func TestStaleCronGenerationCannotMutateSchedule(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := testTask()
	task.RandomJitterSeconds = 0
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	service := New(store, nil, nil, logger)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 5, 14, 0, 0, 0, time.UTC)
	}
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	staleGeneration := service.generations[task.ID]
	service.mu.Unlock()
	task.CronExpression = "*/10 * * * *"
	if err := store.UpdateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	service.fire(task.ID, staleGeneration)
	after, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.LastScheduledAt != nil || after.LastScheduledAt != nil {
		t.Fatalf("stale callback changed last scheduled time from %v to %v", before.LastScheduledAt, after.LastScheduledAt)
	}
	if before.NextScheduledAt == nil || after.NextScheduledAt == nil || !before.NextScheduledAt.Equal(*after.NextScheduledAt) {
		t.Fatalf("stale callback changed next scheduled time from %v to %v", before.NextScheduledAt, after.NextScheduledAt)
	}
}

func TestOutOfOrderCallbackCannotMoveScheduleMetadataBackward(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := testTask()
	task.RandomJitterSeconds = 0
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	service := New(store, nil, nil, logger)
	newer := time.Date(2026, time.August, 5, 15, 2, 0, 0, time.UTC)
	older := newer.Add(-time.Minute)
	service.now = func() time.Time { return newer }
	queued := make(chan time.Time, 2)
	service.queue = func(_ context.Context, _ string, _ models.TriggerType, scheduledAt *time.Time) (models.Result, error) {
		queued <- *scheduledAt
		return models.Result{}, nil
	}
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	generation := service.generations[task.ID]
	service.mu.Unlock()
	service.fire(task.ID, generation)
	if got := <-queued; !got.Equal(newer) {
		t.Fatalf("first queued time = %v, want %v", got, newer)
	}

	service.now = func() time.Time { return older }
	service.fire(task.ID, generation)
	select {
	case got := <-queued:
		t.Fatalf("out-of-order callback was queued at %v", got)
	default:
	}
	loaded, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastScheduledAt == nil || !loaded.LastScheduledAt.Equal(newer) {
		t.Fatalf("last scheduled time moved backward: %v", loaded.LastScheduledAt)
	}
	expectedNext := newer.Add(time.Minute)
	if loaded.NextScheduledAt == nil || !loaded.NextScheduledAt.Equal(expectedNext) {
		t.Fatalf("next scheduled time changed to %v, want %v", loaded.NextScheduledAt, expectedNext)
	}
}

func TestPersistedLocalTimePreventsDSTDuplicateAfterRestart(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := testTask()
	task.CronExpression = "30 2 * * *"
	task.Timezone = "Europe/Berlin"
	task.RandomJitterSeconds = 0
	firstOccurrence := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	secondOccurrence := time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC)
	task.LastScheduledAt = &firstOccurrence
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}

	// A new Scheduler has no in-memory localFire state. The persisted first
	// 02:30 occurrence must still suppress the second 02:30 after fallback.
	service := New(store, nil, nil, logger)
	service.now = func() time.Time { return secondOccurrence }
	queued := make(chan struct{}, 1)
	service.queue = func(context.Context, string, models.TriggerType, *time.Time) (models.Result, error) {
		queued <- struct{}{}
		return models.Result{}, nil
	}
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	generation := service.generations[task.ID]
	service.mu.Unlock()
	service.fire(task.ID, generation)
	select {
	case <-queued:
		t.Fatal("duplicate DST local time was queued after scheduler restart")
	default:
	}
	loaded, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastScheduledAt == nil || !loaded.LastScheduledAt.Equal(firstOccurrence) {
		t.Fatalf("persisted last scheduled time changed: %v", loaded.LastScheduledAt)
	}
}

func TestScheduledFireCompletesThroughExecutionLifecycle(t *testing.T) {
	store, ctx, logger := testStore(t)
	task := testTask()
	task.RandomJitterSeconds = 0
	if err := store.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	broker := events.New()
	t.Cleanup(broker.Close)
	provider := &schedulerLifecycleProvider{}
	manager := execution.New(store, providers.NewRegistry(provider), nil, schedulerLifecycleRouteValidator{}, broker, logger, "test")
	manager.Start()
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Stop(stopContext)
	})
	service := New(store, manager, broker, logger)
	firedAt := time.Date(2026, time.August, 5, 16, 20, 0, 0, time.UTC)
	service.now = func() time.Time { return firedAt }
	service.delay = func(int) time.Duration { return 0 }
	if err := service.Reschedule(ctx, task); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	generation := service.generations[task.ID]
	service.mu.Unlock()
	service.fire(task.ID, generation)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		page, err := store.ListResults(ctx, database.ResultFilter{TaskID: task.ID, Page: 1, PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 && page.Items[0].Status == models.StatusSucceeded {
			result, err := store.GetResult(ctx, page.Items[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Trigger != models.TriggerScheduled || result.ScheduledAt == nil || !result.ScheduledAt.Equal(firedAt) || result.DownloadBitsPerSecond == nil || *result.DownloadBitsPerSecond != 80_000_000 {
				t.Fatalf("scheduled result=%+v", result)
			}
			if provider.runs.Load() != 1 {
				t.Fatalf("provider runs=%d", provider.runs.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scheduled execution did not reach a successful terminal result")
}

type schedulerLifecycleRouteValidator struct{}

func (schedulerLifecycleRouteValidator) Validate(_ context.Context, profile models.RouteProfile) models.RouteValidation {
	return models.RouteValidation{Success: true, Reachable: true, InterfaceName: profile.InterfaceName, SourceIP: profile.SourceIP, Destination: profile.ValidationTarget, ValidatedAt: time.Now().UTC(), Message: "validated"}
}

type schedulerLifecycleProvider struct{ runs atomic.Int32 }

func (*schedulerLifecycleProvider) ID() models.ProviderID { return models.ProviderCloudflare }
func (*schedulerLifecycleProvider) DisplayName() string   { return "Scheduler lifecycle provider" }
func (*schedulerLifecycleProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SourceAddressBinding: true}
}
func (*schedulerLifecycleProvider) Availability(context.Context) providers.Availability {
	return providers.Availability{Available: true, Version: "test"}
}
func (*schedulerLifecycleProvider) ListServers(context.Context, providers.ServerListRequest) ([]providers.Server, error) {
	return nil, nil
}
func (*schedulerLifecycleProvider) Validate(context.Context, providers.TestTarget) error { return nil }
func (*schedulerLifecycleProvider) Version(context.Context) (string, error)              { return "test", nil }
func (provider *schedulerLifecycleProvider) Run(context.Context, providers.RunRequest) (providers.ProviderResult, error) {
	provider.runs.Add(1)
	download, upload, latency := int64(80_000_000), int64(20_000_000), 8.5
	return providers.ProviderResult{DownloadBitsPerSecond: &download, UploadBitsPerSecond: &upload, LatencyMilliseconds: &latency, ProviderVersion: "test"}, nil
}
