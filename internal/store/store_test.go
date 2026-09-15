package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/store/migrations"
)

func TestOpenAppliesMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 2 {
		t.Errorf("user_version = %d, want 2", version)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		db.Close()
	}
}

func TestMigrateRespectsUserVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	// First open creates the schema and sets user_version=2.
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	var v int
	if err := db.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != 2 {
		t.Errorf("after first open: user_version = %d, want 2", v)
	}
	db.Close()

	// Second open is a no-op for migrations.
	db, err = store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db.Close()
	if err := db.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if v != 2 {
		t.Errorf("after second open: user_version = %d, want 2", v)
	}
}

func TestInsertEventReturnsID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
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
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
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
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
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

func TestListEventsByKind(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insert := func(ts int64, kind string) {
		t.Helper()
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: kind, Scope: "session", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert(10, "snapshot")
	insert(20, "pane-died")
	insert(30, "snapshot")
	insert(40, "session-closed")

	closes, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 2 {
		t.Fatalf("got %d events, want 2", len(closes))
	}
	if closes[0].Ts != 40 || closes[1].Ts != 20 {
		t.Errorf("expected ts=40,20 (DESC), got %d,%d", closes[0].Ts, closes[1].Ts)
	}
}

func TestPruneSnapshotsKeepsNewest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for ts := int64(1); ts <= 10; ts++ {
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.PruneSnapshots(ctx, 3, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"snapshot"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d, want 3", len(all))
	}
	if all[0].Ts != 10 || all[1].Ts != 9 || all[2].Ts != 8 {
		t.Errorf("expected newest 3 (10,9,8), got %d,%d,%d", all[0].Ts, all[1].Ts, all[2].Ts)
	}
}

func TestPruneCloseEventsKeepsNewest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for ts := int64(1); ts <= 10; ts++ {
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "pane-died", Scope: "session", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.PruneCloseEvents(ctx, 3); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d, want 3", len(all))
	}
	if all[0].Ts != 10 || all[1].Ts != 9 || all[2].Ts != 8 {
		t.Errorf("expected newest 3 (10,9,8), got %d,%d,%d", all[0].Ts, all[1].Ts, all[2].Ts)
	}
}

func TestPruneCloseEventsDropsUnresolvable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insert := func(ts int64, kind string) {
		t.Helper()
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: kind, Scope: "session", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert(100, "snapshot")
	insert(200, "snapshot")
	insert(50, "pane-died")
	insert(100, "pane-died") // == MIN snapshot ts: no prior snapshot
	insert(150, "pane-died")
	insert(250, "pane-died")

	if _, err := db.PruneCloseEvents(ctx, 50); err != nil {
		t.Fatal(err)
	}

	closes, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 2 {
		t.Fatalf("got %d closes, want 2", len(closes))
	}
	if closes[0].Ts != 250 || closes[1].Ts != 150 {
		t.Errorf("expected ts=250,150, got %d,%d", closes[0].Ts, closes[1].Ts)
	}
}

func TestPruneCloseEventsKeepsAllWhenNoSnapshots(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, ts := range []int64{50, 150, 250} {
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "pane-died", Scope: "session", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.PruneCloseEvents(ctx, 50); err != nil {
		t.Fatal(err)
	}

	closes, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 3 {
		t.Fatalf("got %d closes, want 3", len(closes))
	}
}

func TestPruneUnresolvableCloseEventsKeepsResolvable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "snapshot", Scope: "session", Host: "h", ManifestJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	const want = 60
	for ts := int64(101); ts <= 100+want; ts++ {
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "pane-died", Scope: "session", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.PruneUnresolvableCloseEvents(ctx); err != nil {
		t.Fatal(err)
	}

	closes, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != want {
		t.Fatalf("got %d closes, want %d", len(closes), want)
	}
}

func TestPruneUnresolvableCloseEventsSurvivesAboveFloorWithEmbeddedEntity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const embeddedManifest = `{"session_id":"s","window_id":"@2","index":{},"resolved":{"item":{"window_index":2,"session_name":"s"},"saved_at":100}}`

	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "snapshot", Scope: "session", Host: "h", ManifestJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 101, Kind: "window-unlinked", Scope: "window", Host: "h", ManifestJSON: embeddedManifest,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.PruneUnresolvableCloseEvents(ctx); err != nil {
		t.Fatal(err)
	}

	closes, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 1 {
		t.Fatalf("got %d closes, want 1: above the floor should survive even though nothing else resolves it", len(closes))
	}
}

