package execution

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
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
	"github.com/dude2k/MultiSpeed/internal/providers/librespeed"
	providerprocess "github.com/dude2k/MultiSpeed/internal/providers/process"
)

type fakeRouteValidator struct{ success bool }

func (f fakeRouteValidator) Validate(context.Context, models.RouteProfile) models.RouteValidation {
	return models.RouteValidation{Success: f.success, InterfaceName: "eth0", SourceIP: "192.0.2.10", DetectedPublicIP: "203.0.113.10", Reachable: f.success, ValidatedAt: time.Now().UTC(), Message: map[bool]string{true: "ok", false: "mismatch"}[f.success]}
}

type fakeProvider struct {
	runs     atomic.Int32
	delay    time.Duration
	requests chan providers.RunRequest
	runErr   error
}

type availabilityOnlyLibreSpeedRunner struct {
	versionCalls   atomic.Int32
	executionCalls atomic.Int32
}

func (r *availabilityOnlyLibreSpeedRunner) Run(_ context.Context, request providerprocess.Request) (providerprocess.Result, error) {
	if len(request.Arguments) == 1 && request.Arguments[0] == "--version" {
		r.versionCalls.Add(1)
		return providerprocess.Result{Stdout: "librespeed-cli v1.0.13+multispeed.dns2.xnet056"}, nil
	}
	r.executionCalls.Add(1)
	return providerprocess.Result{}, errors.New("unexpected LibreSpeed test execution")
}

func (*fakeProvider) ID() models.ProviderID { return models.ProviderCloudflare }
func (*fakeProvider) DisplayName() string   { return "Fake" }
func (*fakeProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SourceAddressBinding: true}
}
func (*fakeProvider) Availability(context.Context) providers.Availability {
	return providers.Availability{Available: true, Version: "test"}
}
func (*fakeProvider) ListServers(context.Context, providers.ServerListRequest) ([]providers.Server, error) {
	return nil, nil
}
func (*fakeProvider) Validate(context.Context, providers.TestTarget) error { return nil }
func (*fakeProvider) Version(context.Context) (string, error)              { return "test", nil }
func (f *fakeProvider) Run(ctx context.Context, request providers.RunRequest) (providers.ProviderResult, error) {
	f.runs.Add(1)
	if f.requests != nil {
		f.requests <- request
	}
	select {
	case <-ctx.Done():
		return providers.ProviderResult{}, ctx.Err()
	case <-time.After(f.delay):
	}
	if f.runErr != nil {
		exitCode := 17
		return providers.ProviderResult{ExitCode: &exitCode, RawResponse: "partial provider output\x00"}, f.runErr
	}
	down := int64(100_000_000)
	up := int64(50_000_000)
	latency := 10.0
	return providers.ProviderResult{DownloadBitsPerSecond: &down, UploadBitsPerSecond: &up, LatencyMilliseconds: &latency, ProviderVersion: "test"}, nil
}

func TestPreventOverlapSkipsWhileDisabledPolicyQueues(t *testing.T) {
	t.Run("enabled skips", func(t *testing.T) {
		provider := &fakeProvider{delay: 100 * time.Millisecond}
		manager, store, task := testManager(t, true, provider)
		first, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
		if err != nil {
			t.Fatal(err)
		}
		second, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
		if err != nil {
			t.Fatal(err)
		}
		if second.Status != models.StatusSkipped {
			t.Fatalf("second status=%s", second.Status)
		}
		_ = waitForTerminal(t, store, first.ID)
		if provider.runs.Load() != 1 {
			t.Fatalf("provider runs=%d", provider.runs.Load())
		}
	})

	t.Run("disabled queues", func(t *testing.T) {
		provider := &fakeProvider{delay: 50 * time.Millisecond}
		manager, store, task := testManager(t, true, provider)
		task.PreventOverlap = false
		if err := store.UpdateTask(context.Background(), &task); err != nil {
			t.Fatal(err)
		}
		first, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
		if err != nil {
			t.Fatal(err)
		}
		second, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
		if err != nil {
			t.Fatal(err)
		}
		if second.Status != models.StatusQueued {
			t.Fatalf("second status=%s", second.Status)
		}
		_ = waitForTerminal(t, store, first.ID)
		_ = waitForTerminal(t, store, second.ID)
		if provider.runs.Load() != 2 {
			t.Fatalf("provider runs=%d", provider.runs.Load())
		}
	})
}

