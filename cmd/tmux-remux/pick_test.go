package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/picker"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/tmux"
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

	kept, hidden := partitionRecoverable(evs, ctxs, nil)
	if len(kept) != 1 || kept[0].ID != 1 {
		t.Fatalf("kept = %+v, want only event 1", kept)
	}
	if hidden != 2 {
		t.Errorf("hidden = %d, want 2", hidden)
	}
}

// Capture drops closes inside a bridge mirror at the source, but a store keeps
// every one recorded before that drop worked — 376 of them on the machine this
// was found on. Listing one offers to inject a window into a rendering of a
// remote, so a close whose session is a mirror right now is hidden the same way
// an unrecoverable one is.
func TestPartitionRecoverableHidesLiveBridgeMirrors(t *testing.T) {
	evs := []store.Event{{ID: 1}, {ID: 2}}
	ctxs := map[int64]picker.CloseContext{
		1: {
			Placement:   picker.ClosePlacement{Session: "mono", WindowIndex: 1, Scope: "window"},
			SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{Name: "mono"}}},
		},
		2: {
			Placement:   picker.ClosePlacement{Session: "halo-houston", WindowIndex: 1, Scope: "window"},
			SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{Name: "halo-houston"}}},
		},
	}

	kept, hidden := partitionRecoverable(evs, ctxs, map[string]bool{"halo-houston": true})
	if len(kept) != 1 || kept[0].ID != 1 {
		t.Fatalf("kept = %+v, want only event 1 (the close outside the mirror)", kept)
	}
	if hidden != 1 {
		t.Errorf("hidden = %d, want 1", hidden)
	}
}

// bridgedSessions reads the answer off a live list-sessions, which is the only
// source that cannot be older than the bridge being asked about.
func TestBridgedSessions(t *testing.T) {
	got := bridgedSessions([]tmux.SessionRow{
		{Name: "mono"},
		{Name: "halo-houston", BridgeHost: "halo"},
		{Name: "halo-lazytmux", BridgeHost: "halo"},
	})
	if len(got) != 2 || !got["halo-houston"] || !got["halo-lazytmux"] {
		t.Errorf("bridgedSessions = %v, want the two @bridge_host sessions", got)
	}
}

// A close event resolved against a throttled (scrollback-skipped) prior
// snapshot must carry that flag onto its sub-manifest — otherwise the close
// picker has no way to tell a throttle-caused empty capture from a genuinely
// unexplained one (see internal/picker/preview.go's closePaneContent).
func TestBuildCloseContextsPropagatesScrollbackSkipped(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	snap := snapshot.Manifest{
		V: 1, Host: "h", SavedAt: 1000, ScrollbackSkipped: true,
		Sessions: []snapshot.Session{
			{Name: "gone", Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Index: 1}}}}},
			{Name: "s1", Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Index: 1}}}}},
		},
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(snapJSON),
	}); err != nil {
		t.Fatal(err)
	}

	// "gone" is missing from the post-close index; "s1" survives — the diff
	// resolves this as the "gone" session having closed.
	closeMan := closeevent.CloseManifest{
		SessionName: "gone",
		Index:       closeevent.IndexPost{Windows: []tmux.WindowRow{{Session: "s1", Index: 1}}},
	}
	closeJSON, err := json.Marshal(closeMan)
	if err != nil {
		t.Fatal(err)
	}
	evs := []store.Event{{ID: 42, Ts: 2000, Kind: "session-closed", ManifestJSON: string(closeJSON)}}

	ctxs := buildCloseContexts(ctx, db, evs)
	cc, ok := ctxs[42]
	if !ok {
		t.Fatalf("no CloseContext resolved for the close event; ctxs = %+v", ctxs)
	}
	if !cc.SubManifest.ScrollbackSkipped {
		t.Error("CloseContext.SubManifest.ScrollbackSkipped = false, want true (must propagate from the prior snapshot)")
	}
}

