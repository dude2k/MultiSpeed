// Package execution owns the shared manual and scheduled run lifecycle.
package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/dude2k/MultiSpeed/internal/network"
	"github.com/dude2k/MultiSpeed/internal/providers"
)

const queueCapacity = 64

type job struct {
	resultID     string
	task         models.Task
	routeProfile *models.RouteProfile
}

type routeValidator interface {
	Validate(context.Context, models.RouteProfile) models.RouteValidation
}

type Manager struct {
	store      *database.Store
	registry   *providers.Registry
	interfaces *network.InterfaceService
	routes     routeValidator
	broker     *events.Broker
	logger     *slog.Logger
	version    string
	queue      chan job
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	limiter    *runLimiter
	runningMu  sync.Mutex
	running    map[string]context.CancelFunc
	queueMu    sync.Mutex
}

func New(store *database.Store, registry *providers.Registry, interfaces *network.InterfaceService, routes routeValidator, broker *events.Broker, logger *slog.Logger, version string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{store: store, registry: registry, interfaces: interfaces, routes: routes, broker: broker, logger: logger, version: version,
		queue: make(chan job, queueCapacity), ctx: ctx, cancel: cancel, limiter: newRunLimiter(), running: make(map[string]context.CancelFunc)}
}

func (m *Manager) Start() {
	for worker := 0; worker < 16; worker++ {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			for {
				select {
				case <-m.ctx.Done():
					return
				case work := <-m.queue:
					m.execute(work)
				}
			}
		}()
	}
}

func (m *Manager) Stop(ctx context.Context) error {
	m.cancel()
	m.runningMu.Lock()
	for _, cancel := range m.running {
		cancel()
	}
	m.runningMu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Queue(ctx context.Context, taskID string, trigger models.TriggerType, scheduledAt *time.Time) (models.Result, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return models.Result{}, err
	}
	if trigger == models.TriggerScheduled && !task.Enabled {
		return models.Result{}, errors.New("disabled task cannot be scheduled")
	}
	var routeProfile *models.RouteProfile
	if task.RouteProfileID != nil {
		profile, profileErr := m.store.GetRouteProfile(ctx, *task.RouteProfileID)
		if profileErr != nil {
			return models.Result{}, fmt.Errorf("load route profile: %w", profileErr)
		}
		routeProfile = &profile
	}
	task.CustomServerDefinition = cloneOptions(task.CustomServerDefinition)
	task.ProviderOptions = cloneOptions(task.ProviderOptions)
	queuedAt := time.Now().UTC()
	result := models.Result{TaskID: task.ID, RouteProfileID: task.RouteProfileID, Trigger: trigger, Provider: task.Provider, ScheduledAt: scheduledAt,
		QueuedAt: queuedAt, Status: models.StatusQueued, SelectedInterface: task.InterfaceName, SelectedSourceIP: task.SourceIP, ApplicationVersion: m.version,
		RouteValidationSnapshot: map[string]any{}}
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if task.PreventOverlap {
		active, activeErr := m.store.TaskHasActiveResults(ctx, task.ID)
		if activeErr != nil {
			return models.Result{}, activeErr
		}
		if active {
			finished := time.Now().UTC()
			result.Status = models.StatusSkipped
			result.FinishedAt = &finished
			result.SanitizedError = "A previous execution of this task is still active and prevent-overlap is enabled."
			if err := m.store.CreateResult(ctx, &result); err != nil {
				return models.Result{}, err
			}
			m.broker.Publish("test.skipped", result)
			m.broker.Publish("result.stored", result)
			return result, nil
		}
	}
	if err := m.store.CreateResult(ctx, &result); err != nil {
		return models.Result{}, err
	}
	select {
	case m.queue <- job{resultID: result.ID, task: task, routeProfile: routeProfile}:
		m.broker.Publish("run.queued", map[string]any{"taskId": task.ID, "resultId": result.ID, "trigger": trigger})
		return result, nil
	default:
		now := time.Now().UTC()
		result.Status = models.StatusSkipped
		result.FinishedAt = &now
		result.SanitizedError = "The bounded execution queue is full."
		if err := m.persistTerminal(&result); err != nil {
			return result, fmt.Errorf("execution queue is full and its skipped state could not be persisted: %w", err)
		}
		m.broker.Publish("test.skipped", result)
		return result, errors.New("execution queue is full")
	}
}