func TestPruneUnresolvableCloseEventsPrunesAtFloorEvenWithEmbeddedEntity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const embeddedManifest = `{"session_id":"s","window_id":"@2","index":{},"resolved":{"item":{"window_index":2,"session_name":"s"},"saved_at":100}}`

	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "snapshot", Scope: "session", Host: "h", ManifestJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	// Captured in the same millisecond as the only snapshot: at the floor, so
	// it is pruned despite carrying an embedded entity that resolves fine.
	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "window-unlinked", Scope: "window", Host: "h", ManifestJSON: embeddedManifest,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.PruneUnresolvableCloseEvents(ctx); err != nil {
		t.Fatal(err)
	}

	closes, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(closes) != 0 {
		t.Fatalf("got %d closes, want 0: the age floor prunes even a resolvable, embedded-entity event", len(closes))
	}
}

func TestPruneUnresolvableCloseEventsDecrementsRefcount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertScrollback(ctx, "sha1", 10, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "snapshot", Scope: "session", Host: "h", ManifestJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	closeID, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "pane-died", Scope: "session", Host: "h", ManifestJSON: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.LinkEventScrollback(ctx, closeID, "s:1:1", "sha1"); err != nil {
		t.Fatal(err)
	}

	var refcount int
	_ = db.DB().QueryRowContext(ctx, "SELECT refcount FROM scrollbacks WHERE sha256='sha1'").Scan(&refcount)
	if refcount != 1 {
		t.Fatalf("refcount before prune = %d, want 1", refcount)
	}

	deleted, err := db.PruneUnresolvableCloseEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	_ = db.DB().QueryRowContext(ctx, "SELECT refcount FROM scrollbacks WHERE sha256='sha1'").Scan(&refcount)
	if refcount != 0 {
		t.Errorf("refcount after prune = %d, want 0", refcount)
	}
}

func TestUpsertScrollbackIncrementsRefcount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertScrollback(ctx, "abc123", 42, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertScrollback(ctx, "abc123", 42, 200); err != nil {
		t.Fatal(err)
	}

	var refcount int
	var lastUsed int64
	err = db.DB().QueryRowContext(ctx, "SELECT refcount, last_used_ts FROM scrollbacks WHERE sha256=?", "abc123").Scan(&refcount, &lastUsed)
	if err != nil {
		t.Fatal(err)
	}
	if refcount != 0 {
		t.Errorf("refcount on upsert should be 0 (linking happens via event_scrollbacks); got %d", refcount)
	}
	if lastUsed != 200 {
		t.Errorf("last_used_ts = %d, want 200", lastUsed)
	}
}

// TestPruneSnapshotsKeepsNewestPerDayWithinWeek verifies the retention
// safety net: besides the keep-N-newest window, the newest snapshot of each
// UTC day in the last 7 days survives — so a pre-shutdown snapshot is not
// evicted by a burst of fresh post-boot saves (the 2026-06-07 data loss).
// UTC-day grouping keeps the test deterministic in any host timezone.
func TestPruneSnapshotsKeepsNewestPerDayWithinWeek(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const day = int64(24 * time.Hour / time.Millisecond)
	now := int64(1780000000000) // fixed anchor
	insert := func(ts int64) {
		t.Helper()
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Two snapshots on each of day-3 and day-2 (the later one per day must
	// survive), one on day-10 (outside the week — must be pruned), and a
	// burst of 4 fresh snapshots that fills the keep-N window.
	insert(now - 10*day)
	insert(now - 3*day)
	insert(now - 3*day + 3_600_000)
	insert(now - 2*day)
	insert(now - 2*day + 3_600_000)
	for i := int64(0); i < 4; i++ {
		insert(now - 3000 + i*1000)
	}

	if err := db.PruneSnapshots(ctx, 3, now); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"snapshot"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]int64, 0, len(all))
	for _, ev := range all { // ListEvents returns ts DESC
		got = append(got, ev.Ts)
	}
	want := []int64{
		now, now - 1000, now - 2000, // 3 newest
		now - 2*day + 3_600_000, // newest of day-2
		now - 3*day + 3_600_000, // newest of day-3
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("survivors mismatch (-want +got):\n%s", diff)
	}
}

