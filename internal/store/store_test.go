package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-state/internal/store"
)

func TestOpenAppliesMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Errorf("user_version = %d, want 1", version)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		db, err := store.Open(ctx, dbPath)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		db.Close()
	}
}

func TestMigrateRespectsUserVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	// First open creates the schema and sets user_version=1.
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	var v int
	if err := db.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != 1 {
		t.Errorf("after first open: user_version = %d, want 1", v)
	}
	db.Close()

	// Second open is a no-op for migrations.
	db, err = store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db.Close()
	if err := db.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != 1 {
		t.Errorf("after second open: user_version = %d, want 1", v)
	}
}

func TestInsertEventReturnsID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	id, err := db.InsertEvent(ctx, store.Event{
		Ts:           1745700000000,
		Kind:         "snapshot",
		Scope:        "server",
		Reason:       "timer",
		Host:         "testhost",
		ManifestJSON: `{"v":1}`,
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestLatestSnapshotReturnsMostRecent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	for i, ts := range []int64{1, 2, 3} {
		_, err := db.InsertEvent(ctx, store.Event{
			Ts:           ts,
			Kind:         "snapshot",
			Scope:        "server",
			Host:         "h",
			ManifestJSON: fmt.Sprintf(`{"i":%d}`, i),
		})
		if err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	ev, err := db.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Ts != 3 {
		t.Errorf("Ts = %d, want 3", ev.Ts)
	}
}

func TestLatestSnapshotReturnsNilWhenEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ev, err := db.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil, got %+v", ev)
	}
}