func (m *Manager) Cancel(resultID string) bool {
	m.runningMu.Lock()
	defer m.runningMu.Unlock()
	cancel, ok := m.running[resultID]
	if ok {
		cancel()
	}
	return ok
}

func (m *Manager) execute(work job) {
	ctx := m.ctx
	task := work.task
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		m.failUnloaded(work.resultID, "Application settings could not be loaded.")
		return
	}
	wanKey := wanPathKey(task.InterfaceName, task.SourceIP)
	if err := m.limiter.Acquire(ctx, task.ID, wanKey, settings.GlobalConcurrency, settings.AllowSeparateWANConcurrency); err != nil {
		m.failUnloaded(work.resultID, "Execution was cancelled while waiting for capacity.")
		return
	}
	defer m.limiter.Release(task.ID, wanKey)

	timeout := time.Duration(task.TimeoutSeconds) * time.Second
	if timeout < 5*time.Second {
		timeout = time.Duration(settings.DefaultTaskTimeout) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	m.runningMu.Lock()
	m.running[work.resultID] = cancel
	m.runningMu.Unlock()
	defer func() { m.runningMu.Lock(); delete(m.running, work.resultID); m.runningMu.Unlock() }()

	result, err := m.store.GetResult(runCtx, work.resultID)
	if err != nil {
		m.failUnloaded(work.resultID, "The queued result could not be loaded for execution.")
		return
	}
	started := time.Now().UTC()
	result.StartedAt = &started
	result.Status = models.StatusValidating
	if err := m.store.UpdateResult(runCtx, &result); err != nil {
		m.finishFailure(&result, models.StatusFailed, "The validating state could not be persisted.")
		return
	}
	m.broker.Publish("route.validation.started", map[string]any{"taskId": task.ID, "resultId": result.ID})

	profile, err := RouteProfileForTask(task, work.routeProfile)
	if err != nil {
		m.finishFailure(&result, models.StatusFailed, err.Error())
		return
	}
	validation := m.routes.Validate(runCtx, profile)
	result.RouteValidationSnapshot = routeSnapshot(validation)
	result.DetectedPublicIP = validation.DetectedPublicIP
	m.broker.Publish("route.validation.completed", map[string]any{"taskId": task.ID, "resultId": result.ID, "validation": validation})
	if !validation.Success {
		m.finishFailure(&result, models.StatusFailed, "Route validation failed: "+validation.Message)
		return
	}

	provider, err := m.registry.Get(task.Provider)
	if err != nil {
		m.finishFailure(&result, models.StatusFailed, err.Error())
		return
	}
	availability := provider.Availability(runCtx)
	if !availability.Available {
		m.finishFailure(&result, models.StatusSkipped, availability.Message)
		return
	}
	result.Status = models.StatusRunning
	if err := m.store.UpdateResult(runCtx, &result); err != nil {
		m.finishFailure(&result, models.StatusFailed, "The running state could not be persisted.")
		return
	}
	m.broker.Publish("test.started", map[string]any{"taskId": task.ID, "resultId": result.ID, "provider": task.Provider})

	providerResult, runErr := provider.Run(runCtx, providers.RunRequest{TaskID: task.ID, InterfaceName: task.InterfaceName, SourceIP: task.SourceIP,
		IPFamily: task.IPFamily, Target: providers.TestTarget{SelectionMode: task.ServerSelectionMode, ServerID: task.ServerID, ServerURL: task.ServerURL,
			CustomServerDefinition: task.CustomServerDefinition}, TimeoutSeconds: task.TimeoutSeconds, Options: task.ProviderOptions})
	if runErr == nil {
		if err := validateProviderResult(providerResult); err != nil {
			m.finishFailure(&result, models.StatusFailed, "Provider returned invalid metrics: "+err.Error())
			return
		}
	} else {
		providerResult = safePartialProviderResult(providerResult)
	}
	applyProviderResult(&result, providerResult)
	if runErr != nil {
		status := models.StatusFailed
		if errors.Is(runCtx.Err(), context.Canceled) {
			status = models.StatusCancelled
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			runErr = fmt.Errorf("test exceeded its %s timeout: %w", timeout, runErr)
		}
		m.finishFailure(&result, status, runErr.Error())
		return
	}
	finished := time.Now().UTC()
	result.FinishedAt = &finished
	result.Status = models.StatusSucceeded
	if result.ExecutionDurationMS == 0 {
		result.ExecutionDurationMS = finished.Sub(started).Milliseconds()
	}
	if err := m.persistTerminal(&result); err != nil {
		m.logger.Error("persist completed result", "task_id", task.ID, "result_id", result.ID, "error", err)
		return
	}
	m.logger.Info("speed test completed", "task_id", task.ID, "result_id", result.ID, "provider", task.Provider, "duration_ms", result.ExecutionDurationMS)
	m.broker.Publish("test.completed", result)
	m.broker.Publish("result.stored", result)
}

