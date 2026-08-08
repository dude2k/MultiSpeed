package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/dude2k/MultiSpeed/internal/models"
)

const (
	MaxDashboardTasks      = 1000
	MaxDashboardActiveRuns = 1000
	DashboardFailureLimit  = 5
)

// DashboardResults is a bounded, diagnostics-free result snapshot used to
// assemble the operational dashboard without scanning result history.
type DashboardResults struct {
	Tasks          []models.Task
	LatestByTask   []models.Result
	LatestByPath   []models.Result
	ActiveRuns     []models.Result
	RecentFailures []models.Result
}

// GetDashboardResults executes all dashboard reads in one SQLite snapshot.
// Covering lookup indexes keep latest-per-task/path and status reads bounded.
func (s *Store) GetDashboardResults(ctx context.Context) (DashboardResults, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DashboardResults{}, err
	}
	defer func() { _ = tx.Rollback() }()
	tasks, err := queryDashboardTasks(ctx, tx)
	if err != nil {
		return DashboardResults{}, err
	}
	if len(tasks) > MaxDashboardTasks {
		return DashboardResults{}, errors.New("dashboard task limit exceeded")
	}

	latestByTask, err := queryDashboardResults(ctx, tx, `SELECT `+resultListColumns+`
FROM results AS current
WHERE EXISTS (
    SELECT 1 FROM tasks WHERE tasks.id = current.task_id AND tasks.deleted_at IS NULL
)
AND NOT EXISTS (
    SELECT 1 FROM results AS newer
    WHERE newer.task_id = current.task_id
      AND (newer.queued_at > current.queued_at OR (newer.queued_at = current.queued_at AND newer.id > current.id))
)
ORDER BY current.queued_at DESC, current.id DESC
LIMIT 1001`)
	if err != nil {
		return DashboardResults{}, err
	}
	if len(latestByTask) > MaxDashboardTasks {
		return DashboardResults{}, errors.New("dashboard task result limit exceeded")
	}

	latestByPath, err := queryDashboardResults(ctx, tx, `SELECT `+resultListColumns+`
FROM results AS current
WHERE EXISTS (
    SELECT 1 FROM tasks
    WHERE tasks.deleted_at IS NULL
      AND tasks.interface_name = current.selected_interface
      AND tasks.source_ip = current.selected_source_ip
)
AND NOT EXISTS (
    SELECT 1 FROM results AS newer
    WHERE newer.selected_interface = current.selected_interface
      AND newer.selected_source_ip = current.selected_source_ip
      AND (newer.queued_at > current.queued_at OR (newer.queued_at = current.queued_at AND newer.id > current.id))
)
ORDER BY current.queued_at DESC, current.id DESC
LIMIT 1001`)
	if err != nil {
		return DashboardResults{}, err
	}
	if len(latestByPath) > MaxDashboardTasks {
		return DashboardResults{}, errors.New("dashboard path result limit exceeded")
	}

	activeRuns, err := queryDashboardResults(ctx, tx, `SELECT `+resultListColumns+`
FROM results
WHERE status IN ('queued', 'validating', 'running')
ORDER BY queued_at DESC, id DESC
LIMIT 1000`)
	if err != nil {
		return DashboardResults{}, err
	}
	recentFailures, err := queryDashboardResults(ctx, tx, `SELECT `+resultListColumns+`
FROM results
WHERE status = 'failed'
ORDER BY COALESCE(finished_at, started_at, queued_at) DESC, id DESC
LIMIT 5`)
	if err != nil {
		return DashboardResults{}, err
	}
	if err := tx.Commit(); err != nil {
		return DashboardResults{}, err
	}
	return DashboardResults{
		Tasks: tasks, LatestByTask: latestByTask, LatestByPath: latestByPath,
		ActiveRuns: activeRuns, RecentFailures: recentFailures,
	}, nil
}

type dashboardQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryDashboardTasks(ctx context.Context, queryer dashboardQueryer) ([]models.Task, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT `+taskColumns+`
FROM tasks WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE, id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]models.Task, 0)
	for rows.Next() {
		task, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func queryDashboardResults(ctx context.Context, queryer dashboardQueryer, query string) ([]models.Result, error) {
	rows, err := queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	results := make([]models.Result, 0)
	for rows.Next() {
		result, scanErr := scanResult(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