func TestQueuedJobUsesImmutableTaskSnapshot(t *testing.T) {
	requests := make(chan providers.RunRequest, 2)
	provider := &fakeProvider{delay: 150 * time.Millisecond, requests: requests}
	manager, store, blocker := testManager(t, true, provider)
	blocker.PreventOverlap = false
	if err := store.UpdateTask(context.Background(), &blocker); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Queue(context.Background(), blocker.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("first provider run did not start")
	}
	target := blocker
	target.ID = ""
	target.Name = "Snapshot target"
	target.SourceIP = "192.0.2.20"
	if err := store.CreateTask(context.Background(), &target); err != nil {
		t.Fatal(err)
	}
	queued, err := manager.Queue(context.Background(), target.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	target.SourceIP = "192.0.2.99"
	if err := store.UpdateTask(context.Background(), &target); err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, store, first.ID)
	_ = waitForTerminal(t, store, queued.ID)
	select {
	case request := <-requests:
		if request.SourceIP != "192.0.2.20" {
			t.Fatalf("queued request used edited source %q", request.SourceIP)
		}
	case <-time.After(time.Second):
		t.Fatal("queued provider run did not start")
	}
	result, err := store.GetResult(context.Background(), queued.ID)
	if err != nil || result.SelectedSourceIP != "192.0.2.20" {
		t.Fatalf("result snapshot=%#v error=%v", result, err)
	}
}

func testManager(t *testing.T, routeOK bool, provider *fakeProvider) (*Manager, *database.Store, models.Task) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	task := models.Task{Name: "Test", Enabled: true, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC", ServerSelectionMode: "automatic", InterfaceName: "eth0", SourceIP: "192.0.2.10", IPFamily: "ipv4", TimeoutSeconds: 5, PreventOverlap: true, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	broker := events.New()
	manager := New(store, providers.NewRegistry(provider), nil, fakeRouteValidator{success: routeOK}, broker, logger, "test")
	manager.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Stop(ctx)
		broker.Close()
	})
	return manager, store, task
}

func TestManualRunLifecycleSucceeds(t *testing.T) {
	provider := &fakeProvider{}
	manager, store, task := testManager(t, true, provider)
	queued, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTerminal(t, store, queued.ID)
	if result.Status != models.StatusSucceeded || result.DownloadBitsPerSecond == nil || *result.DownloadBitsPerSecond != 100_000_000 {
		t.Fatalf("result=%#v", result)
	}
	if provider.runs.Load() != 1 {
		t.Fatalf("runs=%d", provider.runs.Load())
	}
}

func TestManualRunFailsStoredLibreSpeedCustomTaskRemovedFromCurrentAllowlist(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "removed-custom-server.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const removedURL = "https://removed-policy-target.invalid/backend"
	task := models.Task{
		Name: "Previously authorized custom LibreSpeed", Enabled: true, Provider: models.ProviderLibreSpeed,
		CronExpression: "0 * * * *", Timezone: "UTC", ServerSelectionMode: "custom", ServerURL: removedURL,
		InterfaceName: "eth0", SourceIP: "192.0.2.10", IPFamily: "ipv4", TimeoutSeconds: 5,
		PreventOverlap: true, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{},
	}
	if err := store.CreateTask(context.Background(), &task); err != nil {
		t.Fatal(err)
	}

	policy, err := providers.NewCustomServerURLPolicy([]string{"https://currently-authorized.example.test/backend"})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := &availabilityOnlyLibreSpeedRunner{}
	adapter := librespeed.NewWithCustomServerURLPolicy(binary, runner, policy)
	broker := events.New()
	manager := New(store, providers.NewRegistry(adapter), nil, fakeRouteValidator{success: true}, broker, logger, "test")
	manager.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Stop(ctx)
		broker.Close()
	})

	queued, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTerminal(t, store, queued.ID)
	if result.Status != models.StatusFailed {
		t.Fatalf("status=%s, want %s; error=%q", result.Status, models.StatusFailed, result.SanitizedError)
	}
	if !strings.Contains(strings.ToLower(result.SanitizedError), "not authorized by the deployment allowlist") {
		t.Fatalf("failure does not explain the current allowlist policy: %q", result.SanitizedError)
	}
	for _, unexpected := range []string{"dns", "lookup", "resolve"} {
		if strings.Contains(strings.ToLower(result.SanitizedError), unexpected) {
			t.Fatalf("failure indicates DNS work before authorization (%q): %q", unexpected, result.SanitizedError)
		}
	}
	if runner.executionCalls.Load() != 0 {
		t.Fatalf("LibreSpeed test execution calls=%d, want 0", runner.executionCalls.Load())
	}
	if runner.versionCalls.Load() == 0 {
		t.Fatal("expected the fixed LibreSpeed availability/version check")
	}
	if result.ProcessExitCode != nil || result.RawProviderResponse != "" {
		t.Fatalf("result contains diagnostics from an unexpected CLI test run: exit=%v raw=%q", result.ProcessExitCode, result.RawProviderResponse)
	}
}

