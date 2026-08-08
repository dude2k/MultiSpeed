package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
	"github.com/google/uuid"
)

const resultColumns = `id, task_id, route_profile_id, trigger_type, provider, queued_at, scheduled_at, started_at, finished_at,
status, download_bps, upload_bps, latency_ms, jitter_ms, packet_loss_percent, download_bytes, upload_bytes,
selected_interface, selected_source_ip, detected_public_ip, server_id, server_name, server_host, server_sponsor,
server_location, server_country, provider_result_url, cloudflare_colo, route_validation_snapshot, execution_duration_ms,
process_exit_code, sanitized_error, raw_provider_response, provider_version, application_version, tls_verification_disabled`

// resultListColumns substitutes bounded values for large diagnostic columns.
// Collection reads therefore do not load raw provider payloads or route
// snapshots from SQLite; those remain available through GetResult and exports.
const resultListColumns = `id, task_id, route_profile_id, trigger_type, provider, queued_at, scheduled_at, started_at, finished_at,
status, download_bps, upload_bps, latency_ms, jitter_ms, packet_loss_percent, download_bytes, upload_bytes,
selected_interface, selected_source_ip, detected_public_ip, server_id, server_name, server_host, server_sponsor,
server_location, server_country, provider_result_url, cloudflare_colo, '{}' AS route_validation_snapshot, execution_duration_ms,
process_exit_code, sanitized_error, '' AS raw_provider_response, provider_version, application_version, tls_verification_disabled`

// MaxResultPageSize bounds interactive result reads. Larger historical
// comparisons use the statistics endpoint instead of materializing raw rows.
const MaxResultPageSize = 200

type ResultFilter struct {
	Page       int
	PageSize   int
	TaskID     string
	Provider   string
	Status     string
	Interface  string
	SourceIP   string
	ServerID   string
	From       *time.Time
	To         *time.Time
	Sort       string
	Descending bool
}

func (s *Store) CreateResult(ctx context.Context, result *models.Result) error {
	if result.ID == "" {
		result.ID = uuid.NewString()
	}
	if result.QueuedAt.IsZero() {
		result.QueuedAt = time.Now().UTC()
	}
	routeSnapshot, err := json.Marshal(result.RouteValidationSnapshot)
	if err != nil {
		return err
	}
	dbResult, err := s.db.ExecContext(ctx, `INSERT INTO results (`+resultColumns+`) SELECT `+strings.TrimSuffix(strings.Repeat("?,", 36), ",")+`
WHERE EXISTS (SELECT 1 FROM tasks WHERE id=? AND deleted_at IS NULL)`,
		result.ID, result.TaskID, result.RouteProfileID, result.Trigger, result.Provider, formatTime(result.QueuedAt), formatOptionalTime(result.ScheduledAt),
		formatOptionalTime(result.StartedAt), formatOptionalTime(result.FinishedAt), result.Status, result.DownloadBitsPerSecond,
		result.UploadBitsPerSecond, result.LatencyMilliseconds, result.JitterMilliseconds, result.PacketLossPercent,
		result.DownloadBytes, result.UploadBytes, result.SelectedInterface, result.SelectedSourceIP, result.DetectedPublicIP,
		result.ServerID, result.ServerName, result.ServerHost, result.ServerSponsor, result.ServerLocation, result.ServerCountry,
		result.ProviderResultURL, result.CloudflareColo, string(routeSnapshot), result.ExecutionDurationMS, result.ProcessExitCode,
		result.SanitizedError, result.RawProviderResponse, result.ProviderVersion, result.ApplicationVersion, result.TLSVerificationDisabled,
		result.TaskID)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}
	if count, countErr := dbResult.RowsAffected(); countErr != nil {
		return countErr
	} else if count == 0 {
		return fmt.Errorf("task: %w", ErrNotFound)
	}
	return nil
}

