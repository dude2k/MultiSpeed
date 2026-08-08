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

const routeColumns = `id, name, description, interface_name, source_ip, expected_gateway,
expected_routing_table, validation_target, notes, created_at, updated_at, last_validation_at,
last_validation_succeeded, last_validation_snapshot, deleted_at`

func (s *Store) CreateRouteProfile(ctx context.Context, profile *models.RouteProfile) error {
	if profile.ID == "" {
		profile.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	profile.CreatedAt, profile.UpdatedAt = now, now
	snapshot, err := json.Marshal(profile.LastValidationSnapshot)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO route_profiles (`+routeColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.Name, profile.Description, profile.InterfaceName, profile.SourceIP, profile.ExpectedGateway,
		profile.ExpectedRoutingTable, profile.ValidationTarget, profile.Notes, formatTime(profile.CreatedAt), formatTime(profile.UpdatedAt),
		formatOptionalTime(profile.LastValidationAt), profile.LastValidationSucceeded, string(snapshot), nil)
	if err != nil {
		return fmt.Errorf("insert route profile: %w", err)
	}
	return nil
}

func (s *Store) UpdateRouteProfile(ctx context.Context, profile *models.RouteProfile) error {
	profile.UpdatedAt = time.Now().UTC()
	snapshot, err := json.Marshal(profile.LastValidationSnapshot)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE route_profiles SET name=?, description=?, interface_name=?, source_ip=?, expected_gateway=?,
expected_routing_table=?, validation_target=?, notes=?, updated_at=?, last_validation_at=?, last_validation_succeeded=?, last_validation_snapshot=? WHERE id=? AND deleted_at IS NULL`,
		profile.Name, profile.Description, profile.InterfaceName, profile.SourceIP, profile.ExpectedGateway, profile.ExpectedRoutingTable,
		profile.ValidationTarget, profile.Notes, formatTime(profile.UpdatedAt), formatOptionalTime(profile.LastValidationAt),
		profile.LastValidationSucceeded, string(snapshot), profile.ID)
	if err != nil {
		return err
	}
	return requireAffected(result, "route profile")
}

func (s *Store) UpdateRouteValidation(ctx context.Context, id string, validation models.RouteValidation) error {
	raw, err := json.Marshal(validation)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE route_profiles SET last_validation_at=?, last_validation_succeeded=?, last_validation_snapshot=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		formatTime(validation.ValidatedAt), validation.Success, string(raw), formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	return requireAffected(result, "route profile")
}

func (s *Store) GetRouteProfile(ctx context.Context, id string) (models.RouteProfile, error) {
	profile, err := scanRoute(s.db.QueryRowContext(ctx, `SELECT `+routeColumns+` FROM route_profiles WHERE id=? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return profile, ErrNotFound
	}
	return profile, err
}

func (s *Store) ListRouteProfiles(ctx context.Context) ([]models.RouteProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+routeColumns+` FROM route_profiles WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE, id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	profiles := make([]models.RouteProfile, 0)
	for rows.Next() {
		profile, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
		if len(profiles) > 1000 {
			return nil, errors.New("route profile list exceeds the maximum of 1000 items")
		}
	}
	return profiles, rows.Err()
}

func (s *Store) DeleteRouteProfile(ctx context.Context, id string) error {
	now := formatTime(time.Now().UTC())
	result, err := s.db.ExecContext(ctx, `UPDATE route_profiles SET deleted_at=?, updated_at=?
WHERE id=? AND deleted_at IS NULL AND NOT EXISTS (
    SELECT 1 FROM tasks WHERE route_profile_id=? AND deleted_at IS NULL
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
	var exists, inUse bool
	if err := s.db.QueryRowContext(ctx, `SELECT
EXISTS(SELECT 1 FROM route_profiles WHERE id=? AND deleted_at IS NULL),
EXISTS(SELECT 1 FROM tasks WHERE route_profile_id=? AND deleted_at IS NULL)`, id, id).Scan(&exists, &inUse); err != nil {
		return err
	}
	if exists && inUse {
		return ErrInUse
	}
	return fmt.Errorf("route profile: %w", ErrNotFound)
}

func scanRoute(row rowScanner) (models.RouteProfile, error) {
	var profile models.RouteProfile
	var createdRaw, updatedRaw, snapshotRaw string
	var validatedRaw, deletedRaw sql.NullString
	var succeeded sql.NullBool
	err := row.Scan(&profile.ID, &profile.Name, &profile.Description, &profile.InterfaceName, &profile.SourceIP,
		&profile.ExpectedGateway, &profile.ExpectedRoutingTable, &profile.ValidationTarget, &profile.Notes,
		&createdRaw, &updatedRaw, &validatedRaw, &succeeded, &snapshotRaw, &deletedRaw)
	if err != nil {
		return profile, err
	}
	if profile.CreatedAt, err = parseTime(createdRaw); err != nil {
		return profile, err
	}
	if profile.UpdatedAt, err = parseTime(updatedRaw); err != nil {
		return profile, err
	}
	if profile.LastValidationAt, err = nullableTime(validatedRaw); err != nil {
		return profile, err
	}
	if succeeded.Valid {
		profile.LastValidationSucceeded = &succeeded.Bool
	}
	if err := json.Unmarshal([]byte(snapshotRaw), &profile.LastValidationSnapshot); err != nil {
		return profile, err
	}
	if profile.DeletedAt, err = nullableTime(deletedRaw); err != nil {
		return profile, err
	}
	return profile, nil
}
