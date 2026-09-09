package closeevent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// fillStore opens a store holding snaps, each timestamped by its own SavedAt
// so a test reads as the timeline it is describing.
func fillStore(t *testing.T, snaps ...snapshot.Manifest) *store.Store {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, m := range snaps {
		body, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.InsertEvent(ctx, store.Event{Ts: m.SavedAt, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(body)}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func manifest(savedAt int64, skipped bool, panes ...snapshot.Pane) snapshot.Manifest {
	return snapshot.Manifest{
		V: 1, Host: "h", SavedAt: savedAt, ScrollbackSkipped: skipped,
		Sessions: []snapshot.Session{{
			Name:    "work",
			Windows: []snapshot.Window{{Index: 1, Name: "w", Panes: panes}},
		}},
	}
}

// The bug this fixes: a save inside min_save_interval writes a snapshot with
// no scrollback at all, and a close resolves against whichever snapshot is
// newest before it — so the pane's captured output sits in the store one save
// back, invisible to the close.
func TestFillScrollback_AdoptsTheCaptureOneSaveBack(t *testing.T) {
	const closeTs = 1_000_000
	db := fillStore(t,
		manifest(closeTs-60_000, false, snapshot.Pane{Index: 2, ID: "%2", ScrollbackSHA: "sha-old"}),
		manifest(closeTs-1_000, true, snapshot.Pane{Index: 2, ID: "%2"}),
	)
	item := &closeevent.ClosedItem{
		SessionName: "work", WindowIndex: 1,
		Window: &snapshot.Window{Index: 1, Name: "w"},
		Pane:   &snapshot.Pane{Index: 2, ID: "%2"},
	}
	if !closeevent.FillScrollback(context.Background(), db, item, closeTs-1_000) {
		t.Fatal("FillScrollback reported nothing filled")
	}
	if got := item.Pane.ScrollbackSHA; got != "sha-old" {
		t.Errorf("pane scrollback = %q, want %q", got, "sha-old")
	}
}

// Pane numbers are reused as panes come and go, so a pane whose index matches
// is not the same pane. Adopting its output would show a reader the wrong
// pane's screen and call it the closed one's.
func TestFillScrollback_MatchesOnPaneIDNotIndex(t *testing.T) {
	const closeTs = 1_000_000
	db := fillStore(t,
		manifest(closeTs-60_000, false, snapshot.Pane{Index: 2, ID: "%7", ScrollbackSHA: "sha-someone-else"}),
	)
	item := &closeevent.ClosedItem{
		SessionName: "work", WindowIndex: 1,
		Window: &snapshot.Window{Index: 1, Name: "w"},
		Pane:   &snapshot.Pane{Index: 2, ID: "%2"},
	}
	if closeevent.FillScrollback(context.Background(), db, item, closeTs) {
		t.Error("filled from a pane that only shares an index")
	}
	if item.Pane.ScrollbackSHA != "" {
		t.Errorf("pane scrollback = %q, want it left empty", item.Pane.ScrollbackSHA)
	}
}

// Past the lookback span the capture no longer describes the pane as it was
// when it closed, so the preview's "nothing captured" is the truer answer.
func TestFillScrollback_StopsAtTheLookbackSpan(t *testing.T) {
	const closeTs = 100_000_000
	db := fillStore(t,
		manifest(closeTs-int64(16*time.Minute/time.Millisecond), false, snapshot.Pane{Index: 2, ID: "%2", ScrollbackSHA: "sha-stale"}),
	)
	item := &closeevent.ClosedItem{
		SessionName: "work", WindowIndex: 1,
		Window: &snapshot.Window{Index: 1, Name: "w"},
		Pane:   &snapshot.Pane{Index: 2, ID: "%2"},
	}
	if closeevent.FillScrollback(context.Background(), db, item, closeTs) {
		t.Error("filled from a snapshot older than the lookback span")
	}
}

// A window close lost every pane in the window, so every pane's block in the
// preview is worth filling — and a pane that already carries scrollback keeps
// the newer capture it came with.
func TestFillScrollback_WindowCloseFillsEveryPaneAndKeepsFresherOnes(t *testing.T) {
	const closeTs = 1_000_000
	db := fillStore(t,
		manifest(closeTs-60_000, false,
			snapshot.Pane{Index: 1, ID: "%1", ScrollbackSHA: "sha-1"},
			snapshot.Pane{Index: 2, ID: "%2", ScrollbackSHA: "sha-2"},
		),
	)
	item := &closeevent.ClosedItem{
		SessionName: "work", WindowIndex: 1,
		Window: &snapshot.Window{Index: 1, Name: "w", Panes: []snapshot.Pane{
			{Index: 1, ID: "%1", ScrollbackSHA: "sha-fresh"},
			{Index: 2, ID: "%2"},
		}},
	}
	if !closeevent.FillScrollback(context.Background(), db, item, closeTs) {
		t.Fatal("FillScrollback reported nothing filled")
	}
	if got := item.Window.Panes[0].ScrollbackSHA; got != "sha-fresh" {
		t.Errorf("pane 1 scrollback = %q, want the fresher %q", got, "sha-fresh")
	}
	if got := item.Window.Panes[1].ScrollbackSHA; got != "sha-2" {
		t.Errorf("pane 2 scrollback = %q, want %q", got, "sha-2")
	}
}

// A pane close carries its parent window for the restore to split back into,
// but only the pane that died was lost. The siblings are still running and
// their scrollback is not this close's to show.
func TestFillScrollback_PaneCloseLeavesSurvivingSiblingsAlone(t *testing.T) {
	const closeTs = 1_000_000
	db := fillStore(t,
		manifest(closeTs-60_000, false,
			snapshot.Pane{Index: 1, ID: "%1", ScrollbackSHA: "sha-survivor"},
			snapshot.Pane{Index: 2, ID: "%2", ScrollbackSHA: "sha-victim"},
		),
	)
	item := &closeevent.ClosedItem{
		SessionName: "work", WindowIndex: 1,
		Window: &snapshot.Window{Index: 1, Name: "w", Panes: []snapshot.Pane{{Index: 1, ID: "%1"}}},
		Pane:   &snapshot.Pane{Index: 2, ID: "%2"},
	}
	closeevent.FillScrollback(context.Background(), db, item, closeTs)
	if got := item.Pane.ScrollbackSHA; got != "sha-victim" {
		t.Errorf("closed pane scrollback = %q, want %q", got, "sha-victim")
	}
	if got := item.Window.Panes[0].ScrollbackSHA; got != "" {
		t.Errorf("surviving sibling picked up scrollback %q", got)
	}
}

// A session close lost every window, so panes across all of them are worth
// filling.
func TestFillScrollback_SessionCloseSpansEveryWindow(t *testing.T) {
	const closeTs = 1_000_000
	prior := snapshot.Manifest{
		V: 1, Host: "h", SavedAt: closeTs - 60_000,
		Sessions: []snapshot.Session{{Name: "work", Windows: []snapshot.Window{
			{Index: 1, Name: "a", Panes: []snapshot.Pane{{Index: 1, ID: "%1", ScrollbackSHA: "sha-a"}}},
			{Index: 2, Name: "b", Panes: []snapshot.Pane{{Index: 1, ID: "%2", ScrollbackSHA: "sha-b"}}},
		}}},
	}
	db := fillStore(t, prior)
	item := &closeevent.ClosedItem{
		SessionName: "work",
		Session: &snapshot.Session{Name: "work", Windows: []snapshot.Window{
			{Index: 1, Name: "a", Panes: []snapshot.Pane{{Index: 1, ID: "%1"}}},
			{Index: 2, Name: "b", Panes: []snapshot.Pane{{Index: 1, ID: "%2"}}},
		}},
	}
	if !closeevent.FillScrollback(context.Background(), db, item, closeTs) {
		t.Fatal("FillScrollback reported nothing filled")
	}
	for i, want := range []string{"sha-a", "sha-b"} {
		if got := item.Session.Windows[i].Panes[0].ScrollbackSHA; got != want {
			t.Errorf("window %d pane scrollback = %q, want %q", i+1, got, want)
		}
	}
}