func (s *Store) UpdateResult(ctx context.Context, result *models.Result) error {
	snapshot, err := json.Marshal(result.RouteValidationSnapshot)
	if err != nil {
		return err
	}
	dbResult, err := s.db.ExecContext(ctx, `UPDATE results SET scheduled_at=?, started_at=?, finished_at=?, status=?, download_bps=?, upload_bps=?,
latency_ms=?, jitter_ms=?, packet_loss_percent=?, download_bytes=?, upload_bytes=?, detected_public_ip=?, server_id=?, server_name=?, server_host=?,
server_sponsor=?, server_location=?, server_country=?, provider_result_url=?, cloudflare_colo=?, route_validation_snapshot=?, execution_duration_ms=?,
process_exit_code=?, sanitized_error=?, raw_provider_response=?, provider_version=?, application_version=?, tls_verification_disabled=? WHERE id=?`,
		formatOptionalTime(result.ScheduledAt), formatOptionalTime(result.StartedAt), formatOptionalTime(result.FinishedAt), result.Status,
		result.DownloadBitsPerSecond, result.UploadBitsPerSecond, result.LatencyMilliseconds, result.JitterMilliseconds, result.PacketLossPercent,
		result.DownloadBytes, result.UploadBytes, result.DetectedPublicIP, result.ServerID, result.ServerName, result.ServerHost,
		result.ServerSponsor, result.ServerLocation, result.ServerCountry, result.ProviderResultURL, result.CloudflareColo, string(snapshot),
		result.ExecutionDurationMS, result.ProcessExitCode, result.SanitizedError, result.RawProviderResponse, result.ProviderVersion,
		result.ApplicationVersion, result.TLSVerificationDisabled, result.ID)
	if err != nil {
		return err
	}
	return requireAffected(dbResult, "result")
}