func TestLinkEventScrollbackBumpsRefcount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, _ := db.InsertEvent(ctx, store.Event{Ts: 1, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"})
	_ = db.UpsertScrollback(ctx, "sha1", 10, 1)
	if err := db.LinkEventScrollback(ctx, id, "s:1:1", "sha1"); err != nil {
		t.Fatal(err)
	}

	var refcount int
	_ = db.DB().QueryRowContext(ctx, "SELECT refcount FROM scrollbacks WHERE sha256='sha1'").Scan(&refcount)
	if refcount != 1 {
		t.Errorf("refcount = %d, want 1", refcount)
	}
}

func TestDeletingEventDecrementsRefcount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, _ := db.InsertEvent(ctx, store.Event{Ts: 1, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"})
	_ = db.UpsertScrollback(ctx, "sha1", 10, 1)
	_ = db.LinkEventScrollback(ctx, id, "s:1:1", "sha1")

	if _, err := db.DB().ExecContext(ctx, "DELETE FROM events WHERE id=?", id); err != nil {
		t.Fatal(err)
	}

	var refcount int
	_ = db.DB().QueryRowContext(ctx, "SELECT refcount FROM scrollbacks WHERE sha256='sha1'").Scan(&refcount)
	if refcount != 0 {
		t.Errorf("refcount = %d, want 0", refcount)
	}
}

func TestSetGetMeta(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SetMeta(ctx, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMeta(ctx, "k")
	if err != nil || got != "v1" {
		t.Fatalf("GetMeta(k) = %q, %v; want v1, nil", got, err)
	}
	if err := db.SetMeta(ctx, "k", "v2"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetMeta(ctx, "k")
	if got != "v2" {
		t.Errorf("update did not stick: got %q", got)
	}
	missing, err := db.GetMeta(ctx, "nope")
	if err != nil || missing != "" {
		t.Errorf("missing key: got %q, %v; want \"\", nil", missing, err)
	}
}

// TestLatestSnapshotBeforeIgnoresNewerSnapshots pins the semantics restore
// relies on: a snapshot written at/after the anchor (server start) is never
// selected, only the newest strictly-older one.
func TestLatestSnapshotBeforeIgnoresNewerSnapshots(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, ts := range []int64{100, 200} { // 100 = pre-boot, 200 = post-start save
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	ev, err := db.LatestSnapshotBefore(ctx, 150)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || ev.Ts != 100 {
		t.Fatalf("LatestSnapshotBefore(150) = %+v, want Ts=100", ev)
	}

	ev, err = db.LatestSnapshotBefore(ctx, 100) // strict <: equal ts excluded
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Errorf("LatestSnapshotBefore(100) = %+v, want nil", ev)
	}
}

func TestEventsAreScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/run/user/1000/tmux-1000/default")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/tmp/tmux-1000/default")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	ev := func(ts int64) store.Event {
		return store.Event{Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}
	}
	if _, err := a.InsertEvent(ctx, ev(1000)); err != nil {
		t.Fatalf("insert into a: %v", err)
	}
	// Newer, and in the other lane: the bug this partition exists to stop is
	// lane a resolving a close against this row.
	if _, err := b.InsertEvent(ctx, ev(2000)); err != nil {
		t.Fatalf("insert into b: %v", err)
	}

	snap, err := a.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap == nil || snap.Ts != 1000 {
		t.Fatalf("lane a LatestSnapshot = %+v, want ts 1000", snap)
	}

	before, err := a.LatestSnapshotBefore(ctx, 3000)
	if err != nil {
		t.Fatalf("LatestSnapshotBefore: %v", err)
	}
	if before == nil || before.Ts != 1000 {
		t.Fatalf("lane a LatestSnapshotBefore = %+v, want ts 1000", before)
	}

	evs, err := a.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("lane a ListEvents returned %d events, want 1", len(evs))
	}
}

// TestMigration0002ClearsPopulatedV1Database builds a v1 database with real
// rows, then confirms Open's 0002 migration deletes events and lets the
// event_scrollbacks cascade drive scrollback refcounts to zero — the chain
// gc depends on to collect orphaned blob files. Every other test opens a
// fresh, empty file, so this is the only test that exercises the DELETE FROM
// events statement against data.
func TestMigration0002ClearsPopulatedV1Database(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	// Same DSN pragmas as store.Open, so the rows seeded here are genuinely
	// FK-valid — the cascade that fires on Open's own connection during the
	// migration depends on Open's pragmas, not this handle's.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", dbPath)
	v1db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open v1 db: %v", err)
	}

	body, err := fs.ReadFile(migrations.FS, "0001_initial.sql")
	if err != nil {
		t.Fatalf("read 0001_initial.sql: %v", err)
	}
	if _, err := v1db.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("apply 0001_initial.sql: %v", err)
	}
	if _, err := v1db.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
		t.Fatalf("set user_version=1: %v", err)
	}

	res, err := v1db.ExecContext(ctx, `
		INSERT INTO events (ts, kind, scope, host, manifest_json)
		VALUES (1000, 'snapshot', 'session', 'h', '{}')
	`)
	if err != nil {
		t.Fatalf("insert v1 event: %v", err)
	}
	eventID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("event LastInsertId: %v", err)
	}
	if _, err := v1db.ExecContext(ctx, `
		INSERT INTO scrollbacks (sha256, bytes, refcount, last_used_ts) VALUES ('sha1', 10, 1, 1)
	`); err != nil {
		t.Fatalf("insert v1 scrollback: %v", err)
	}
	if _, err := v1db.ExecContext(ctx, `
		INSERT INTO event_scrollbacks (event_id, pane_key, scrollback_sha) VALUES (?, 's:1:1', 'sha1')
	`, eventID); err != nil {
		t.Fatalf("insert v1 event_scrollback: %v", err)
	}

	if err := v1db.Close(); err != nil {
		t.Fatalf("close v1 db: %v", err)
	}

	s, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open (applies 0002): %v", err)
	}
	defer s.Close()

	var eventCount int
	if err := s.DB().QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 0 {
		t.Errorf("events count = %d, want 0", eventCount)
	}

	var linkCount int
	if err := s.DB().QueryRowContext(ctx, "SELECT count(*) FROM event_scrollbacks").Scan(&linkCount); err != nil {
		t.Fatalf("count event_scrollbacks: %v", err)
	}
	if linkCount != 0 {
		t.Errorf("event_scrollbacks count = %d, want 0", linkCount)
	}

	var refcount int
	if err := s.DB().QueryRowContext(ctx, "SELECT refcount FROM scrollbacks WHERE sha256 = 'sha1'").Scan(&refcount); err != nil {
		t.Fatalf("read refcount: %v", err)
	}
	if refcount != 0 {
		t.Errorf("scrollback refcount = %d, want 0", refcount)
	}

	var version int
	if err := s.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 2 {
		t.Errorf("user_version = %d, want 2", version)
	}
}

func TestPruneIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	now := time.Now().UnixMilli()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	// Same day so the per-day retention floor cannot rescue anything, and
	// older than a week so it does not apply at all.
	base := now - 30*24*int64(time.Hour/time.Millisecond)
	for i := 0; i < 5; i++ {
		snap := store.Event{Ts: base + int64(i), Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}
		if _, err := a.InsertEvent(ctx, snap); err != nil {
			t.Fatalf("insert a snapshot: %v", err)
		}
		if _, err := b.InsertEvent(ctx, snap); err != nil {
			t.Fatalf("insert b snapshot: %v", err)
		}
	}

	if err := a.PruneSnapshots(ctx, 2, now); err != nil {
		t.Fatalf("PruneSnapshots: %v", err)
	}

	aEvs, err := a.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents a: %v", err)
	}
	if len(aEvs) != 2 {
		t.Errorf("lane a kept %d snapshots, want 2", len(aEvs))
	}
	bEvs, err := b.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents b: %v", err)
	}
	if len(bEvs) != 5 {
		t.Errorf("lane b kept %d snapshots, want 5 — pruning lane a must not touch lane b", len(bEvs))
	}
}

func TestPruneCloseEventsIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	// Lane a's snapshot floor sits above every one of lane b's closes. Unscoped,
	// PruneUnresolvableCloseEvents would delete all of lane b's history.
	if _, err := a.InsertEvent(ctx, store.Event{Ts: 9000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}); err != nil {
		t.Fatalf("insert a snapshot: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := b.InsertEvent(ctx, store.Event{Ts: int64(100 + i), Kind: "pane-died", Scope: "pane", Host: "h", ManifestJSON: "{}"}); err != nil {
			t.Fatalf("insert b close: %v", err)
		}
	}

	if _, err := a.PruneCloseEvents(ctx, 50); err != nil {
		t.Fatalf("PruneCloseEvents: %v", err)
	}

	bEvs, err := b.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents b: %v", err)
	}
	if len(bEvs) != 3 {
		t.Errorf("lane b kept %d close events, want 3", len(bEvs))
	}
}

func TestDeleteEventsIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	aID, err := a.InsertEvent(ctx, store.Event{Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"})
	if err != nil {
		t.Fatalf("insert a event: %v", err)
	}
	bID, err := b.InsertEvent(ctx, store.Event{Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"})
	if err != nil {
		t.Fatalf("insert b event: %v", err)
	}

	// Lane a deliberately passes lane b's id alongside its own — DeleteEvents
	// must not reach across lanes even when handed one, but must still
	// delete the id that is actually lane a's.
	if err := a.DeleteEvents(ctx, []int64{aID, bID}); err != nil {
		t.Fatalf("DeleteEvents: %v", err)
	}

	aEvs, err := a.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents a: %v", err)
	}
	if len(aEvs) != 0 {
		t.Errorf("lane a kept %d events, want 0 — DeleteEvents must delete lane a's own id", len(aEvs))
	}

	bEvs, err := b.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents b: %v", err)
	}
	if len(bEvs) != 1 {
		t.Errorf("lane b kept %d events, want 1 — lane a must not delete lane b's event", len(bEvs))
	}
}

func TestMetaIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	if err := a.SetMeta(ctx, "last_save_ts", "1000"); err != nil {
		t.Fatalf("SetMeta a: %v", err)
	}
	if err := b.SetMeta(ctx, "last_save_ts", "2000"); err != nil {
		t.Fatalf("SetMeta b: %v", err)
	}

	got, err := a.GetMeta(ctx, "last_save_ts")
	if err != nil {
		t.Fatalf("GetMeta a: %v", err)
	}
	if got != "1000" {
		t.Errorf("lane a last_save_ts = %q, want %q", got, "1000")
	}
}

func TestListAndDeleteServerLanes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	if _, err := a.InsertEvent(ctx, store.Event{Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if _, err := b.InsertEvent(ctx, store.Event{Ts: 2000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}); err != nil {
		t.Fatalf("insert b: %v", err)
	}
	if err := b.SetMeta(ctx, "last_save_ts", "2000"); err != nil {
		t.Fatalf("SetMeta b: %v", err)
	}

	lanes, err := a.ListServerLanes(ctx)
	if err != nil {
		t.Fatalf("ListServerLanes: %v", err)
	}
	want := map[string]int64{"/sock/a": 1000, "/sock/b": 2000}
	if len(lanes) != len(want) {
		t.Fatalf("ListServerLanes returned %d lanes, want %d", len(lanes), len(want))
	}
	for _, lane := range lanes {
		if ts, ok := want[lane.Key]; !ok || ts != lane.NewestTs {
			t.Errorf("lane %q newest = %d, want %d", lane.Key, lane.NewestTs, want[lane.Key])
		}
	}

	// Reaping from lane a must clear lane b's events and its server_state.
	n, err := a.DeleteServerLane(ctx, "/sock/b")
	if err != nil {
		t.Fatalf("DeleteServerLane: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteServerLane removed %d events, want 1", n)
	}
	got, err := b.GetMeta(ctx, "last_save_ts")
	if err != nil {
		t.Fatalf("GetMeta b: %v", err)
	}
	if got != "" {
		t.Errorf("lane b last_save_ts = %q after reaping, want empty", got)
	}
	if evs, err := a.ListEvents(ctx, store.ListOpts{}); err != nil || len(evs) != 1 {
		t.Errorf("lane a has %d events (err %v), want 1 — reaping b must not touch a", len(evs), err)
	}
}
