// Package scheduler persists and manages one cron schedule per task.
package scheduler

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/events"
	"github.com/dude2k/MultiSpeed/internal/execution"
	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	store          *database.Store
	execution      *execution.Manager
	broker         *events.Broker
	logger         *slog.Logger
	cron           *cron.Cron
	parser         cron.Parser
	mu             sync.Mutex
	entries        map[string]cron.EntryID
	generations    map[string]uint64
	nextGeneration uint64
	localFire      map[string]string
	ctx            context.Context
	cancel         context.CancelFunc
	now            func() time.Time
	delay          func(int) time.Duration
	wait           func(context.Context, time.Duration) bool
	queue          func(context.Context, string, models.TriggerType, *time.Time) (models.Result, error)
}

func New(store *database.Store, execution *execution.Manager, broker *events.Broker, logger *slog.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	service := &Scheduler{
		store:       store,
		execution:   execution,
		broker:      broker,
		logger:      logger,
		cron:        cron.New(cron.WithParser(parser), cron.WithLocation(time.UTC), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		parser:      parser,
		entries:     map[string]cron.EntryID{},
		generations: map[string]uint64{},
		localFire:   map[string]string{},
		ctx:         ctx,
		cancel:      cancel,
		now:         time.Now,
		delay:       randomDelay,
		wait:        waitForDelay,
	}
	service.queue = func(ctx context.Context, taskID string, trigger models.TriggerType, scheduledAt *time.Time) (models.Result, error) {
		if service.execution == nil {
			return models.Result{}, errors.New("execution manager is unavailable")
		}
		return service.execution.Queue(ctx, taskID, trigger, scheduledAt)
	}
	return service
}

func (s *Scheduler) Start(ctx context.Context) error {
	tasks, err := s.store.ListEnabledTasks(ctx)
	if err != nil {
		return err
	}
	for i := range tasks {
		if err := s.Reschedule(ctx, tasks[i]); err != nil {
			return fmt.Errorf("schedule task %s: %w", tasks[i].ID, err)
		}
	}
	s.cron.Start()
	s.logger.Info("scheduler started", "jobs", len(tasks))
	return nil
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.cancel()
	cronCtx := s.cron.Stop()
	select {
	case <-cronCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) Reschedule(ctx context.Context, task models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rescheduleLocked(ctx, task)
}

// Reconcile atomically replaces the in-memory cron set with the supplied
// persisted tasks. Callers validate the complete set before invoking it.
func (s *Scheduler) Reconcile(ctx context.Context, tasks []models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for taskID, entryID := range s.entries {
		s.cron.Remove(entryID)
		s.nextGenerationLocked()
		delete(s.entries, taskID)
		delete(s.generations, taskID)
		delete(s.localFire, taskID)
	}
	for index := range tasks {
		if err := s.rescheduleLocked(ctx, tasks[index]); err != nil {
			return fmt.Errorf("reconcile task %s: %w", tasks[index].ID, err)
		}
	}
	return nil
}

func (s *Scheduler) rescheduleLocked(ctx context.Context, task models.Task) error {
	if existing, ok := s.entries[task.ID]; ok {
		s.cron.Remove(existing)
		delete(s.entries, task.ID)
	}
	// Remove the active generation before doing any other work. A callback that
	// is already running can still finish its read-only preparation, but it can
	// no longer persist schedule metadata or enqueue after this point.
	s.nextGenerationLocked()
	delete(s.generations, task.ID)

	current, err := s.store.GetTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("load current task schedule: %w", err)
	}
	// The database update and this call are not one transaction. Always schedule
	// the latest persisted configuration so two concurrent API updates cannot
	// leave cron memory reflecting whichever stale handler acquired s.mu last.
	task = current
	lastScheduledAt := task.LastScheduledAt
	if !task.Enabled {
		var none *time.Time
		return s.store.UpdateTaskSchedule(ctx, task.ID, lastScheduledAt, none)
	}
	location, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return fmt.Errorf("timezone %q: %w", task.Timezone, err)
	}
	parsedSchedule, err := s.parser.Parse(task.CronExpression)
	if err != nil {
		return fmt.Errorf("cron expression: %w", err)
	}
	expression := "CRON_TZ=" + task.Timezone + " " + task.CronExpression
	generation := s.nextGenerationLocked()
	s.generations[task.ID] = generation
	entryID, err := s.cron.AddFunc(expression, func() { s.fire(task.ID, generation) })
	if err != nil {
		delete(s.generations, task.ID)
		return err
	}
	s.entries[task.ID] = entryID
	next := parsedSchedule.Next(s.now().In(location)).UTC()
	if err := s.store.UpdateTaskSchedule(ctx, task.ID, lastScheduledAt, &next); err != nil {
		s.cron.Remove(entryID)
		delete(s.entries, task.ID)
		delete(s.generations, task.ID)
		return err
	}
	return nil
}