func TestRouteFailureNeverRunsProvider(t *testing.T) {
	provider := &fakeProvider{}
	manager, store, task := testManager(t, false, provider)
	queued, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTerminal(t, store, queued.ID)
	if result.Status != models.StatusFailed {
		t.Fatalf("status=%s", result.Status)
	}
	if provider.runs.Load() != 0 {
		t.Fatalf("provider unexpectedly ran %d times", provider.runs.Load())
	}
}

func TestProviderFailurePersistsSanitizedTerminalResult(t *testing.T) {
	provider := &fakeProvider{runErr: errors.New("provider exploded\x00\x01")}
	manager, store, task := testManager(t, true, provider)
	queued, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTerminal(t, store, queued.ID)
	if result.Status != models.StatusFailed || !strings.Contains(result.SanitizedError, "provider exploded") || strings.ContainsAny(result.SanitizedError, "\x00\x01") {
		t.Fatalf("failed provider result=%+v", result)
	}
	if result.ProcessExitCode == nil || *result.ProcessExitCode != 17 || strings.ContainsRune(result.RawProviderResponse, '\x00') {
		t.Fatalf("partial provider diagnostics were not safely persisted: %+v", result)
	}
}

func TestCancellationInterruptsProviderAndPersistsCancelledResult(t *testing.T) {
	requests := make(chan providers.RunRequest, 1)
	provider := &fakeProvider{delay: time.Hour, requests: requests}
	manager, store, task := testManager(t, true, provider)
	queued, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("provider run did not start")
	}
	if !manager.Cancel(queued.ID) {
		t.Fatal("running result was not cancellable")
	}
	result := waitForTerminal(t, store, queued.ID)
	if result.Status != models.StatusCancelled || !strings.Contains(strings.ToLower(result.SanitizedError), "canceled") {
		t.Fatalf("cancelled result=%+v", result)
	}
}

func TestMissingInterfaceFailsBeforeProviderExecution(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "missing-interface.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	task := models.Task{Name: "Missing interface", Enabled: true, Provider: models.ProviderCloudflare, CronExpression: "0 * * * *", Timezone: "UTC", ServerSelectionMode: "automatic", InterfaceName: "wan-missing", SourceIP: "192.0.2.10", IPFamily: "ipv4", TimeoutSeconds: 5, PreventOverlap: true, RouteValidation: "required", CustomServerDefinition: map[string]any{}, ProviderOptions: map[string]any{}}
	if err := store.CreateTask(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	broker := events.New()
	interfaces := network.NewInterfaceServiceWithDiscoverer(broker, func(context.Context) ([]models.NetworkInterface, error) { return nil, nil })
	if _, err := interfaces.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{}
	manager := New(store, providers.NewRegistry(provider), interfaces, network.NewRouteValidator(interfaces), broker, logger, "test")
	manager.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Stop(ctx)
		broker.Close()
	})
	queued, err := manager.Queue(context.Background(), task.ID, models.TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := waitForTerminal(t, store, queued.ID)
	if result.Status != models.StatusFailed || !strings.Contains(result.SanitizedError, `network interface "wan-missing" does not exist`) || provider.runs.Load() != 0 {
		t.Fatalf("missing-interface result=%+v providerRuns=%d", result, provider.runs.Load())
	}
}
func waitForTerminal(t *testing.T, store *database.Store, id string) models.Result {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := store.GetResult(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == models.StatusSucceeded || result.Status == models.StatusFailed || result.Status == models.StatusSkipped || result.Status == models.StatusCancelled {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("result did not become terminal")
	return models.Result{}
}

func TestRunLimiterBlocksSameWANAndAllowsSeparateWAN(t *testing.T) {
	limiter := newRunLimiter()
	ctx := context.Background()
	firstWAN := wanPathKey("eth0", "2001:db8::1")
	equivalentWAN := wanPathKey("eth0", "2001:0db8:0:0:0:0:0:1")
	if firstWAN != equivalentWAN {
		t.Fatalf("equivalent IPv6 WAN keys differ: %q != %q", firstWAN, equivalentWAN)
	}
	if err := limiter.Acquire(ctx, "a", firstWAN, 2, true); err != nil {
		t.Fatal(err)
	}
	blockedCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := limiter.Acquire(blockedCtx, "b", equivalentWAN, 2, true); err == nil {
		t.Fatal("same WAN acquisition unexpectedly succeeded")
	}
	separateWAN := wanPathKey("eth1", "2001:db8::1")
	if err := limiter.Acquire(ctx, "b", separateWAN, 2, true); err != nil {
		t.Fatalf("separate WAN acquisition failed: %v", err)
	}
	limiter.Release("b", separateWAN)
	limiter.Release("a", firstWAN)
}
