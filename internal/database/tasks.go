package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/google/uuid"
)

const taskColumns = `id, name, description, enabled, provider, cron_expression, timezone,
random_jitter_seconds, server_selection_mode, server_id, server_url, custom_server_definition,
interface_name, source_ip, ip_family, route_profile_id, timeout_seconds, provider_options,
prevent_overlap, route_validation, created_at, updated_at, last_scheduled_at, next_scheduled_at, deleted_at`

// CreateTask inserts a fully independent task.
func (s *Store) CreateTask(ctx context.Context, task *models.Task) error {
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	custom, err := json.Marshal(task.CustomServerDefinition)
	if err != nil {
		return fmt.Errorf("encode custom server definition: %w", err)
	}
	options, err := json.Marshal(task.ProviderOptions)
	if err != nil {
		return fmt.Errorf("encode provider options: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO tasks (`+taskColumns+`) VALUES (
?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Name, task.Description, task.Enabled, task.Provider, task.CronExpression, task.Timezone,
		task.RandomJitterSeconds, task.ServerSelectionMode, task.ServerID, task.ServerURL, string(custom),
		task.InterfaceName, task.SourceIP, task.IPFamily, task.RouteProfileID, task.TimeoutSeconds, string(options),
		task.PreventOverlap, task.RouteValidation, formatTime(task.CreatedAt), formatTime(task.UpdatedAt),
		formatOptionalTime(task.LastScheduledAt), formatOptionalTime(task.NextScheduledAt), nil)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// UpdateTask updates mutable fields while preserving identity and creation time.
func (s *Store) UpdateTask(ctx context.Context, task *models.Task) error {
	task.UpdatedAt = time.Now().UTC()
	custom, err := json.Marshal(task.CustomServerDefinition)
	if err != nil {
		return err
	}
	options, err := json.Marshal(task.ProviderOptions)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET name=?, description=?, enabled=?, provider=?, cron_expression=?, timezone=?,
random_jitter_seconds=?, server_selection_mode=?, server_id=?, server_url=?, custom_server_definition=?, interface_name=?, source_ip=?,
ip_family=?, route_profile_id=?, timeout_seconds=?, provider_options=?, prevent_overlap=?, route_validation=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		task.Name, task.Description, task.Enabled, task.Provider, task.CronExpression, task.Timezone, task.RandomJitterSeconds,
		task.ServerSelectionMode, task.ServerID, task.ServerURL, string(custom), task.InterfaceName, task.SourceIP, task.IPFamily,
		task.RouteProfileID, task.TimeoutSeconds, string(options), task.PreventOverlap, task.RouteValidation, formatTime(task.UpdatedAt), task.ID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return requireAffected(result, "task")
}

func (s *Store) UpdateTaskSchedule(ctx context.Context, id string, last, next *time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET last_scheduled_at=?, next_scheduled_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		formatOptionalTime(last), formatOptionalTime(next), formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	return requireAffected(result, "task")
}

func (s *Store) GetTask(ctx context.Context, id string) (models.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id=? AND deleted_at IS NULL`, id)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Task{}, ErrNotFound
	}
	return task, err
}

func (s *Store) ListTasks(ctx context.Context) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE, id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.Task, 0)
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if len(items) > 1000 {
			return nil, errors.New("task list exceeds the maximum of 1000 items")
		}
	}
	return items, rows.Err()
}

func (s *Store) ListEnabledTasks(ctx context.Context) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE enabled=1 AND deleted_at IS NULL ORDER BY id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.Task, 0)
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if len(items) > 1000 {
			return nil, errors.New("enabled task list exceeds the maximum of 1000 items")
		}
	}
	return items, rows.Err()
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET enabled=0, deleted_at=?, updated_at=?
WHERE id=? AND deleted_at IS NULL AND NOT EXISTS (
    SELECT 1 FROM results WHERE task_id=? AND status IN ('queued','validating','running')
)`, now, now, id, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var exists, active bool
	if err := s.db.QueryRowContext(ctx, `SELECT
EXISTS(SELECT 1 FROM tasks WHERE id=? AND deleted_at IS NULL),
EXISTS(SELECT 1 FROM results WHERE task_id=? AND status IN ('queued','validating','running'))`, id, id).Scan(&exists, &active); err != nil {
		return err
	}
	if exists && active {
		return ErrActive
	}
	return fmt.Errorf("task: %w", ErrNotFound)
}

func (s *Store) TaskHasActiveResults(ctx context.Context, id string) (bool, error) {
	var active bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM results WHERE task_id=? AND status IN ('queued','validating','running'))`, id).Scan(&active)
	return active, err
}

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner) (models.Task, error) {
	var task models.Task
	var enabled, prevent bool
	var customRaw, optionsRaw, createdRaw, updatedRaw string
	var routeID, lastRaw, nextRaw, deletedRaw sql.NullString
	err := row.Scan(&task.ID, &task.Name, &task.Description, &enabled, &task.Provider, &task.CronExpression, &task.Timezone,
		&task.RandomJitterSeconds, &task.ServerSelectionMode, &task.ServerID, &task.ServerURL, &customRaw,
		&task.InterfaceName, &task.SourceIP, &task.IPFamily, &routeID, &task.TimeoutSeconds, &optionsRaw,
		&prevent, &task.RouteValidation, &createdRaw, &updatedRaw, &lastRaw, &nextRaw, &deletedRaw)
	if err != nil {
		return task, err
	}
	task.Enabled, task.PreventOverlap = enabled, prevent
	if routeID.Valid {
		task.RouteProfileID = &routeID.String
	}
	if err := json.Unmarshal([]byte(customRaw), &task.CustomServerDefinition); err != nil {
		return task, err
	}
	if err := json.Unmarshal([]byte(optionsRaw), &task.ProviderOptions); err != nil {
		return task, err
	}
	if task.CreatedAt, err = parseTime(createdRaw); err != nil {
		return task, err
	}
	if task.UpdatedAt, err = parseTime(updatedRaw); err != nil {
		return task, err
	}
	if task.LastScheduledAt, err = nullableTime(lastRaw); err != nil {
		return task, err
	}
	if task.NextScheduledAt, err = nullableTime(nextRaw); err != nil {
		return task, err
	}
	if task.DeletedAt, err = nullableTime(deletedRaw); err != nil {
		return task, err
	}
	return task, nil
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

var (
	ErrNotFound = errors.New("not found")
	ErrActive   = errors.New("resource has an active execution")
	ErrInUse    = errors.New("resource is still in use")
)

func requireAffected(result sql.Result, resource string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%s: %w", resource, ErrNotFound)
	}
	return nil
}
