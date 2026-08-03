package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/tmux"
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

func TestRestorableCloseReportsUnrecoverableHead(t *testing.T) {
	ctx := context.Background()
	db := seedStore(ctx, t)

	// Recoverable: @9 is in the snapshot, and it's gone from the post-close index.
	recoverable := insertEvent(ctx, t, db, 200, "window-unlinked", closeWindowManifest(t, "@9"))
	// Newer but unrecoverable: @14 was born+died inside a snapshot gap, so it
	// never made it into the snapshot. It must surface as discarded rather than
	// be stepped over — restoring @9 here would look like undo doing nothing.
	unrecoverable := insertEvent(ctx, t, db, 300, "window-unlinked", closeWindowManifest(t, "@14"))

	target, err := restorableClose(ctx, db)
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if len(target.Discarded) != 1 || target.Discarded[0].ID != unrecoverable {
		t.Fatalf("Discarded = %+v, want just event %d", target.Discarded, unrecoverable)
	}
	if !target.OK {
		t.Fatal("expected a recoverable event behind the discarded head, got none")
	}
	if target.Event.ID != recoverable {
		t.Errorf("target event %d, want %d", target.Event.ID, recoverable)
	}
	if m := target.Item.SubManifest(target.Prior.Host, target.Prior.SavedAt); len(m.Sessions) != 1 || m.Sessions[0].Name != "mono" {
		t.Errorf("manifest = %+v, want one session 'mono'", m.Sessions)
	}
}

func TestDiscardSummaryMentionsFollowUpPress(t *testing.T) {
	evs := []store.Event{{Ts: time.Now().UnixMilli(), Scope: "window"}}
	if got := discardSummary(evs, true); !strings.Contains(got, "prefix+u again") {
		t.Errorf("summary = %q, want a hint to press again", got)
	}
	if got := discardSummary(evs, false); !strings.Contains(got, "nothing older") {
		t.Errorf("summary = %q, want the exhausted-history wording", got)
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

	target, err := restorableClose(ctx, db)
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if !target.OK || target.Event.ID != pane {
		t.Fatalf("popped event %d ok=%v, want the pane event %d", target.Event.ID, target.OK, pane)
	}
	if len(target.Discarded) != 0 {
		t.Errorf("Discarded = %+v, want none", target.Discarded)
	}
	if target.Item.Pane == nil || target.Item.Pane.ID != "%9" {
		t.Errorf("item.Pane = %+v, want the lost pane %%9", target.Item.Pane)
	}
	if target.Item.Window == nil || target.Item.Window.ID != "@9" {
		t.Errorf("item.Window = %+v, want parent window @9", target.Item.Window)
	}
}

func TestReindexIntoLiveSessions(t *testing.T) {
	// One window (mono:4) restored into a session that's still live with its
	// index already taken: closing the window renumbered the rest, so 4 now
	// holds a different window. Pinning 4 would fail new-window with "index in
	// use"; reindex must move it to a free slot past the live max (5 -> 6).
	m := snapshot.Manifest{Sessions: []snapshot.Session{{
		Name:    "mono",
		Windows: []snapshot.Window{{Index: 4, Name: "win"}},
	}}}
	live := []tmux.WindowRow{
		{Session: "mono", Index: 1},
		{Session: "mono", Index: 4},
		{Session: "mono", Index: 5},
		{Session: "other", Index: 4},
	}
	reindexIntoLiveSessions(&m, live)
	if got := m.Sessions[0].Windows[0].Index; got != 6 {
		t.Errorf("collided index = %d, want 6 (past the live max)", got)
	}
}

func TestReindexLeavesFreeIndexAndDeadSessionAlone(t *testing.T) {
	m := snapshot.Manifest{Sessions: []snapshot.Session{
		// Live session, free index -> unchanged.
		{Name: "mono", Windows: []snapshot.Window{{Index: 2}}},
		// Session not currently live (whole-session restore) -> unchanged, it
		// gets created fresh by CreateSession.
		{Name: "gone", Windows: []snapshot.Window{{Index: 4}}},
	}}
	live := []tmux.WindowRow{{Session: "mono", Index: 1}}
	reindexIntoLiveSessions(&m, live)
	if got := m.Sessions[0].Windows[0].Index; got != 2 {
		t.Errorf("free index = %d, want 2 (unchanged)", got)
	}
	if got := m.Sessions[1].Windows[0].Index; got != 4 {
		t.Errorf("dead-session index = %d, want 4 (unchanged)", got)
	}
}

func TestRestorableCloseEmptyWhenNothingRecoverable(t *testing.T) {
	ctx := context.Background()
	db := seedStore(ctx, t)
	unrecoverable := insertEvent(ctx, t, db, 300, "window-unlinked", closeWindowManifest(t, "@14"))

	target, err := restorableClose(ctx, db)
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if target.OK {
		t.Error("expected no recoverable event, got one")
	}
	if len(target.Discarded) != 1 || target.Discarded[0].ID != unrecoverable {
		t.Errorf("Discarded = %+v, want just event %d", target.Discarded, unrecoverable)
	}
}