func (s *Store) GetResult(ctx context.Context, id string) (models.Result, error) {
	result, err := scanResult(s.db.QueryRowContext(ctx, `SELECT `+resultColumns+` FROM results WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	return result, err
}

func (s *Store) ListResults(ctx context.Context, filter ResultFilter) (models.Page[models.Result], error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 25
	}
	if filter.PageSize > MaxResultPageSize {
		filter.PageSize = MaxResultPageSize
	}
	where, args := resultWhere(filter)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM results`+where, args...).Scan(&total); err != nil {
		return models.Page[models.Result]{}, err
	}
	totalPages := total / filter.PageSize
	if total%filter.PageSize != 0 {
		totalPages++
	}
	emptyPage := func() models.Page[models.Result] {
		return models.Page[models.Result]{Items: []models.Result{}, Page: filter.Page, PageSize: filter.PageSize, TotalItems: total, TotalPages: totalPages}
	}
	// Avoid an overflowing OFFSET for adversarially large page numbers. Once
	// the count is known, any page beyond the last one is unambiguously empty.
	if total == 0 || filter.Page > totalPages {
		return emptyPage(), nil
	}
	sortColumn := map[string]string{"startedAt": "started_at", "finishedAt": "finished_at", "download": "download_bps", "upload": "upload_bps", "latency": "latency_ms"}[filter.Sort]
	if sortColumn == "" {
		sortColumn = "queued_at"
	}
	direction := "ASC"
	if filter.Descending || filter.Sort == "" {
		direction = "DESC"
	}
	queryArgs := append(append([]any{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+resultListColumns+` FROM results`+where+` ORDER BY `+sortColumn+` `+direction+`, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return models.Page[models.Result]{}, err
	}
	defer func() { _ = rows.Close() }()
	// SQL LIMIT is bounded above, so append growth is bounded without deriving
	// an allocation capacity from request-controlled pagination input.
	items := make([]models.Result, 0)
	for rows.Next() {
		item, err := scanResult(rows)
		if err != nil {
			return models.Page[models.Result]{}, err
		}
		items = append(items, item)
	}
	return models.Page[models.Result]{Items: items, Page: filter.Page, PageSize: filter.PageSize, TotalItems: total, TotalPages: totalPages}, rows.Err()
}

// WalkResults streams one stable SQLite SELECT snapshot for exports. It avoids
// OFFSET pagination races when results are concurrently inserted or deleted.
func (s *Store) WalkResults(ctx context.Context, filter ResultFilter, visit func(models.Result) error) error {
	if visit == nil {
		return errors.New("result visitor is required")
	}
	where, args := resultWhere(filter)
	rows, err := s.db.QueryContext(ctx, `SELECT `+resultColumns+` FROM results`+where+` ORDER BY queued_at ASC, id ASC`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item, scanErr := scanResult(rows)
		if scanErr != nil {
			return scanErr
		}
		if visitErr := visit(item); visitErr != nil {
			return visitErr
		}
	}
	return rows.Err()
}

func resultWhere(filter ResultFilter) (string, []any) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	for _, pair := range []struct{ value, column string }{
		{filter.TaskID, "task_id"}, {filter.Provider, "provider"}, {filter.Status, "status"},
		{filter.Interface, "selected_interface"}, {filter.SourceIP, "selected_source_ip"}, {filter.ServerID, "server_id"},
	} {
		if pair.value != "" {
			conditions = append(conditions, pair.column+" = ?")
			args = append(args, pair.value)
		}
	}
	if filter.From != nil {
		conditions = append(conditions, "COALESCE(started_at, finished_at, scheduled_at, queued_at) >= ?")
		args = append(args, formatTime(*filter.From))
	}
	if filter.To != nil {
		conditions = append(conditions, "COALESCE(started_at, finished_at, scheduled_at, queued_at) < ?")
		args = append(args, formatTime(*filter.To))
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *Store) DeleteResult(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM results WHERE id=? AND status IN ('succeeded','failed','skipped','cancelled')`, id)
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
	var active bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM results WHERE id=? AND status IN ('queued','validating','running'))`, id).Scan(&active); err != nil {
		return err
	}
	if active {
		return ErrActive
	}
	return fmt.Errorf("result: %w", ErrNotFound)
}

func (s *Store) DeleteResults(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return 0, errors.New("between 1 and 500 result IDs are required")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = ids[i]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM results WHERE id IN (`+placeholders+`) AND status IN ('queued','validating','running')`, args...).Scan(&active); err != nil {
		return 0, err
	}
	if active > 0 {
		return 0, ErrActive
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM results WHERE id IN (`+placeholders+`) AND status IN ('succeeded','failed','skipped','cancelled')`, args...)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func scanResult(row rowScanner) (models.Result, error) {
	var result models.Result
	var routeID, queued, scheduled, started, finished sql.NullString
	var down, up, downBytes, upBytes sql.NullInt64
	var latency, jitter, loss sql.NullFloat64
	var exitCode sql.NullInt64
	var snapshotRaw string
	err := row.Scan(&result.ID, &result.TaskID, &routeID, &result.Trigger, &result.Provider, &queued, &scheduled, &started, &finished,
		&result.Status, &down, &up, &latency, &jitter, &loss, &downBytes, &upBytes, &result.SelectedInterface,
		&result.SelectedSourceIP, &result.DetectedPublicIP, &result.ServerID, &result.ServerName, &result.ServerHost,
		&result.ServerSponsor, &result.ServerLocation, &result.ServerCountry, &result.ProviderResultURL, &result.CloudflareColo,
		&snapshotRaw, &result.ExecutionDurationMS, &exitCode, &result.SanitizedError, &result.RawProviderResponse,
		&result.ProviderVersion, &result.ApplicationVersion, &result.TLSVerificationDisabled)
	if err != nil {
		return result, err
	}
	if routeID.Valid {
		result.RouteProfileID = &routeID.String
	}
	var errTime error
	if queued.Valid {
		if result.QueuedAt, errTime = parseTime(queued.String); errTime != nil {
			return result, errTime
		}
	}
	if result.ScheduledAt, errTime = nullableTime(scheduled); errTime != nil {
		return result, errTime
	}
	if result.StartedAt, errTime = nullableTime(started); errTime != nil {
		return result, errTime
	}
	if result.FinishedAt, errTime = nullableTime(finished); errTime != nil {
		return result, errTime
	}
	if down.Valid {
		result.DownloadBitsPerSecond = &down.Int64
	}
	if up.Valid {
		result.UploadBitsPerSecond = &up.Int64
	}
	if latency.Valid {
		result.LatencyMilliseconds = &latency.Float64
	}
	if jitter.Valid {
		result.JitterMilliseconds = &jitter.Float64
	}
	if loss.Valid {
		result.PacketLossPercent = &loss.Float64
	}
	if downBytes.Valid {
		result.DownloadBytes = &downBytes.Int64
	}
	if upBytes.Valid {
		result.UploadBytes = &upBytes.Int64
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		result.ProcessExitCode = &code
	}
	if err := json.Unmarshal([]byte(snapshotRaw), &result.RouteValidationSnapshot); err != nil {
		return result, err
	}
	return result, nil
}
