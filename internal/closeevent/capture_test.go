package closeevent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/tmux"
)

func TestCaptureSessionInsertsRow(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "session-closed", SessionID: "$1", Host: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected event id > 0")
	}

	all, _ := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if len(all) != 1 || all[0].Kind != "session-closed" {
		t.Errorf("expected one session-closed event, got %v", all)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(all[0].ManifestJSON), &m); err != nil {
		t.Errorf("manifest must be valid json: %v", err)
	}
}

func TestCaptureStoresProvidedPostCloseIndex(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "window-unlinked", WindowID: "@5", Host: "h",
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{{Session: "s1", Index: 1, ID: "@1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected event id > 0")
	}

	all, _ := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	cm, err := closeevent.ParseManifest(all[0].ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if cm.WindowID != "@5" {
		t.Errorf("WindowID = %q, want @5", cm.WindowID)
	}
	if len(cm.Index.Windows) != 1 || cm.Index.Windows[0].ID != "@1" {
		t.Errorf("stored index = %+v, want the provided post-close window @1", cm.Index)
	}
}

func TestCaptureSkipsMovedWindow(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// window-unlinked fires on move-window: @5 still exists in the post-close
	// index under a different session, so nothing was closed.
	id, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "window-unlinked", WindowID: "@5", Host: "h",
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{{Session: "s2", Index: 3, ID: "@5"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Errorf("expected skip (id=0) for a still-live window, got id=%d", id)
	}
}

func TestCaptureSkipsLivePane(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "pane-died", PaneID: "%3", Host: "h",
		Index: closeevent.IndexPost{
			Panes: []tmux.PaneRow{{ID: "%3"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Errorf("expected skip (id=0) for a still-live pane, got id=%d", id)
	}
}

func TestCascadeDedup_WindowSkipsAfterSession(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "session-closed", SessionID: "$1", Host: "h",
	}); err != nil {
		t.Fatal(err)
	}

	// Within the dedup window, window-unlinked of the same session should be skipped.
	id2, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "window-unlinked", SessionID: "$1", WindowID: "@5", Host: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != 0 {
		t.Errorf("expected dedup (id2=0), got id2=%d", id2)
	}
}

// seedSnapshot stores a snapshot of one window (@1) holding two panes.
func seedSnapshot(ctx context.Context, t *testing.T, db *store.Store) {
	t.Helper()
	m := snapshot.Manifest{V: 1, Host: "h", SavedAt: 100, Sessions: []snapshot.Session{{
		Name: "s1",
		Windows: []snapshot.Window{{
			Index: 1, ID: "@1",
			Panes: []snapshot.Pane{
				{Index: 0, ID: "%1", Command: "fish"},
				{Index: 1, ID: "%2", Command: "nvim"},
			},
		}},
	}}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 100, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(b),
	}); err != nil {
		t.Fatal(err)
	}
}

// prefix+x fires after-kill-pane, a command hook carrying no hook_pane. The
// dead pane has to be identified by diffing the survivors against the snapshot.
func TestCaptureResolvesIDLessPaneFromSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSnapshot(ctx, t, db)

	id, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "pane-died", Host: "h",
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{{Session: "s1", Index: 1, ID: "@1"}},
			Panes:   []tmux.PaneRow{{Session: "s1", WindowIndex: 1, PaneIndex: 0, ID: "%1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected the killed pane to be recorded")
	}

	all, _ := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	man, err := closeevent.ParseManifest(all[0].ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if man.PaneID != "%2" || man.WindowID != "@1" {
		t.Errorf("resolved pane=%q window=%q, want %%2 in @1", man.PaneID, man.WindowID)
	}
}

func TestCaptureDropsIDLessPaneWhenAmbiguous(t *testing.T) {
	ctx := context.Background()
	cases := map[string]closeevent.IndexPost{
		// Window gone too: window-unlinked owns this close, and the pane alone
		// can't be restored into a window that no longer exists.
		"window died with the pane": {},
		// Two panes missing at once — a stale snapshot or a bulk teardown.
		// Picking either one would restore the wrong pane.
		"more than one pane missing": {
			Windows: []tmux.WindowRow{{Session: "s1", Index: 1, ID: "@1"}},
		},
	}
	for name, post := range cases {
		t.Run(name, func(t *testing.T) {
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			seedSnapshot(ctx, t, db)

			id, err := closeevent.Capture(ctx, db, closeevent.Args{
				Kind: "pane-died", Host: "h", Index: post,
			})
			if err != nil {
				t.Fatal(err)
			}
			if id != 0 {
				t.Errorf("recorded event %d, want none", id)
			}
		})
	}
}

func TestCaptureStoresSessionName(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "window-unlinked", WindowID: "@5", SessionID: "$1",
		SessionName: "lazytmux", Host: "h",
	}); err != nil {
		t.Fatal(err)
	}

	all, _ := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if len(all) != 1 {
		t.Fatalf("expected one event, got %d", len(all))
	}
	cm, err := closeevent.ParseManifest(all[0].ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if cm.SessionName != "lazytmux" {
		t.Errorf("SessionName = %q, want %q", cm.SessionName, "lazytmux")
	}
}

// A pre-change event has no stored name; parsing must leave it empty rather
// than fail, so the snapshot-diff fallback can take over.
func TestParseManifestWithoutSessionNameLeavesItEmpty(t *testing.T) {
	cm, err := closeevent.ParseManifest(`{"session_id":"$1","window_id":"@5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if cm.SessionName != "" {
		t.Errorf("SessionName = %q, want empty", cm.SessionName)
	}
}
