package database

import (
	"context"
	"fmt"
	"time"
)

// RecoverInterruptedResults marks non-terminal executions left by a previous
// process as cancelled. Scheduled tasks are reloaded separately and no missed
// run is silently replayed.
func (s *Store) RecoverInterruptedResults(ctx context.Context) (int64, error) {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `UPDATE results
SET status='cancelled', finished_at=?, sanitized_error='MultiSpeed restarted before this execution completed.'
WHERE status IN ('queued','validating','running')`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) DeleteResultsBefore(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize < 1 || batchSize > 5000 {
		return 0, fmt.Errorf("batch size must be between 1 and 5000")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM results WHERE id IN (
SELECT id FROM results
WHERE status IN ('succeeded','failed','skipped','cancelled') AND finished_at IS NOT NULL AND finished_at < ?
ORDER BY finished_at LIMIT ?
)`, formatTime(cutoff), batchSize)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`)
	return err
}

func (s *Store) Optimize(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA optimize`)
	return err
}
