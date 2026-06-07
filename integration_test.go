//go:build integration

package main_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

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
	out, err := s.Run(ctx, []string{"list-windows", "-a", "-F", "#{session_name}\x1f#{window_index}\x1f#{window_name}\x1f#{window_layout}\x1f#{window_id}"})
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
func (s scopedTmux) ShowWindowOptions(ctx context.Context, target string) (map[string]string, error) {
	out, err := s.Run(ctx, []string{"show-options", "-w", "-t", target})
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	return tmux.ParseWindowOptions(out), nil
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
	if _, err := srv.Tmux("set-option", "-t", "lazytmux:1", "-w", "@branch", "feat/x"); err != nil {
		t.Fatal(err)
	}
	// Empty value: tmux renders it as `''` in show-options. The parser must
	// recover "" here, not the literal two-char string.
	if _, err := srv.Tmux("set-option", "-t", "lazytmux:1", "-w", "@pr_title", ""); err != nil {
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
	saver := snapshot.NewSaver(db, sb, st, snapshot.SaverOptions{
		Host: "test", CaptureScrollback: true,
		WindowOptionPrefixes: []string{"@branch", "@pr_"},
	})
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
	var win1Opts map[string]string
	for _, s := range m.Sessions {
		if s.Name != "lazytmux" {
			continue
		}
		hasLazytmux = true
		for _, w := range s.Windows {
			if w.Index == 1 {
				win1Opts = w.Options
			}
		}
	}
	if !hasLazytmux {
		t.Error("manifest missing lazytmux session")
	}
	if win1Opts["@branch"] != "feat/x" {
		t.Errorf("window @branch option = %q, want feat/x", win1Opts["@branch"])
	}
	prTitle, ok := win1Opts["@pr_title"]
	if !ok {
		t.Error("window @pr_title option missing from manifest")
	}
	if prTitle != "" {
		t.Errorf("empty @pr_title round-tripped as %q, want empty string", prTitle)
	}
}
