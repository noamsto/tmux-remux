package closeevent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-state/internal/closeevent"
	"github.com/noamsto/tmux-state/internal/store"
)

func TestCaptureSessionInsertsRow(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	payload := `{"name":"s1","windows":[]}`
	_ = closeevent.UpsertIndex(ctx, db.DB(), "$1", payload)

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

func TestCascadeDedup_WindowSkipsAfterSession(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = closeevent.UpsertIndex(ctx, db.DB(), "$1", `{"name":"s1"}`)

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
