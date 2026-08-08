// Package retention removes expired result history in short, bounded database
// transactions while preserving tasks and other configuration.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Mode string

const (
	ModeForever Mode = "forever"
	ModeDays    Mode = "days"
	ModeMonths  Mode = "months"
	ModeManual  Mode = "manual"
)

type Policy struct {
	Mode  Mode `json:"mode"`
	Value int  `json:"value"`
}

// Config bounds the work performed by one cleanup run. Zero values select
// conservative defaults.
type Config struct {
	BatchSize      int
	MaxBatches     int
	RunMaintenance bool
}

const (
	defaultBatchSize  = 500
	defaultMaxBatches = 100
	maximumBatchSize  = 5000
	maximumBatches    = 10000
)

// ResultStore is the minimum database capability needed for cleanup.
type ResultStore interface {
	DeleteResultsBefore(context.Context, time.Time, int) (int64, error)
}

type maintainer interface {
	Checkpoint(context.Context) error
	Optimize(context.Context) error
}

type Cleaner struct {
	store  ResultStore
	logger *slog.Logger
	config Config
}

// Metrics records the outcome of one bounded cleanup run.
type Metrics struct {
	Mode                 Mode          `json:"mode"`
	Cutoff               *time.Time    `json:"cutoff,omitempty"`
	StartedAt            time.Time     `json:"startedAt"`
	FinishedAt           time.Time     `json:"finishedAt"`
	Duration             time.Duration `json:"duration"`
	Batches              int           `json:"batches"`
	DeletedResults       int64         `json:"deletedResults"`
	LimitReached         bool          `json:"limitReached"`
	MaintenancePerformed bool          `json:"maintenancePerformed"`
}

func New(store ResultStore, logger *slog.Logger, config Config) (*Cleaner, error) {
	if store == nil {
		return nil, errors.New("retention result store is required")
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultBatchSize
	}
	if config.MaxBatches == 0 {
		config.MaxBatches = defaultMaxBatches
	}
	if config.BatchSize < 1 || config.BatchSize > maximumBatchSize {
		return nil, fmt.Errorf("retention batch size must be between 1 and %d", maximumBatchSize)
	}
	if config.MaxBatches < 1 || config.MaxBatches > maximumBatches {
		return nil, fmt.Errorf("retention max batches must be between 1 and %d", maximumBatches)
	}
	if config.RunMaintenance {
		if _, ok := store.(maintainer); !ok {
			return nil, errors.New("retention maintenance requested but store does not support it")
		}
	}
	return &Cleaner{store: store, logger: logger, config: config}, nil
}

// Cutoff returns the UTC expiration instant for a policy. Forever has no
// cutoff. Days and months use calendar arithmetic from the supplied instant.
func Cutoff(policy Policy, now time.Time) (*time.Time, error) {
	if now.IsZero() {
		return nil, errors.New("retention reference time is required")
	}
	now = now.UTC()
	switch policy.Mode {
	case ModeForever:
		if policy.Value < 0 {
			return nil, errors.New("retention value cannot be negative")
		}
		return nil, nil
	case ModeDays:
		if policy.Value < 1 {
			return nil, errors.New("day retention value must be at least one")
		}
		cutoff := now.AddDate(0, 0, -policy.Value)
		return &cutoff, nil
	case ModeMonths:
		if policy.Value < 1 {
			return nil, errors.New("month retention value must be at least one")
		}
		cutoff := subtractMonths(now, policy.Value)
		return &cutoff, nil
	default:
		return nil, fmt.Errorf("unsupported retention mode %q", policy.Mode)
	}
}

