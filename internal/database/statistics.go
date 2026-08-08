package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/models"
)

// StatisticsFilter restricts the result rows streamed to the statistics
// service. Empty comparison slices match every value in that dimension.
type StatisticsFilter struct {
	From            time.Time
	To              time.Time
	TaskIDs         []string
	Interfaces      []string
	SourceIPs       []string
	Providers       []string
	ServerIDs       []string
	RouteProfileIDs []string
	PublicIPs       []string
}

// StatisticsResult contains only fields needed for aggregation and comparison.
// It deliberately omits provider payloads and other potentially large columns.
type StatisticsResult struct {
	ID                    string
	TaskID                string
	RouteProfileID        string
	Provider              models.ProviderID
	Status                models.ResultStatus
	Timestamp             time.Time
	DownloadBitsPerSecond *int64
	UploadBitsPerSecond   *int64
	LatencyMilliseconds   *float64
	JitterMilliseconds    *float64
	PacketLossPercent     *float64
	ExecutionDurationMS   int64
	Interface             string
	SourceIP              string
	ServerID              string
	PublicIP              string
	ProviderVersion       string
}

// WalkStatisticsResults streams matching results in chronological order. The
// callback is invoked while the query is open and must not retain pointer fields
// for mutation. Returning an error stops iteration immediately.
func (s *Store) WalkStatisticsResults(ctx context.Context, filter StatisticsFilter, visit func(StatisticsResult) error) error {
	if visit == nil {
		return errors.New("statistics visitor is required")
	}
	if filter.From.IsZero() || filter.To.IsZero() || !filter.To.After(filter.From) {
		return errors.New("statistics time range must have a non-zero end after start")
	}

	const timestampExpression = `COALESCE(started_at, finished_at, scheduled_at, queued_at)`
	conditions := []string{timestampExpression + ` >= ?`, timestampExpression + ` < ?`}
	args := []any{formatTime(filter.From), formatTime(filter.To)}
	for _, item := range []struct {
		column string
		values []string
	}{
		{"task_id", filter.TaskIDs},
		{"selected_interface", filter.Interfaces},
		{"selected_source_ip", filter.SourceIPs},
		{"provider", filter.Providers},
		{"server_id", filter.ServerIDs},
		{"route_profile_id", filter.RouteProfileIDs},
		{"detected_public_ip", filter.PublicIPs},
	} {
		clause, values, err := statisticsInClause(item.column, item.values)
		if err != nil {
			return err
		}
		if clause != "" {
			conditions = append(conditions, clause)
			args = append(args, values...)
		}
	}

	query := `SELECT id, task_id, route_profile_id, provider, status, ` + timestampExpression + `,
download_bps, upload_bps, latency_ms, jitter_ms, packet_loss_percent, execution_duration_ms,
selected_interface, selected_source_ip, server_id, detected_public_ip, provider_version
FROM results WHERE ` + strings.Join(conditions, " AND ") + `
ORDER BY ` + timestampExpression + ` ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query statistics results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		item, err := scanStatisticsResult(rows)
		if err != nil {
			return fmt.Errorf("scan statistics result: %w", err)
		}
		if err := visit(item); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate statistics results: %w", err)
	}
	return nil
}

func statisticsInClause(column string, values []string) (string, []any, error) {
	if len(values) == 0 {
		return "", nil, nil
	}
	if len(values) > 200 {
		return "", nil, fmt.Errorf("statistics filter %s has more than 200 values", column)
	}
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return column + " IN (" + strings.Join(placeholders, ",") + ")", args, nil
}

func scanStatisticsResult(row rowScanner) (StatisticsResult, error) {
	var item StatisticsResult
	var routeProfileID sql.NullString
	var timestamp string
	var download, upload sql.NullInt64
	var latency, jitter, loss sql.NullFloat64
	if err := row.Scan(
		&item.ID,
		&item.TaskID,
		&routeProfileID,
		&item.Provider,
		&item.Status,
		&timestamp,
		&download,
		&upload,
		&latency,
		&jitter,
		&loss,
		&item.ExecutionDurationMS,
		&item.Interface,
		&item.SourceIP,
		&item.ServerID,
		&item.PublicIP,
		&item.ProviderVersion,
	); err != nil {
		return item, err
	}
	if routeProfileID.Valid {
		item.RouteProfileID = routeProfileID.String
	}
	parsed, err := parseTime(timestamp)
	if err != nil {
		return item, fmt.Errorf("parse result timestamp: %w", err)
	}
	item.Timestamp = parsed
	if download.Valid {
		value := download.Int64
		item.DownloadBitsPerSecond = &value
	}
	if upload.Valid {
		value := upload.Int64
		item.UploadBitsPerSecond = &value
	}
	if latency.Valid {
		value := latency.Float64
		item.LatencyMilliseconds = &value
	}
	if jitter.Valid {
		value := jitter.Float64
		item.JitterMilliseconds = &value
	}
	if loss.Valid {
		value := loss.Float64
		item.PacketLossPercent = &value
	}
	return item, nil
}