func wanPathKey(interfaceName, sourceIP string) string {
	if parsed := net.ParseIP(sourceIP); parsed != nil {
		sourceIP = parsed.String()
	}
	return interfaceName + "\x00" + sourceIP
}

func (m *Manager) finishFailure(result *models.Result, status models.ResultStatus, message string) {
	now := time.Now().UTC()
	result.FinishedAt = &now
	result.Status = status
	result.SanitizedError = providers.SanitizeOutput(message, 8192)
	if result.StartedAt != nil && result.ExecutionDurationMS == 0 {
		result.ExecutionDurationMS = now.Sub(*result.StartedAt).Milliseconds()
	}
	if err := m.persistTerminal(result); err != nil {
		m.logger.Error("persist failed result", "result_id", result.ID, "error", err)
		return
	}
	event := "test.failed"
	if status == models.StatusSkipped {
		event = "test.skipped"
	}
	if status == models.StatusCancelled {
		event = "test.cancelled"
	}
	m.logger.Warn("speed test ended without success", "task_id", result.TaskID, "result_id", result.ID, "status", status, "error", result.SanitizedError)
	m.broker.Publish(event, *result)
	m.broker.Publish("result.stored", *result)
}

func (m *Manager) persistTerminal(result *models.Result) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.store.UpdateResult(ctx, result)
}

func (m *Manager) failUnloaded(resultID, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := m.store.GetResult(ctx, resultID)
	if err == nil {
		m.finishFailure(&result, models.StatusFailed, message)
	}
}

// RouteProfileForTask creates the immutable effective profile used by both
// validation-only requests and real executions.
func RouteProfileForTask(task models.Task, stored *models.RouteProfile) (models.RouteProfile, error) {
	profile := models.RouteProfile{InterfaceName: task.InterfaceName, SourceIP: task.SourceIP, ValidationTarget: familyValidationTarget(task.SourceIP)}
	if task.RouteProfileID != nil {
		if stored == nil || stored.ID != *task.RouteProfileID {
			return models.RouteProfile{}, errors.New("the selected route profile no longer exists")
		}
		profile = *stored
		if profile.InterfaceName != task.InterfaceName || !sameIP(profile.SourceIP, task.SourceIP) {
			return models.RouteProfile{}, errors.New("the route profile interface/source does not match the task")
		}
	}
	if task.RouteValidation == "interface-only" {
		profile.ExpectedGateway = ""
		profile.ExpectedRoutingTable = ""
	}
	return profile, nil
}

func sameIP(first, second string) bool {
	firstIP, secondIP := net.ParseIP(first), net.ParseIP(second)
	return firstIP != nil && secondIP != nil && firstIP.Equal(secondIP)
}

