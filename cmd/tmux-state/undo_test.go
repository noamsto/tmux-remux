package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-state/internal/closeevent"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
)

// seedStore returns an open store with a single snapshot capturing one window
// (mono:4, id @9) plus whatever close events the test inserts on top.
func seedStore(ctx context.Context, t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	snap := snapshot.Manifest{V: 1, Host: "h", SavedAt: 100, Sessions: []snapshot.Session{{
		Name: "mono",
		Windows: []snapshot.Window{{
			Index: 4, Name: "win", Layout: "L", ID: "@9",
			Panes: []snapshot.Pane{{Index: 1, Cwd: "/m", Command: "fish", ID: "%9"}},
		}},
	}}}
	insertEvent(ctx, t, db, 100, "snapshot", string(mustJSON(t, snap)))
	return db
}

func insertEvent(ctx context.Context, t *testing.T, db *store.Store, ts int64, kind, manifest string) int64 {
	t.Helper()
	id, err := db.InsertEvent(ctx, store.Event{Ts: ts, Kind: kind, Scope: "server", Host: "h", ManifestJSON: manifest})
	if err != nil {
		t.Fatalf("insert %s: %v", kind, err)
	}
	return id
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// closeWindowManifest builds a window-unlinked CloseManifest naming the closed
// window id. The post-close index stays empty — resolution keys off the id.
func closeWindowManifest(t *testing.T, closedID string) string {
	t.Helper()
	return string(mustJSON(t, closeevent.CloseManifest{WindowID: closedID}))
}

func TestRestorableCloseSkipsUnrecoverableHead(t *testing.T) {
	ctx := context.Background()
	db := seedStore(ctx, t)

	// Recoverable: @9 is in the snapshot, and it's gone from the post-close index.
	recoverable := insertEvent(ctx, t, db, 200, "window-unlinked", closeWindowManifest(t, "@9"))
	// Newer but unrecoverable: @14 was born+died inside a snapshot gap, so it
	// never made it into the snapshot. It must not block undo.
	insertEvent(ctx, t, db, 300, "window-unlinked", closeWindowManifest(t, "@14"))

	ev, item, prior, ok, err := restorableClose(ctx, db)
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if !ok {
		t.Fatal("expected a recoverable event, got none")
	}
	if ev.ID != recoverable {
		t.Errorf("popped event %d, want %d (the recoverable one behind the unrecoverable head)", ev.ID, recoverable)
	}
	if m := item.SubManifest(prior.Host, prior.SavedAt); len(m.Sessions) != 1 || m.Sessions[0].Name != "mono" {
		t.Errorf("manifest = %+v, want one session 'mono'", m.Sessions)
	}
}

func TestRestorableClosePicksLonePane(t *testing.T) {
	ctx := context.Background()
	db := seedStore(ctx, t)

	insertEvent(ctx, t, db, 200, "window-unlinked", closeWindowManifest(t, "@9"))
	// A lone pane-died is now recoverable (its parent window @9 is in the
	// snapshot), so it wins the head over the older window close.
	paneMan := string(mustJSON(t, closeevent.CloseManifest{PaneID: "%9", WindowID: "@9"}))
	pane := insertEvent(ctx, t, db, 300, "pane-died", paneMan)

	ev, item, _, ok, err := restorableClose(ctx, db)
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if !ok || ev.ID != pane {
		t.Fatalf("popped event %d ok=%v, want the pane event %d", ev.ID, ok, pane)
	}
	if item.Pane == nil || item.Pane.ID != "%9" {
		t.Errorf("item.Pane = %+v, want the lost pane %%9", item.Pane)
	}
	if item.Window == nil || item.Window.ID != "@9" {
		t.Errorf("item.Window = %+v, want parent window @9", item.Window)
	}
}

func TestRestorableCloseEmptyWhenNothingRecoverable(t *testing.T) {
	ctx := context.Background()
	db := seedStore(ctx, t)
	insertEvent(ctx, t, db, 300, "window-unlinked", closeWindowManifest(t, "@14"))

	_, _, _, ok, err := restorableClose(ctx, db)
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if ok {
		t.Error("expected no recoverable event, got one")
	}
}
