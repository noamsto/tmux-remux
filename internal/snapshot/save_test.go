package snapshot_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/noamsto/tmux-state/internal/scrollback"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
	"github.com/noamsto/tmux-state/internal/tmux"
)

type captureClient struct {
	*fakeClient
	captured map[string][]byte
}

func (c *captureClient) CapturePane(_ context.Context, target string) ([]byte, error) {
	if v, ok := c.captured[target]; ok {
		return v, nil
	}
	return []byte("default"), nil
}

func TestSaveInsertsEventAndScrollbacks(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	scrollDir := filepath.Join(dir, "scrollbacks")
	ctx := context.Background()

	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sb := scrollback.New(scrollDir)

	cc := &captureClient{
		fakeClient: &fakeClient{
			sessions: []tmux.SessionRow{{Name: "s1", LastAttached: 100}},
			windows:  []tmux.WindowRow{{Session: "s1", Index: 1, Name: "w1", Layout: "L"}},
			panes:    []tmux.PaneRow{{Session: "s1", WindowIndex: 1, PaneIndex: 1, Cwd: "/x", Command: "nvim", PID: 1, LastUsed: 1}},
		},
		captured: map[string][]byte{"s1:1.1": []byte("hello")},
	}

	saver := snapshot.NewSaver(db, sb, cc, snapshot.SaverOptions{
		Host: "test", CaptureScrollback: true, MinSaveInterval: 0,
	})

	if err := saver.Save(ctx, "test"); err != nil {
		t.Fatal(err)
	}

	ev, err := db.LatestSnapshot(ctx)
	if err != nil || ev == nil {
		t.Fatalf("LatestSnapshot = %v, %v", ev, err)
	}
	rows, err := db.DB().QueryContext(ctx, "SELECT scrollback_sha FROM event_scrollbacks WHERE event_id=?", ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Error("expected at least one event_scrollback row")
	}
}

func TestSaveSkipsWhenFingerprintUnchangedAndThrottled(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, _ := store.Open(ctx, filepath.Join(dir, "test.db"))
	defer db.Close()
	sb := scrollback.New(filepath.Join(dir, "scrollbacks"))
	cc := &captureClient{
		fakeClient: &fakeClient{
			sessions: []tmux.SessionRow{{Name: "s1"}},
			windows:  []tmux.WindowRow{{Session: "s1", Index: 1, Name: "w"}},
			panes:    []tmux.PaneRow{{Session: "s1", WindowIndex: 1, PaneIndex: 1, Command: "bash"}},
		},
		captured: map[string][]byte{},
	}
	saver := snapshot.NewSaver(db, sb, cc, snapshot.SaverOptions{
		Host: "h", CaptureScrollback: false, MinSaveInterval: time.Hour,
	})
	if err := saver.Save(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := saver.Save(ctx, "second"); err != nil {
		t.Fatal(err)
	}

	all, _ := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"snapshot"}, Limit: 100})
	if len(all) != 1 {
		t.Errorf("expected 1 event (second was throttled), got %d", len(all))
	}
}

// TestSaveSkipsWhenNoSessions covers the "no tmux server running" case:
// Build returns an empty manifest, and Save must not insert an event.
// Without this guard the systemd save timer pollutes the event log with
// sessions:null rows, which `restore` then picks as "latest snapshot".
func TestSaveSkipsWhenNoSessions(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, _ := store.Open(ctx, filepath.Join(dir, "test.db"))
	defer db.Close()
	sb := scrollback.New(filepath.Join(dir, "scrollbacks"))

	empty := &captureClient{fakeClient: &fakeClient{}, captured: map[string][]byte{}}
	saver := snapshot.NewSaver(db, sb, empty, snapshot.SaverOptions{Host: "h"})

	if err := saver.Save(ctx, "timer"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	all, _ := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"snapshot"}, Limit: 10})
	if len(all) != 0 {
		t.Errorf("expected 0 snapshot events, got %d", len(all))
	}
}