func cloneOptions(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func validateProviderResult(value providers.ProviderResult) error {
	for name, metric := range map[string]*int64{
		"download throughput": value.DownloadBitsPerSecond,
		"upload throughput":   value.UploadBitsPerSecond,
		"download bytes":      value.DownloadBytes,
		"upload bytes":        value.UploadBytes,
	} {
		if metric != nil && *metric < 0 {
			return fmt.Errorf("%s is negative", name)
		}
	}
	for name, metric := range map[string]*float64{
		"latency": value.LatencyMilliseconds,
		"jitter":  value.JitterMilliseconds,
	} {
		if metric != nil && (*metric < 0 || math.IsNaN(*metric) || math.IsInf(*metric, 0)) {
			return fmt.Errorf("%s is not a finite non-negative number", name)
		}
	}
	if value.PacketLossPercent != nil && (*value.PacketLossPercent < 0 || *value.PacketLossPercent > 100 || math.IsNaN(*value.PacketLossPercent) || math.IsInf(*value.PacketLossPercent, 0)) {
		return errors.New("packet loss is outside 0 through 100 percent")
	}
	if value.DurationMilliseconds < 0 {
		return errors.New("execution duration is negative")
	}
	if value.DownloadBitsPerSecond == nil || value.UploadBitsPerSecond == nil || value.LatencyMilliseconds == nil {
		return errors.New("download, upload, and latency metrics are required")
	}
	return nil
}

func safePartialProviderResult(value providers.ProviderResult) providers.ProviderResult {
	for _, metric := range []**int64{&value.DownloadBitsPerSecond, &value.UploadBitsPerSecond, &value.DownloadBytes, &value.UploadBytes} {
		if *metric != nil && **metric < 0 {
			*metric = nil
		}
	}
	for _, metric := range []**float64{&value.LatencyMilliseconds, &value.JitterMilliseconds} {
		if *metric != nil && (**metric < 0 || math.IsNaN(**metric) || math.IsInf(**metric, 0)) {
			*metric = nil
		}
	}
	if value.PacketLossPercent != nil && (*value.PacketLossPercent < 0 || *value.PacketLossPercent > 100 || math.IsNaN(*value.PacketLossPercent) || math.IsInf(*value.PacketLossPercent, 0)) {
		value.PacketLossPercent = nil
	}
	if value.DurationMilliseconds < 0 {
		value.DurationMilliseconds = 0
	}
	return value
}

func applyProviderResult(result *models.Result, value providers.ProviderResult) {
	result.DownloadBitsPerSecond = value.DownloadBitsPerSecond
	result.UploadBitsPerSecond = value.UploadBitsPerSecond
	result.LatencyMilliseconds = value.LatencyMilliseconds
	result.JitterMilliseconds = value.JitterMilliseconds
	result.PacketLossPercent = value.PacketLossPercent
	result.DownloadBytes = value.DownloadBytes
	result.UploadBytes = value.UploadBytes
	if value.PublicIP != "" {
		result.DetectedPublicIP = value.PublicIP
	}
	result.ServerID = value.Server.ID
	result.ServerName = value.Server.Name
	result.ServerHost = value.Server.Host
	result.ServerSponsor = value.Server.Sponsor
	result.ServerLocation = value.Server.Location
	result.ServerCountry = value.Server.Country
	result.ProviderResultURL = value.ResultURL
	result.CloudflareColo = value.CloudflareColo
	result.ExecutionDurationMS = value.DurationMilliseconds
	result.ProcessExitCode = value.ExitCode
	result.RawProviderResponse = providers.SanitizeOutput(value.RawResponse, providers.MaxStoredOutput)
	result.ProviderVersion = value.ProviderVersion
	result.TLSVerificationDisabled = value.TLSVerificationDisabled
}

func familyValidationTarget(source string) string {
	ip := net.ParseIP(source)
	if ip != nil && ip.To4() == nil {
		return "2606:4700:4700::1111"
	}
	return "1.1.1.1"
}
func routeSnapshot(value models.RouteValidation) map[string]any {
	raw := map[string]any{"success": value.Success, "interfaceName": value.InterfaceName, "sourceIp": value.SourceIP, "gateway": value.Gateway, "routingTable": value.RoutingTable, "destination": value.Destination, "detectedPublicIp": value.DetectedPublicIP, "reachable": value.Reachable, "durationMs": value.DurationMS, "validatedAt": value.ValidatedAt, "message": value.Message}
	return raw
}

type runLimiter struct {
	mu           sync.Mutex
	runningTotal int
	tasks        map[string]bool
	wans         map[string]bool
	changed      chan struct{}
}

func newRunLimiter() *runLimiter {
	return &runLimiter{tasks: map[string]bool{}, wans: map[string]bool{}, changed: make(chan struct{})}
}
func (l *runLimiter) Acquire(ctx context.Context, taskID, wan string, globalLimit int, separateWAN bool) error {
	if globalLimit < 1 {
		globalLimit = 1
	}
	if globalLimit > 16 {
		globalLimit = 16
	}
	for {
		l.mu.Lock()
		allowed := !l.tasks[taskID] && !l.wans[wan] && l.runningTotal < globalLimit && (separateWAN || l.runningTotal == 0)
		if allowed {
			l.tasks[taskID] = true
			l.wans[wan] = true
			l.runningTotal++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (l *runLimiter) Release(taskID, wan string) {
	l.mu.Lock()
	delete(l.tasks, taskID)
	delete(l.wans, wan)
	if l.runningTotal > 0 {
		l.runningTotal--
	}
	close(l.changed)
	l.changed = make(chan struct{})
	l.mu.Unlock()
}
