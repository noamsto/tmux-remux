package main

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/picker"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

func TestPartitionRecoverable(t *testing.T) {
	evs := []store.Event{
		{ID: 1}, // recoverable: non-empty sub-manifest
		{ID: 2}, // unrecoverable: no context entry at all
		{ID: 3}, // unrecoverable: context present but empty sub-manifest
	}
	ctxs := map[int64]picker.CloseContext{
		1: {
			Label:       "mono/win (1p)",
			SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{Name: "mono"}}},
		},
		3: {Label: "window-unlinked"},
	}

	kept, hidden := partitionRecoverable(evs, ctxs)
	if len(kept) != 1 || kept[0].ID != 1 {
		t.Fatalf("kept = %+v, want only event 1", kept)
	}
	if hidden != 2 {
		t.Errorf("hidden = %d, want 2", hidden)
	}
}
