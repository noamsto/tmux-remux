// Package store provides typed access to the tmux-state SQLite event store.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	// Register the modernc.org/sqlite driver under the name "sqlite".
	_ "modernc.org/sqlite"

	"github.com/noamsto/tmux-state/internal/store/migrations"
)

// Store wraps a *sql.DB connection to the tmux-state SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path, runs any pending
// migrations, and returns a *Store. WAL is enabled, foreign keys are on.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// DB returns the underlying *sql.DB. Callers may use it for ad-hoc queries
// not yet wrapped by typed methods.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	files, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%04d_", &version); err != nil {
			return fmt.Errorf("parse migration name %q: %w", name, err)
		}

		var current int
		if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
			return fmt.Errorf("read user_version: %w", err)
		}
		if version <= current {
			continue
		}
		if version != current+1 {
			return fmt.Errorf("migration gap: have %d, next is %d", current, version)
		}

		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}

		if err := s.applyMigration(ctx, version, string(body)); err != nil {
			return fmt.Errorf("apply migration %q: %w", name, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("exec body: %w", err)
	}
	// PRAGMA does not accept bound parameters — use Sprintf.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Event is a row in the events table.
type Event struct {
	ID            int64
	Ts            int64
	Kind          string
	Scope         string
	Reason        string
	Host          string
	ParentEventID *int64
	ManifestJSON  string
}

// InsertEvent inserts a new event row and returns its id.
func (s *Store) InsertEvent(ctx context.Context, ev Event) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO events (ts, kind, scope, reason, host, parent_event_id, manifest_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, ev.Ts, ev.Kind, ev.Scope, ev.Reason, ev.Host, ev.ParentEventID, ev.ManifestJSON)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	return res.LastInsertId()
}

// LatestSnapshot returns the newest snapshot event by timestamp, or (nil, nil)
// when no snapshot exists.
func (s *Store) LatestSnapshot(ctx context.Context) (*Event, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, ts, kind, scope, reason, host, parent_event_id, manifest_json
		FROM events
		WHERE kind = 'snapshot'
		ORDER BY ts DESC
		LIMIT 1
	`)
	var ev Event
	err := row.Scan(&ev.ID, &ev.Ts, &ev.Kind, &ev.Scope, &ev.Reason, &ev.Host, &ev.ParentEventID, &ev.ManifestJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest snapshot: %w", err)
	}
	return &ev, nil
}
