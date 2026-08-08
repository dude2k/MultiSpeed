// Package database owns SQLite lifecycle, migrations, integrity checks, and persistence.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/migrations"
	_ "modernc.org/sqlite"
)

const schemaTimeout = 30 * time.Second

// Store is the application's persistence boundary.
type Store struct {
	db     *sql.DB
	path   string
	logger *slog.Logger
}

// Open creates the data directory, opens SQLite with safe pragmas, and applies migrations.
func Open(ctx context.Context, path string, logger *slog.Logger) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	store := &Store{db: db, path: path, logger: logger}
	migrationCtx, cancel := context.WithTimeout(ctx, schemaTimeout)
	defer cancel()
	if err := store.migrate(migrationCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.PingContext(migrationCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return store, nil
}

func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)
	for _, name := range entries {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return fmt.Errorf("migration %q has no numeric prefix", name)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("migration %q: %w", name, err)
		}
		var exists int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if exists > 0 {
			continue
		}
		script, err := migrations.Files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if _, err = tx.ExecContext(ctx, string(script)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, formatTime(time.Now().UTC()))
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
		s.logger.Info("database migration applied", "version", version, "name", name)
	}
	return nil
}

// Close checkpoints the WAL and closes SQLite.
func (s *Store) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, checkpointErr := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	closeErr := s.db.Close()
	return errors.Join(checkpointErr, closeErr)
}

// CheckIntegrity executes SQLite's lightweight startup/readiness integrity check.
func (s *Store) CheckIntegrity(ctx context.Context) error {
	var result string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("sqlite quick_check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("sqlite quick_check returned %q", result)
	}
	return nil
}

// SchemaVersion reports the highest applied migration.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

// DatabaseSize returns the size of the main database and active WAL/SHM sidecars.
func (s *Store) DatabaseSize() int64 {
	var total int64
	for _, candidate := range []string{s.path, s.path + "-wal", s.path + "-shm"} {
		if info, err := os.Stat(candidate); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Path returns the configured SQLite path for the local system-information
// page. The API never accepts a path from a caller.
func (s *Store) Path() string { return s.path }

// Backup creates a transactionally consistent SQLite snapshot at destination.
func (s *Store) Backup(ctx context.Context, destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("backup destination must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := os.Stat(destination); err == nil {
		return errors.New("backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup destination: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, destination); err != nil {
		return fmt.Errorf("sqlite online backup: %w", err)
	}
	return nil
}

const fixedUTCLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(value time.Time) string { return value.UTC().Format(fixedUTCLayout) }

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func nullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