// Run applies a configured policy at the supplied reference time. Forever is
// a successful no-op.
func (c *Cleaner) Run(ctx context.Context, policy Policy, now time.Time) (Metrics, error) {
	if c == nil || c.store == nil {
		return Metrics{}, errors.New("retention cleaner is not configured")
	}
	cutoff, err := Cutoff(policy, now)
	if err != nil {
		return Metrics{}, err
	}
	if cutoff == nil {
		finishedAt := time.Now().UTC()
		metrics := Metrics{Mode: ModeForever, StartedAt: finishedAt, FinishedAt: finishedAt}
		c.log(metrics, nil)
		return metrics, nil
	}
	return c.runBefore(ctx, policy.Mode, *cutoff, time.Now().UTC())
}

// RunBefore performs manual cleanup before an explicit cutoff.
func (c *Cleaner) RunBefore(ctx context.Context, cutoff time.Time) (Metrics, error) {
	if cutoff.IsZero() {
		return Metrics{}, errors.New("manual retention cutoff is required")
	}
	return c.runBefore(ctx, ModeManual, cutoff.UTC(), time.Now().UTC())
}

func (c *Cleaner) runBefore(ctx context.Context, mode Mode, cutoff, startedAt time.Time) (metrics Metrics, returnErr error) {
	if c == nil || c.store == nil {
		return Metrics{}, errors.New("retention cleaner is not configured")
	}
	metrics = Metrics{Mode: mode, Cutoff: timePointer(cutoff), StartedAt: startedAt}
	defer func() {
		metrics.FinishedAt = time.Now().UTC()
		metrics.Duration = metrics.FinishedAt.Sub(metrics.StartedAt)
		if metrics.Duration < 0 {
			metrics.Duration = 0
		}
		c.log(metrics, returnErr)
	}()

	for batch := 0; batch < c.config.MaxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return metrics, err
		}
		deleted, err := c.store.DeleteResultsBefore(ctx, cutoff, c.config.BatchSize)
		metrics.Batches++
		if err != nil {
			return metrics, fmt.Errorf("delete expired result batch: %w", err)
		}
		if deleted < 0 || deleted > int64(c.config.BatchSize) {
			return metrics, fmt.Errorf("retention store returned invalid batch count %d", deleted)
		}
		metrics.DeletedResults += deleted
		if deleted < int64(c.config.BatchSize) {
			break
		}
		if batch == c.config.MaxBatches-1 {
			metrics.LimitReached = true
		}
	}

	if c.config.RunMaintenance {
		maintenance := c.store.(maintainer)
		checkpointErr := maintenance.Checkpoint(ctx)
		optimizeErr := maintenance.Optimize(ctx)
		if err := errors.Join(checkpointErr, optimizeErr); err != nil {
			return metrics, fmt.Errorf("post-retention database maintenance: %w", err)
		}
		metrics.MaintenancePerformed = true
	}
	return metrics, nil
}

func (c *Cleaner) log(metrics Metrics, err error) {
	if c == nil || c.logger == nil {
		return
	}
	attributes := []any{
		"mode", metrics.Mode,
		"batches", metrics.Batches,
		"deleted_results", metrics.DeletedResults,
		"limit_reached", metrics.LimitReached,
		"maintenance_performed", metrics.MaintenancePerformed,
		"duration", metrics.Duration,
	}
	if metrics.Cutoff != nil {
		attributes = append(attributes, "cutoff", *metrics.Cutoff)
	}
	if err != nil {
		attributes = append(attributes, "error", err)
		c.logger.Error("result retention cleanup failed", attributes...)
		return
	}
	c.logger.Info("result retention cleanup completed", attributes...)
}

func timePointer(value time.Time) *time.Time {
	return &value
}

// subtractMonths clamps the day to the final day of the destination month.
// time.AddDate normalizes February 31 into March, which would unexpectedly
// shorten retention for runs made near the end of a month.
func subtractMonths(value time.Time, months int) time.Time {
	targetMonthStart := time.Date(value.Year(), value.Month()-time.Month(months), 1,
		value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), value.Location())
	lastDay := targetMonthStart.AddDate(0, 1, -1).Day()
	day := value.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(targetMonthStart.Year(), targetMonthStart.Month(), day,
		value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), value.Location())
}
