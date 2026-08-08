package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

type ConfigurationSnapshot struct {
	Settings      models.Settings
	RouteProfiles []models.RouteProfile
	Tasks         []models.Task
}

// Configuration returns the portable persisted configuration from one
// read-only transaction, so an export never mixes different committed states.
func (s *Store) Configuration(ctx context.Context) (ConfigurationSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()

	settings, err := getSettings(ctx, tx)
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	tasks, err := listConfigurationTasks(ctx, tx)
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	routes, err := listConfigurationRoutes(ctx, tx)
	if err != nil {
		return ConfigurationSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return ConfigurationSnapshot{}, err
	}
	return ConfigurationSnapshot{Settings: settings, RouteProfiles: routes, Tasks: tasks}, nil
}

// ReplaceConfiguration atomically replaces active settings, tasks, and route
// profiles while preserving result history, soft-deleted identities, and the
// separately managed Ookla EULA acknowledgement.
func (s *Store) ReplaceConfiguration(ctx context.Context, settings models.Settings, routes []models.RouteProfile, tasks []models.Task) error {
	encodedTasks := make([][2]string, len(tasks))
	for index := range tasks {
		custom, err := json.Marshal(tasks[index].CustomServerDefinition)
		if err != nil {
			return fmt.Errorf("encode task %q custom server definition: %w", tasks[index].ID, err)
		}
		options, err := json.Marshal(tasks[index].ProviderOptions)
		if err != nil {
			return fmt.Errorf("encode task %q provider options: %w", tasks[index].ID, err)
		}
		encodedTasks[index] = [2]string{string(custom), string(options)}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM results WHERE status IN ('queued','validating','running')`).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrActive
	}

	existingTaskIDs, err := liveTaskIDs(ctx, tx)
	if err != nil {
		return err
	}
	existingRouteIDs, err := liveRouteIDs(ctx, tx)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE settings SET display_units=?, default_timezone=?, global_concurrency=?,
allow_separate_wan_concurrency=?, retention_mode=?, retention_value=?, default_chart_range=?,
interface_refresh_interval_seconds=?, default_task_timeout_seconds=?, database_maintenance_schedule=? WHERE singleton=1`,
		settings.DisplayUnits, settings.DefaultTimezone, settings.GlobalConcurrency, settings.AllowSeparateWANConcurrency,
		settings.RetentionMode, settings.RetentionValue, settings.DefaultChartRange, settings.InterfaceRefreshInterval,
		settings.DefaultTaskTimeout, settings.DatabaseMaintenanceSchedule); err != nil {
		return fmt.Errorf("replace configuration settings: %w", err)
	}

	now := formatTime(time.Now().UTC())
	importedRouteIDs := make(map[string]struct{}, len(routes))
	for index := range routes {
		profile := routes[index]
		importedRouteIDs[profile.ID] = struct{}{}
		if _, err := tx.ExecContext(ctx, `INSERT INTO route_profiles (`+routeColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, interface_name=excluded.interface_name,
source_ip=excluded.source_ip, expected_gateway=excluded.expected_gateway, expected_routing_table=excluded.expected_routing_table,
validation_target=excluded.validation_target, notes=excluded.notes, updated_at=excluded.updated_at,
last_validation_at=NULL, last_validation_succeeded=NULL, last_validation_snapshot='{}', deleted_at=NULL`,
			profile.ID, profile.Name, profile.Description, profile.InterfaceName, profile.SourceIP, profile.ExpectedGateway,
			profile.ExpectedRoutingTable, profile.ValidationTarget, profile.Notes, now, now, nil, nil, "{}", nil); err != nil {
			return fmt.Errorf("replace route profile %q: %w", profile.ID, err)
		}
	}

	importedTaskIDs := make(map[string]struct{}, len(tasks))
	for index := range tasks {
		task := tasks[index]
		importedTaskIDs[task.ID] = struct{}{}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tasks (`+taskColumns+`) VALUES (
?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, enabled=excluded.enabled,
provider=excluded.provider, cron_expression=excluded.cron_expression, timezone=excluded.timezone,
random_jitter_seconds=excluded.random_jitter_seconds, server_selection_mode=excluded.server_selection_mode,
server_id=excluded.server_id, server_url=excluded.server_url, custom_server_definition=excluded.custom_server_definition,
interface_name=excluded.interface_name, source_ip=excluded.source_ip, ip_family=excluded.ip_family,
route_profile_id=excluded.route_profile_id, timeout_seconds=excluded.timeout_seconds, provider_options=excluded.provider_options,
prevent_overlap=excluded.prevent_overlap, route_validation=excluded.route_validation, updated_at=excluded.updated_at, deleted_at=NULL`,
			task.ID, task.Name, task.Description, task.Enabled, task.Provider, task.CronExpression, task.Timezone,
			task.RandomJitterSeconds, task.ServerSelectionMode, task.ServerID, task.ServerURL, encodedTasks[index][0],
			task.InterfaceName, task.SourceIP, task.IPFamily, task.RouteProfileID, task.TimeoutSeconds, encodedTasks[index][1],
			task.PreventOverlap, task.RouteValidation, now, now, nil, nil, nil); err != nil {
			return fmt.Errorf("replace task %q: %w", task.ID, err)
		}
	}

	for _, id := range existingTaskIDs {
		if _, present := importedTaskIDs[id]; present {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET enabled=0, updated_at=?, deleted_at=? WHERE id=? AND deleted_at IS NULL`, now, now, id); err != nil {
			return fmt.Errorf("retire replaced task %q: %w", id, err)
		}
	}
	for _, id := range existingRouteIDs {
		if _, present := importedRouteIDs[id]; present {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE route_profiles SET updated_at=?, deleted_at=? WHERE id=? AND deleted_at IS NULL`, now, now, id); err != nil {
			return fmt.Errorf("retire replaced route profile %q: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit configuration replacement: %w", err)
	}
	return nil
}

func listConfigurationTasks(ctx context.Context, tx *sql.Tx) ([]models.Task, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE, id LIMIT 1001`)
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
			return nil, fmt.Errorf("task list exceeds the maximum of 1000 items")
		}
	}
	return items, rows.Err()
}

func listConfigurationRoutes(ctx context.Context, tx *sql.Tx) ([]models.RouteProfile, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+routeColumns+` FROM route_profiles WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE, id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.RouteProfile, 0)
	for rows.Next() {
		item, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if len(items) > 1000 {
			return nil, fmt.Errorf("route profile list exceeds the maximum of 1000 items")
		}
	}
	return items, rows.Err()
}

func liveTaskIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return scanIDs(rows)
}

func liveRouteIDs(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM route_profiles WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return scanIDs(rows)
}

func scanIDs(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
