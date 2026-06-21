//go:build integration

package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-state/internal/restore"
	"github.com/noamsto/tmux-state/internal/scrollback"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
	"github.com/noamsto/tmux-state/internal/tmux"
	"github.com/noamsto/tmux-state/testutil"
)

// scopedTmux runs tmux against a specific socket. Implements both the Lister
// and CaptureLister interfaces consumed by snapshot.Saver.
type scopedTmux struct {
	socket string
}

func (s scopedTmux) Run(ctx context.Context, args []string) (string, error) {
	full := append([]string{"-f", "/dev/null", "-u", "-S", s.socket}, args...)
	cmd := exec.CommandContext(ctx, "tmux", full...) //nolint:gosec
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
func (s scopedTmux) ListSessions(ctx context.Context) ([]tmux.SessionRow, error) {
	out, err := s.Run(ctx, []string{"list-sessions", "-F", "#{session_name}\x1f#{session_last_attached}"})
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	return tmux.ParseSessions(out)
}
func (s scopedTmux) ListWindows(ctx context.Context) ([]tmux.WindowRow, error) {
	out, err := s.Run(ctx, []string{"list-windows", "-a", "-F", "#{session_name}\x1f#{window_index}\x1f#{window_name}\x1f#{window_layout}\x1f#{window_id}\x1f#{E:automatic-rename}"})
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	return tmux.ParseWindows(out)
}
func (s scopedTmux) ListPanes(ctx context.Context) ([]tmux.PaneRow, error) {
	out, err := s.Run(ctx, []string{"list-panes", "-a", "-F", "#{session_name}\x1f#{window_index}\x1f#{pane_index}\x1f#{pane_current_path}\x1f#{pane_current_command}\x1f#{pane_pid}\x1f#{pane_last_used}\x1f#{pane_id}"})
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	return tmux.ParsePanes(out)
}
func (s scopedTmux) CapturePane(ctx context.Context, target string) ([]byte, error) {
	out, err := s.Run(ctx, []string{"capture-pane", "-pJ", "-t", target, "-S", "-"})
	return []byte(out), err
}

func TestSaveRestoreRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := testutil.StartServer(t)
	st := scopedTmux{socket: srv.Socket}

	if _, err := srv.Tmux("rename-session", "-t", "init", "lazytmux"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Tmux("new-window", "-t", "lazytmux", "-n", "build"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Tmux("split-window", "-t", "lazytmux:1"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	scrollDir := filepath.Join(dir, "sb")
	ctx := context.Background()

	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sb := scrollback.New(scrollDir)
	saver := snapshot.NewSaver(db, sb, st, snapshot.SaverOptions{Host: "test", CaptureScrollback: true})
	if err := saver.Save(ctx, "integration"); err != nil {
		t.Fatalf("save: %v", err)
	}

	ev, _ := db.LatestSnapshot(ctx)
	if ev == nil {
		t.Fatal("no snapshot")
	}

	var m snapshot.Manifest
	if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Sessions) == 0 {
		t.Error("manifest missing sessions")
	}
	hasLazytmux := false
	for _, s := range m.Sessions {
		if s.Name == "lazytmux" {
			hasLazytmux = true
		}
	}
	if !hasLazytmux {
		t.Error("manifest missing lazytmux session")
	}
}

func TestPaneRestoreSplitsIntoLiveWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := testutil.StartServer(t)
	st := scopedTmux{socket: srv.Socket}

	// The default window starts with one pane; split to two.
	if _, err := srv.Tmux("split-window", "-t", "init"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sb := scrollback.New(filepath.Join(dir, "sb"))
	saver := snapshot.NewSaver(db, sb, st, snapshot.SaverOptions{Host: "test"})
	if err := saver.Save(ctx, "integration"); err != nil {
		t.Fatalf("save: %v", err)
	}

	ev, _ := db.LatestSnapshot(ctx)
	var m snapshot.Manifest
	if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
		t.Fatal(err)
	}
	win := m.Sessions[0].Windows[0]
	if len(win.Panes) != 2 {
		t.Fatalf("snapshot window has %d panes, want 2", len(win.Panes))
	}
	lost := win.Panes[1]

	// Kill the second pane; the window stays live with its first pane.
	if _, err := srv.Tmux("kill-pane", "-t", fmt.Sprintf("init:%d.%d", win.Index, lost.Index)); err != nil {
		t.Fatal(err)
	}
	if n := panesInWindow(t, st, win.Index); n != 1 {
		t.Fatalf("after kill: %d panes, want 1", n)
	}

	plan := restore.BuildPaneRestore(lost, win, "init", true, restore.BuildOptions{DefaultShell: "/bin/sh"})
	if err := restore.Apply(ctx, st, plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if n := panesInWindow(t, st, win.Index); n != 2 {
		t.Errorf("after restore: %d panes, want 2 (the lost pane split back in)", n)
	}
}

func panesInWindow(t *testing.T, st scopedTmux, windowIndex int) int {
	t.Helper()
	panes, err := st.ListPanes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range panes {
		if p.WindowIndex == windowIndex {
			n++
		}
	}
	return n
}
