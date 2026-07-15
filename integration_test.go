//go:build integration

package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/noamsto/tmux-remux/internal/filter"
	"github.com/noamsto/tmux-remux/internal/restore"
	"github.com/noamsto/tmux-remux/internal/scrollback"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/tmux"
	"github.com/noamsto/tmux-remux/testutil"
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
	return tmux.ParseWindows(out, nil)
}
func (s scopedTmux) ListPanes(ctx context.Context) ([]tmux.PaneRow, error) {
	out, err := s.Run(ctx, []string{"list-panes", "-a", "-F", "#{session_name}\x1f#{window_index}\x1f#{pane_index}\x1f#{pane_current_path}\x1f#{pane_current_command}\x1f#{pane_pid}\x1f#{pane_last_used}\x1f#{pane_id}\x1f#{@remux_relaunch}"})
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

// TestDecorationRestoreRoundtrip captures decoration options (@crew_name,
// @crew_color) from a real tmux server into a manifest, then replays the
// resulting restore plan against a fresh server and confirms the options
// land back on the recreated window. tmux.Client has no -S flag of its own —
// it resolves its target socket from $TMUX (see withSynthesizedTmuxEnv in
// internal/tmux/client.go) — so each phase points Client at the right server
// by setting TMUX to that server's socket.
func TestDecorationRestoreRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	src := testutil.StartServer(t)
	if _, err := src.Tmux("rename-session", "-t", "init", "deco"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-window-option", "-t", "deco:0", "@crew_color", "colour141"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-window-option", "-t", "deco:0", "@crew_name", "dispatcher"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TMUX", src.Socket+",0,0")
	srcClient := tmux.NewClient("tmux", "@crew_name", "@crew_color")

	ctx := context.Background()
	m, err := snapshot.Build(ctx, srcClient, "test", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}

	var win *snapshot.Window
	for i := range m.Sessions {
		if m.Sessions[i].Name != "deco" {
			continue
		}
		for j := range m.Sessions[i].Windows {
			if m.Sessions[i].Windows[j].Index == 0 {
				win = &m.Sessions[i].Windows[j]
				break
			}
		}
	}
	if win == nil {
		t.Fatal("deco window missing from manifest")
	}
	wantDecoration := map[string]string{"@crew_name": "dispatcher", "@crew_color": "colour141"}
	if !reflect.DeepEqual(win.Decoration, wantDecoration) {
		t.Errorf("captured Decoration = %#v, want %#v", win.Decoration, wantDecoration)
	}

	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{})

	dst := testutil.StartServer(t)
	t.Setenv("TMUX", dst.Socket+",0,0")
	dstClient := tmux.NewClient("tmux")
	if err := restore.Apply(ctx, dstClient, plan); err != nil {
		t.Fatalf("apply: %v", err)
	}

	out, err := dst.Tmux("show-options", "-w", "-v", "-t", "deco:0", "@crew_color")
	if err != nil {
		t.Fatalf("show-options: %v", err)
	}
	if got := strings.TrimSpace(out); got != "colour141" {
		t.Errorf("restored @crew_color = %q, want %q", got, "colour141")
	}
}
