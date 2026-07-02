package closeevent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-state/internal/closeevent"
	"github.com/noamsto/tmux-state/internal/store"
	"github.com/noamsto/tmux-state/internal/tmux"
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
