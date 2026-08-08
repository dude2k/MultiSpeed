package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

func (s *Store) GetSettings(ctx context.Context) (models.Settings, error) {
	return getSettings(ctx, s.db)
}

func getSettings(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (models.Settings, error) {
	var settings models.Settings
	var ooklaAcceptedAt sql.NullString
	var ooklaVersion sql.NullString
	err := query.QueryRowContext(ctx, `SELECT display_units, default_timezone, global_concurrency,
allow_separate_wan_concurrency, retention_mode, retention_value, default_chart_range,
interface_refresh_interval_seconds, default_task_timeout_seconds, database_maintenance_schedule,
ookla_eula_accepted, ookla_eula_accepted_at, ookla_eula_version
FROM settings WHERE singleton=1`).Scan(&settings.DisplayUnits, &settings.DefaultTimezone, &settings.GlobalConcurrency,
		&settings.AllowSeparateWANConcurrency, &settings.RetentionMode, &settings.RetentionValue, &settings.DefaultChartRange,
		&settings.InterfaceRefreshInterval, &settings.DefaultTaskTimeout, &settings.DatabaseMaintenanceSchedule,
		&settings.OoklaEULAAccepted, &ooklaAcceptedAt, &ooklaVersion)
	if err != nil {
		return settings, err
	}
	settings.OoklaEULAAcceptedAt, err = nullableTime(ooklaAcceptedAt)
	if ooklaVersion.Valid {
		settings.OoklaEULAVersion = ooklaVersion.String
	}
	// A stored acknowledgement is effective only for the exact terms revision
	// reviewed by this release. Keep its timestamp/version visible for audit.
	settings.OoklaEULAAccepted = settings.OoklaEULAAccepted && settings.OoklaEULAVersion == models.CurrentOoklaEULAVersion
	return settings, err
}

// SetOoklaEULAAcceptance records or revokes the operator's explicit consent.
// It is intentionally separate from UpdateSettings so a routine settings
// replacement cannot accidentally change a licensing decision.
func (s *Store) SetOoklaEULAAcceptance(ctx context.Context, accepted bool) error {
	var acceptedAt any
	if accepted {
		acceptedAt = formatTime(time.Now().UTC())
	}
	var version any
	if accepted {
		version = models.CurrentOoklaEULAVersion
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE settings SET ookla_eula_accepted=?, ookla_eula_accepted_at=?, ookla_eula_version=? WHERE singleton=1`, accepted, acceptedAt, version); err != nil {
		return fmt.Errorf("update Ookla EULA acceptance: %w", err)
	}
	return nil
}

func (s *Store) OoklaEULAAcceptance(ctx context.Context) (bool, error) {
	var accepted bool
	var version sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT ookla_eula_accepted, ookla_eula_version FROM settings WHERE singleton=1`).Scan(&accepted, &version); err != nil {
		return false, fmt.Errorf("read Ookla EULA acceptance: %w", err)
	}
	return accepted && version.Valid && version.String == models.CurrentOoklaEULAVersion, nil
}

func (s *Store) UpdateSettings(ctx context.Context, settings models.Settings) error {
	_, err := s.db.ExecContext(ctx, `UPDATE settings SET display_units=?, default_timezone=?, global_concurrency=?,
allow_separate_wan_concurrency=?, retention_mode=?, retention_value=?, default_chart_range=?,
interface_refresh_interval_seconds=?, default_task_timeout_seconds=?, database_maintenance_schedule=? WHERE singleton=1`,
		settings.DisplayUnits, settings.DefaultTimezone, settings.GlobalConcurrency, settings.AllowSeparateWANConcurrency,
		settings.RetentionMode, settings.RetentionValue, settings.DefaultChartRange, settings.InterfaceRefreshInterval,
		settings.DefaultTaskTimeout, settings.DatabaseMaintenanceSchedule)
	if err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return nil
}

func (s *Store) Counts(ctx context.Context) (tasks, results, running int, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE deleted_at IS NULL`).Scan(&tasks); err != nil {
		return
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM results`).Scan(&results); err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT task_id) FROM results WHERE status IN ('validating','running')`).Scan(&running)
	return
}