func (s *Scheduler) Remove(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.entries[taskID]; ok {
		s.cron.Remove(id)
		delete(s.entries, taskID)
	}
	s.nextGenerationLocked()
	delete(s.generations, taskID)
	delete(s.localFire, taskID)
	return nil
}

func (s *Scheduler) fire(taskID string, generation uint64) {
	if !s.isCurrentGeneration(taskID, generation) {
		return
	}
	task, err := s.store.GetTask(s.ctx, taskID)
	if err != nil || !task.Enabled {
		return
	}
	location, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return
	}
	parsedSchedule, err := s.parser.Parse(task.CronExpression)
	if err != nil {
		s.logger.Error("parse scheduled task cron", "task_id", taskID, "error", err)
		return
	}
	scheduled := s.now().UTC()
	localKey := scheduled.In(location).Format("2006-01-02T15:04")
	next := parsedSchedule.Next(scheduled.In(location)).UTC()

	s.mu.Lock()
	if !s.isCurrentGenerationLocked(taskID, generation) {
		s.mu.Unlock()
		return
	}
	current, err := s.store.GetTask(s.ctx, taskID)
	if err != nil {
		s.mu.Unlock()
		s.logger.Error("reload scheduled task metadata", "task_id", taskID, "error", err)
		return
	}
	storedLocalKey := ""
	if current.LastScheduledAt != nil {
		storedLocalKey = current.LastScheduledAt.In(location).Format("2006-01-02T15:04")
	}
	if s.localFire[taskID] == localKey || storedLocalKey == localKey {
		s.mu.Unlock()
		s.logger.Warn("duplicate local cron time skipped", "task_id", taskID, "local_time", localKey)
		return
	}
	if current.LastScheduledAt != nil && !scheduled.After(*current.LastScheduledAt) {
		s.mu.Unlock()
		s.logger.Warn("out-of-order cron callback skipped", "task_id", taskID, "scheduled_at", scheduled, "last_scheduled_at", *current.LastScheduledAt)
		return
	}
	s.localFire[taskID] = localKey
	last := scheduled
	if err := s.store.UpdateTaskSchedule(s.ctx, taskID, &last, &next); err != nil {
		s.mu.Unlock()
		s.logger.Error("persist scheduled task metadata", "task_id", taskID, "error", err)
		return
	}
	s.mu.Unlock()

	delay := s.delay(task.RandomJitterSeconds)
	if delay > 0 && !s.wait(s.ctx, delay) {
		return
	}

	// Keep the generation check and queue operation in the same critical
	// section. This closes the otherwise unavoidable check-then-enqueue race
	// with Remove and Reschedule.
	s.mu.Lock()
	if !s.isCurrentGenerationLocked(taskID, generation) {
		s.mu.Unlock()
		return
	}
	_, err = s.queue(s.ctx, taskID, models.TriggerScheduled, &scheduled)
	s.mu.Unlock()
	if err != nil {
		s.logger.Error("queue scheduled task", "task_id", taskID, "error", err)
	}
}

func (s *Scheduler) nextGenerationLocked() uint64 {
	s.nextGeneration++
	if s.nextGeneration == 0 {
		s.nextGeneration++
	}
	return s.nextGeneration
}

func (s *Scheduler) isCurrentGeneration(taskID string, generation uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isCurrentGenerationLocked(taskID, generation)
}

func (s *Scheduler) isCurrentGenerationLocked(taskID string, generation uint64) bool {
	current, ok := s.generations[taskID]
	return ok && current == generation
}

func (s *Scheduler) NextRuns(task models.Task, count int) ([]time.Time, error) {
	if count < 1 || count > 20 {
		return nil, errors.New("count must be between 1 and 20")
	}
	location, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return nil, err
	}
	schedule, err := s.parser.Parse(task.CronExpression)
	if err != nil {
		return nil, err
	}
	current := time.Now().In(location)
	result := make([]time.Time, 0, count)
	for range count {
		current = schedule.Next(current)
		result = append(result, current.UTC())
	}
	return result, nil
}

func randomDelay(maxSeconds int) time.Duration {
	if maxSeconds <= 0 {
		return 0
	}
	if maxSeconds > 3600 {
		maxSeconds = 3600
	}
	var data [8]byte
	if _, err := cryptorand.Read(data[:]); err != nil {
		return 0
	}
	return time.Duration(binary.LittleEndian.Uint64(data[:])%uint64(maxSeconds+1)) * time.Second
}

func waitForDelay(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