// An embedded entity resolved via the capture-time fallback can carry real
// scrollback even though the freshly looked-up prior snapshot was throttled
// — they can come from different snapshots. The sub-manifest must reflect
// the embedded entity's own scrollback, not prior's throttle status.
func TestBuildCloseContextsScrollbackSkippedReflectsResolvedItem(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Throttled, and empty of sessions — the diff can never resolve this
	// close against it, forcing Resolve onto the embedded path.
	snap := snapshot.Manifest{V: 1, Host: "h", SavedAt: 1000, ScrollbackSkipped: true}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertEvent(ctx, store.Event{
		Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(snapJSON),
	}); err != nil {
		t.Fatal(err)
	}

	embedded := closeevent.ClosedItem{
		SessionName: "s1",
		WindowIndex: 1,
		Window:      &snapshot.Window{Index: 1, Panes: []snapshot.Pane{{Index: 1, ScrollbackSHA: "deadbeef"}}},
		Pane:        &snapshot.Pane{Index: 1, ScrollbackSHA: "deadbeef"},
	}
	closeMan := closeevent.CloseManifest{
		PaneID:   "%1",
		Resolved: &closeevent.ResolvedClose{Item: embedded, SavedAt: 500},
	}
	closeJSON, err := json.Marshal(closeMan)
	if err != nil {
		t.Fatal(err)
	}
	evs := []store.Event{{ID: 7, Ts: 2000, Kind: "pane-died", Host: "h", ManifestJSON: string(closeJSON)}}

	ctxs := buildCloseContexts(ctx, db, evs)
	cc, ok := ctxs[7]
	if !ok {
		t.Fatalf("no CloseContext resolved for the close event; ctxs = %+v", ctxs)
	}
	if cc.SubManifest.ScrollbackSkipped {
		t.Error("CloseContext.SubManifest.ScrollbackSkipped = true, want false: the embedded entity carries real scrollback")
	}
}

// An event already in the store carries no embedded scrollback when the
// snapshot it was captured against was throttled. The preview would have
// nothing to draw, so resolving it looks back past the throttled snapshot for
// the pane's own last capture — which is also what clears the "skipped" note.
func TestBuildCloseContextsAdoptsScrollbackFromAnEarlierSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	window := func(sha string) snapshot.Window {
		return snapshot.Window{Index: 1, Name: "w", ID: "@1", Panes: []snapshot.Pane{
			{Index: 1, ID: "%1"},
			{Index: 2, ID: "%2", ScrollbackSHA: sha},
		}}
	}
	insert := func(ts int64, skipped bool, sha string) {
		t.Helper()
		body, err := json.Marshal(snapshot.Manifest{
			V: 1, Host: "h", SavedAt: ts, ScrollbackSkipped: skipped,
			Sessions: []snapshot.Session{{Name: "work", Windows: []snapshot.Window{window(sha)}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(body),
		}); err != nil {
			t.Fatal(err)
		}
	}
	insert(1000, false, "deadbeef")
	insert(2000, true, "")

	// %2 is gone from the post-close index, so the diff resolves the close
	// against the throttled snapshot — the one carrying no scrollback at all.
	closeMan := closeevent.CloseManifest{
		SessionName: "work", WindowID: "@1", PaneID: "%2",
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{{Session: "work", Index: 1, ID: "@1"}},
			Panes:   []tmux.PaneRow{{Session: "work", WindowIndex: 1, PaneIndex: 1, ID: "%1"}},
		},
	}
	closeJSON, err := json.Marshal(closeMan)
	if err != nil {
		t.Fatal(err)
	}
	evs := []store.Event{{ID: 9, Ts: 3000, Kind: "pane-died", Host: "h", ManifestJSON: string(closeJSON)}}

	ctxs := buildCloseContexts(ctx, db, evs)
	cc, ok := ctxs[9]
	if !ok {
		t.Fatalf("no CloseContext resolved for the close event; ctxs = %+v", ctxs)
	}
	if !subManifestHasScrollback(cc.SubManifest) {
		t.Errorf("sub-manifest carries no scrollback; want the capture from the earlier snapshot: %+v", cc.SubManifest)
	}
	if cc.SubManifest.ScrollbackSkipped {
		t.Error("SubManifest.ScrollbackSkipped = true, want false: the close now carries the pane's own capture")
	}
}
