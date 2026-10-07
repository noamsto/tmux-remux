//go:build integration

package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/filter"
	"github.com/noamsto/tmux-remux/internal/restore"
	"github.com/noamsto/tmux-remux/internal/scrollback"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/tmux"
	"github.com/noamsto/tmux-remux/internal/triggers"
	"github.com/noamsto/tmux-remux/testutil"
)

// scopedTmux runs tmux against a specific socket. Implements both the Lister
// and CaptureLister interfaces consumed by snapshot.Saver.
type scopedTmux struct {
	socket string
}

func (s scopedTmux) Run(ctx context.Context, args []string) (string, error) {
	full := append([]string{"-f", "/dev/null", "-u", "-S", s.socket}, args...)
	cmd := exec.CommandContext(ctx, "tmux", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
func (s scopedTmux) ListSessions(ctx context.Context) ([]tmux.SessionRow, error) {
	out, err := s.Run(ctx, []string{"list-sessions", "-F", "#{session_name}\x1f#{session_last_attached}\x1f#{@bridge_host}"})
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
	// Decoration-free: this stub exists to test filter/session-scoping
	// behavior, not decoration, and nothing here asserts decoration.
	return tmux.ParseWindows(out)
}
func (s scopedTmux) ListPanes(ctx context.Context) ([]tmux.PaneRow, error) {
	out, err := s.Run(ctx, []string{"list-panes", "-a", "-F", "#{session_name}\x1f#{window_index}\x1f#{pane_index}\x1f#{pane_current_path}\x1f#{pane_current_command}\x1f#{pane_pid}\x1f#{pane_last_used}\x1f#{pane_id}\x1f#{@remux_relaunch}\x1f#{pane_floating_flag}\x1f#{@remux_relaunch_owner}"})
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

	db, err := store.Open(ctx, dbPath, "/tmp/tmux-test/default")
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
	db, err := store.Open(ctx, filepath.Join(dir, "test.db"), "/tmp/tmux-test/default")
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

	plan := restore.BuildPaneRestore(lost, win, "init", win.ID, restore.BuildOptions{DefaultShell: "/bin/sh"})
	if _, err := restore.Apply(ctx, st, plan); err != nil {
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

// findWindow returns the window at index in testutil.StartServer's "init"
// session, if present.
func findWindow(m snapshot.Manifest, index int) (snapshot.Window, bool) {
	for _, s := range m.Sessions {
		if s.Name != "init" {
			continue
		}
		for _, w := range s.Windows {
			if w.Index == index {
				return w, true
			}
		}
	}
	return snapshot.Window{}, false
}

// TestRelaunchOverrideSurvivesOnlyWhileProgramRuns is the rule's real-tmux
// proof: a pane born running "sleep 300; exec /bin/sh" (the shape restore's
// own startup gives a relaunched override) reports its shell's name in
// pane_current_command while sleep runs — a `sh -c` has no job control, so
// sleep stays in the shell's process group — so ChildCount, not the command
// name, is what tells a busy pane from a stale stamp on an idle prompt.
func TestRelaunchOverrideSurvivesOnlyWhileProgramRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := testutil.StartServer(t)
	st := scopedTmux{socket: srv.Socket}
	const stamp = "sleep 300"
	if _, err := srv.Tmux("set", "-g", "default-shell", "/bin/sh"); err != nil {
		t.Fatal(err)
	}

	// (a) restore-style pane: still running its relaunched program.
	startup := restore.BuildStartupCommand(restore.StartupOpts{DefaultShell: "/bin/sh", OverrideCmd: stamp})
	if _, err := srv.Tmux("new-window", "-d", "-t", "init:5", "-n", "busy", startup); err != nil {
		t.Fatal(err)
	}
	// (b) idle prompt carrying the same stamp — left behind by an agent that
	// died before its SessionEnd hook could clear it.
	if _, err := srv.Tmux("new-window", "-d", "-t", "init:6", "-n", "idle"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"init:5", "init:6"} {
		if _, err := srv.Tmux("set", "-p", "-t", target, "@remux_relaunch", stamp); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	var m snapshot.Manifest
	dump := func() string {
		b, _ := json.MarshalIndent(m, "", "  ")
		return string(b)
	}
	// The idle login shell can briefly have a child while its profile runs, so
	// wait for both windows to settle.
	deadline := time.Now().Add(5 * time.Second)
	for {
		built, err := snapshot.Build(ctx, st, "test", 0)
		if err != nil {
			t.Fatalf("snapshot.Build: %v", err)
		}
		m = built
		busyWin, busyOK := findWindow(m, 5)
		idleWin, idleOK := findWindow(m, 6)
		if busyOK && len(busyWin.Panes) == 1 && busyWin.Panes[0].ChildCount >= 1 &&
			idleOK && len(idleWin.Panes) == 1 && idleWin.Panes[0].ChildCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("window 5 (busy) and window 6 (idle) never both settled; last manifest:\n%s", dump())
		}
		time.Sleep(50 * time.Millisecond)
	}

	busy, _ := findWindow(m, 5)
	if p := busy.Panes[0]; !filter.IsShell(p.Command) || p.Relaunch != stamp {
		t.Errorf("window 5 pane = %q with relaunch %q, want a shell name carrying %q; manifest:\n%s", p.Command, p.Relaunch, stamp, dump())
	}
	idle, ok := findWindow(m, 6)
	if !ok || len(idle.Panes) != 1 || idle.Panes[0].ChildCount != 0 {
		t.Fatalf("window 6 should be one childless pane; manifest:\n%s", dump())
	}

	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{DefaultShell: "/bin/sh"})
	want := map[int]bool{5: true, 6: false}
	for _, a := range plan {
		cw, ok := a.(restore.CreateWindow)
		if !ok {
			continue
		}
		keep, tracked := want[cw.Index]
		if !tracked {
			continue
		}
		delete(want, cw.Index)
		if strings.Contains(cw.StartupCommand, stamp) != keep {
			t.Errorf("window %d StartupCommand = %q, want override kept = %v; manifest:\n%s", cw.Index, cw.StartupCommand, keep, dump())
		}
	}
	if len(want) != 0 {
		t.Errorf("plan has no CreateWindow for windows %v; plan = %+v", want, plan)
	}
}

// standinStamp is the @remux_relaunch value the stand-in agent's hook writes.
const standinStamp = "claude --resume standin-1"

// buildStandin compiles testdata/standin-agent into t.TempDir() and returns its
// path. testdata/ is skipped by ./..., so it is built explicitly.
func buildStandin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "standin-agent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/standin-agent")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build standin-agent: %v\n%s", err, out)
	}
	return bin
}

// singleQuote quotes s for a POSIX shell.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// paneOption reads a pane option of target; unset options read as "".
func paneOption(t *testing.T, srv *testutil.Server, target, option string) string {
	t.Helper()
	out, err := srv.Tmux("show-options", "-pqv", "-t", target, option)
	if err != nil {
		t.Fatalf("show-options %s %s: %v\n%s", target, option, err, out)
	}
	return strings.TrimSpace(out)
}

// panePID returns #{pane_pid} of target.
func panePID(t *testing.T, srv *testutil.Server, target string) int {
	t.Helper()
	out, err := srv.Tmux("display-message", "-p", "-t", target, "#{pane_pid}")
	if err != nil {
		t.Fatalf("display-message %s: %v\n%s", target, err, out)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("pane_pid %q: %v", out, err)
	}
	return pid
}

// waitForOwner polls until the stand-in agent's hook has stamped target, then
// returns the pid recorded in @remux_relaunch_owner.
func waitForOwner(t *testing.T, srv *testutil.Server, target string) int {
	t.Helper()
	var stamp, owner string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		stamp = paneOption(t, srv, target, "@remux_relaunch")
		if stamp == standinStamp {
			owner = paneOption(t, srv, target, "@remux_relaunch_owner")
			fields := strings.Fields(owner)
			if len(fields) == 0 {
				t.Fatalf("%s: @remux_relaunch = %q but @remux_relaunch_owner is empty", target, stamp)
			}
			pid, err := strconv.Atoi(fields[0])
			if err != nil {
				t.Fatalf("%s: @remux_relaunch_owner = %q: %v", target, owner, err)
			}
			return pid
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never stamped; @remux_relaunch = %q, @remux_relaunch_owner = %q", target, stamp, owner)
	return 0
}

// waitForPane polls snapshot.Build until the single pane of window index
// satisfies ok, and returns that pane. On timeout it fails with the last
// manifest.
func waitForPane(t *testing.T, st scopedTmux, index int, ok func(snapshot.Pane) bool) snapshot.Pane {
	t.Helper()
	var m snapshot.Manifest
	deadline := time.Now().Add(5 * time.Second)
	for {
		built, err := snapshot.Build(context.Background(), st, "test", 0)
		if err != nil {
			t.Fatalf("snapshot.Build: %v", err)
		}
		m = built
		if w, found := findWindow(m, index); found && len(w.Panes) == 1 && ok(w.Panes[0]) {
			return w.Panes[0]
		}
		if time.Now().After(deadline) {
			dump, _ := json.MarshalIndent(m, "", "  ")
			t.Fatalf("window %d pane never reached the expected state; last manifest:\n%s", index, dump)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestRelaunchStampOwnerAgentIsPane covers an agent that is the pane process:
// the stamp is bound to the pane process, survives capture while the agent
// runs, and is dropped by Build once the agent is gone even though tmux still
// holds the option.
func TestRelaunchStampOwnerAgentIsPane(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	bin := buildRemux(t)
	standin := buildStandin(t)
	srv := testutil.StartServer(t)
	st := scopedTmux{socket: srv.Socket}
	for _, opt := range [][]string{{"default-shell", "/bin/sh"}, {"remain-on-exit", "on"}} {
		if out, err := srv.Tmux("set", "-g", opt[0], opt[1]); err != nil {
			t.Fatalf("set %s: %v\n%s", opt[0], err, out)
		}
	}

	const target = "init:7"
	if out, err := srv.Tmux("new-window", "-d", "-t", target, "-n", "agent", "exec "+singleQuote(standin)+" "+singleQuote(bin)); err != nil {
		t.Fatalf("new-window: %v\n%s", err, out)
	}

	ownerPID := waitForOwner(t, srv, target)
	if panePID := panePID(t, srv, target); ownerPID != panePID {
		t.Fatalf("owner pid = %d, want the pane process %d", ownerPID, panePID)
	}
	waitForPane(t, st, 7, func(p snapshot.Pane) bool { return p.Relaunch == standinStamp })

	if err := syscall.Kill(ownerPID, syscall.SIGTERM); err != nil {
		t.Fatalf("kill agent: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := srv.Tmux("display-message", "-p", "-t", target, "#{pane_dead}")
		if err != nil {
			t.Fatalf("display-message: %v\n%s", err, out)
		}
		if strings.TrimSpace(out) == "1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pane never died after the agent was killed")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if out, err := srv.Tmux("respawn-pane", "-t", target, "exec sleep 300"); err != nil {
		t.Fatalf("respawn-pane: %v\n%s", err, out)
	}
	// tmux keeps the option across respawn, so a dropped Relaunch below is Build's doing.
	if got := paneOption(t, srv, target, "@remux_relaunch"); got != standinStamp {
		t.Fatalf("@remux_relaunch after respawn = %q, want %q still set", got, standinStamp)
	}
	if p := waitForPane(t, st, 7, func(p snapshot.Pane) bool { return p.Command == "sleep" }); p.Relaunch != "" {
		t.Errorf("Relaunch = %q for a pane whose agent is gone, want it dropped", p.Relaunch)
	}
}

// TestRelaunchStampOwnerUnderShell covers an agent launched from an
// interactive shell prompt, directly or from a nested shell: the owner is the
// agent, not a shell above it, and the stamp is dropped once the agent exits
// and another program runs.
func TestRelaunchStampOwnerUnderShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	bin := buildRemux(t)
	standin := buildStandin(t)
	for _, tc := range []struct {
		name   string
		nested bool
	}{
		{name: "prompt"},
		{name: "nested shell", nested: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.StartServer(t)
			st := scopedTmux{socket: srv.Socket}
			if out, err := srv.Tmux("set", "-g", "default-shell", "/bin/sh"); err != nil {
				t.Fatalf("set default-shell: %v\n%s", err, out)
			}

			const target = "init:8"
			if out, err := srv.Tmux("new-window", "-d", "-t", target, "-n", "shell"); err != nil {
				t.Fatalf("new-window: %v\n%s", err, out)
			}
			// The shell can briefly have a child while its profile runs; wait for the prompt.
			waitForPane(t, st, 8, func(p snapshot.Pane) bool { return p.ChildCount == 0 })
			if tc.nested {
				if out, err := srv.Tmux("send-keys", "-t", target, "/bin/sh", "Enter"); err != nil {
					t.Fatalf("send-keys: %v\n%s", err, out)
				}
				waitForPane(t, st, 8, func(p snapshot.Pane) bool { return p.ChildCount >= 1 })
			}
			if out, err := srv.Tmux("send-keys", "-t", target, singleQuote(standin)+" "+singleQuote(bin), "Enter"); err != nil {
				t.Fatalf("send-keys: %v\n%s", err, out)
			}

			ownerPID := waitForOwner(t, srv, target)
			pane := panePID(t, srv, target)
			if ownerPID == pane {
				t.Fatalf("owner pid = %d is the pane's shell, want the agent beneath it", ownerPID)
			}
			if tc.nested {
				owner, err := snapshot.ProcInfo(ownerPID)
				if err != nil {
					t.Fatalf("ProcInfo(%d): %v", ownerPID, err)
				}
				if owner.PPID == pane {
					t.Fatalf("owner pid = %d is the nested shell, want the agent beneath it", ownerPID)
				}
			}
			waitForPane(t, st, 8, func(p snapshot.Pane) bool { return p.Relaunch == standinStamp })

			if err := syscall.Kill(ownerPID, syscall.SIGTERM); err != nil {
				t.Fatalf("kill agent: %v", err)
			}
			if out, err := srv.Tmux("send-keys", "-t", target, "sleep 300", "Enter"); err != nil {
				t.Fatalf("send-keys: %v\n%s", err, out)
			}
			// A pane with a live child keeps owner-less stamps, so the drop here
			// comes from the owner check, not the idle-shell rule.
			waitForPane(t, st, 8, func(p snapshot.Pane) bool { return p.ChildCount >= 1 && p.Relaunch == "" })
		})
	}
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
	if _, err := restore.Apply(ctx, dstClient, plan); err != nil {
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

// TestBorderDecorationRestoreRoundtrip captures window- and pane-scoped
// border/style options (set locally, not globally) from a real tmux server
// into a manifest, replays the resulting restore plan against a fresh
// server, and confirms: (a) the locally-set values round-trip byte-exact,
// including spaces, #[...] escapes, and commas; (b) a second window with no
// locally-set border options gets nothing pinned on it by restore — it keeps
// inheriting the global/theme default.
func TestBorderDecorationRestoreRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const spaceValue = " #[bold]#{@crew_name}#[nobold] "
	const commaValue = "bg=#{@thm_bg},fg=colour99,bold"

	src := testutil.StartServer(t)
	if _, err := src.Tmux("rename-session", "-t", "init", "deco"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-window-option", "-t", "deco:0", "pane-border-style", commaValue); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-window-option", "-t", "deco:0", "pane-active-border-style", spaceValue); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-window-option", "-t", "deco:0", "pane-border-format", spaceValue); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-option", "-p", "-t", "deco:0.0", "@crew_role", "scout"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-option", "-p", "-t", "deco:0.0", "pane-border-style", spaceValue); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Tmux("set-option", "-p", "-t", "deco:0.0", "pane-active-border-style", commaValue); err != nil {
		t.Fatal(err)
	}
	// Second window: no locally-set border/decoration options at all.
	if _, err := src.Tmux("new-window", "-t", "deco", "-n", "plain"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TMUX", src.Socket+",0,0")
	windowOpts := []string{
		"@crew_name", "@crew_color",
		"pane-border-style", "pane-active-border-style",
		"pane-border-format", "pane-border-status",
	}
	paneOpts := []string{
		"@crew_role", "@crew_role_color", "@crew_state",
		"pane-border-style", "pane-active-border-style",
		"pane-border-format",
	}
	srcClient := tmux.NewClient("tmux", windowOpts...).SetPaneDecorationOptions(paneOpts)

	ctx := context.Background()
	m, err := snapshot.Build(ctx, srcClient, "test", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}

	var sess *snapshot.Session
	for i := range m.Sessions {
		if m.Sessions[i].Name == "deco" {
			sess = &m.Sessions[i]
			break
		}
	}
	if sess == nil {
		t.Fatal("deco session missing from manifest")
	}
	var win0, win1 *snapshot.Window
	for i := range sess.Windows {
		switch sess.Windows[i].Index {
		case 0:
			win0 = &sess.Windows[i]
		case 1:
			win1 = &sess.Windows[i]
		}
	}
	if win0 == nil || win1 == nil {
		t.Fatalf("deco session missing windows: win0=%v win1=%v", win0, win1)
	}

	wantWin0 := map[string]string{
		"pane-border-style":        commaValue,
		"pane-active-border-style": spaceValue,
		"pane-border-format":       spaceValue,
	}
	if !reflect.DeepEqual(win0.Decoration, wantWin0) {
		t.Errorf("captured window 0 Decoration = %#v, want %#v", win0.Decoration, wantWin0)
	}
	if win1.Decoration != nil {
		t.Errorf("captured window 1 Decoration = %#v, want nil", win1.Decoration)
	}
	if len(win0.Panes) == 0 {
		t.Fatal("window 0 has no panes in manifest")
	}
	wantPane0 := map[string]string{
		"@crew_role":               "scout",
		"pane-border-style":        spaceValue,
		"pane-active-border-style": commaValue,
	}
	if !reflect.DeepEqual(win0.Panes[0].Decoration, wantPane0) {
		t.Errorf("captured pane 0 Decoration = %#v, want %#v", win0.Panes[0].Decoration, wantPane0)
	}
	if len(win1.Panes) == 0 {
		t.Fatal("window 1 has no panes in manifest")
	}
	if win1.Panes[0].Decoration != nil {
		t.Errorf("captured window 1 pane Decoration = %#v, want nil", win1.Panes[0].Decoration)
	}

	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{})

	dst := testutil.StartServer(t)
	t.Setenv("TMUX", dst.Socket+",0,0")
	dstClient := tmux.NewClient("tmux")
	if _, err := restore.Apply(ctx, dstClient, plan); err != nil {
		t.Fatalf("apply: %v", err)
	}

	showLocal := func(t *testing.T, pane bool, target, name string) string {
		t.Helper()
		scope := "-w"
		if pane {
			scope = "-p"
		}
		out, err := dst.Tmux("show-options", "-qv", scope, "-t", target, name)
		if err != nil {
			t.Fatalf("show-options %s %s %s: %v", scope, target, name, err)
		}
		return strings.TrimSuffix(out, "\n")
	}

	for name, want := range wantWin0 {
		if got := showLocal(t, false, "deco:0", name); got != want {
			t.Errorf("restored window 0 %s = %q, want %q", name, got, want)
		}
	}
	for name, want := range wantPane0 {
		if got := showLocal(t, true, "deco:0.0", name); got != want {
			t.Errorf("restored pane 0 %s = %q, want %q", name, got, want)
		}
	}

	for name := range wantWin0 {
		if got := showLocal(t, false, "deco:1", name); got != "" {
			t.Errorf("restored window 1 %s = %q, want empty (not locally set)", name, got)
		}
	}
	for name := range wantPane0 {
		if got := showLocal(t, true, "deco:1.0", name); got != "" {
			t.Errorf("restored window 1 pane 0 %s = %q, want empty (not locally set)", name, got)
		}
	}
}

// TestRestoreFirstWindowAtBaseIndex restores a session's first window against a
// server with base-index 1, where the window tmux hands every new session lands
// on the very index the restored window wants. Creating session and window
// separately loses the window's startup command to "index in use".
func TestRestoreFirstWindowAtBaseIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dst := testutil.StartServer(t)
	if _, err := dst.Tmux("set-option", "-g", "base-index", "1"); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "ran")
	plan := []restore.Action{
		restore.CreateWindow{
			Session:        "s1",
			Index:          1,
			Name:           "dispatcher",
			Cwd:            "/tmp",
			StartupCommand: "touch " + marker + "; exec /bin/sh",
			NewSession:     true,
		},
	}

	t.Setenv("TMUX", dst.Socket+",0,0")
	failed, err := restore.Apply(context.Background(), tmux.NewClient("tmux"), plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("apply reported failed actions: %v", failed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		out, _ := dst.Tmux("list-windows", "-t", "s1", "-F", "#{window_index} #{window_name}")
		t.Fatalf("restored window never ran its startup command; s1 windows:\n%s", strings.TrimSpace(out))
	}
}

// buildRemux compiles the CLI into t.TempDir() and returns its path. Hooks need
// a real binary; the other integration tests call packages directly.
func buildRemux(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tmux-remux")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/tmux-remux")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// wireTriggers renders the fragment for the running tmux and sources it into
// srv. Storage lands under XDG_DATA_HOME, which the caller must have pointed at
// a temp dir before the server started.
func wireTriggers(t *testing.T, srv *testutil.Server, bin string) tmux.Version {
	t.Helper()
	v, err := tmux.NewClient("tmux").Version(context.Background())
	if err != nil {
		t.Fatalf("detect tmux version: %v", err)
	}
	frag := triggers.Render(triggers.Params{Bin: bin, Version: v, AutoRestore: false})
	path := filepath.Join(t.TempDir(), "triggers.conf")
	if err := os.WriteFile(path, []byte(frag), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := srv.Tmux("source-file", path); err != nil {
		t.Fatalf("source-file: %v\n%s", err, out)
	}
	return v
}

// waitForEvent polls the store for up to 5s for an event of kind whose manifest
// satisfies match, and returns it.
func waitForEvent(t *testing.T, dbPath, socket, kind string, match func(closeevent.CloseManifest) bool) closeevent.CloseManifest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var seen []string
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		db, err := store.Open(context.Background(), dbPath, socket)
		if err != nil {
			continue // hook may not have created the DB yet
		}
		evs, err := db.ListEvents(context.Background(), store.ListOpts{Kinds: []string{kind}, Limit: 20})
		_ = db.Close()
		if err != nil {
			continue
		}
		seen = seen[:0]
		for _, ev := range evs {
			var m closeevent.CloseManifest
			if json.Unmarshal([]byte(ev.ManifestJSON), &m) != nil {
				continue
			}
			seen = append(seen, ev.ManifestJSON)
			if match(m) {
				return m
			}
		}
	}
	t.Fatalf("no %s event matched within 5s; saw: %v", kind, seen)
	return closeevent.CloseManifest{}
}

// remuxEnv points storage at a temp dir and returns the resulting DB path. Must
// run before the tmux server starts so the server (and its hook jobs) inherit it.
func remuxEnv(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_RUNTIME_DIR", dataHome)
	return filepath.Join(dataHome, "tmux-remux", "state.db")
}

func TestTriggersCloseEventsCarrySession(t *testing.T) {
	dbPath := remuxEnv(t)
	bin := buildRemux(t)
	srv := testutil.StartServer(t)
	wireTriggers(t, srv, bin)

	// A snapshot has to exist before the close, or capture-event has nothing to
	// diff against.
	if out, err := srv.Tmux("new-session", "-d", "-s", "work", "/bin/sh"); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("run-shell", bin+" save --reason=test"); err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}

	// A pane whose program exits is what fires pane-exited.
	if out, err := srv.Tmux("split-window", "-d", "-t", "work", "sh", "-c", "exit 0"); err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}

	m := waitForEvent(t, dbPath, srv.Socket, "pane-died", func(m closeevent.CloseManifest) bool {
		return m.PaneID != ""
	})
	if m.SessionID == "" {
		t.Error("pane-died event has no session id — the hook passed --session empty")
	}
	if m.SessionName != "work" {
		t.Errorf("pane-died SessionName = %q, want \"work\"", m.SessionName)
	}
}

func TestTriggersWindowCloseCarriesSession(t *testing.T) {
	dbPath := remuxEnv(t)
	bin := buildRemux(t)
	srv := testutil.StartServer(t)
	wireTriggers(t, srv, bin)

	if out, err := srv.Tmux("new-session", "-d", "-s", "work", "/bin/sh"); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("new-window", "-d", "-t", "work", "-n", "doomed", "/bin/sh"); err != nil {
		t.Fatalf("new-window: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("run-shell", bin+" save --reason=test"); err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("kill-window", "-t", "work:doomed"); err != nil {
		t.Fatalf("kill-window: %v\n%s", err, out)
	}

	m := waitForEvent(t, dbPath, srv.Socket, "window-unlinked", func(m closeevent.CloseManifest) bool {
		return m.WindowID != ""
	})
	if m.SessionID == "" {
		t.Error("window-unlinked event has no session id")
	}
	if m.SessionName != "work" {
		t.Errorf("window-unlinked SessionName = %q, want \"work\"", m.SessionName)
	}
}

func TestUndoDropsFloatingPane(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbPath := remuxEnv(t)
	bin := buildRemux(t)
	srv := testutil.StartServer(t)
	t.Setenv("TMUX", srv.Socket+",0,0")

	if out, err := srv.Tmux("new-session", "-d", "-s", "work", "/bin/sh"); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("new-window", "-d", "-t", "work", "-n", "doomed", "/bin/sh"); err != nil {
		t.Fatalf("new-window: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("split-window", "-d", "-h", "-t", "work:doomed", "/bin/sh"); err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("new-pane", "-d", "-t", "work:doomed", "/bin/sh"); err != nil {
		t.Skipf("tmux server does not support new-pane: %v\n%s", err, out)
	}

	wantGeometry := tiledPaneGeometry(t, srv, "work:doomed")
	if len(wantGeometry) != 2 {
		t.Fatalf("tiled panes before close = %v, want 2", wantGeometry)
	}
	identity, err := srv.Tmux("display-message", "-p", "-t", "work:doomed", "#{session_id}\x1f#{window_id}")
	if err != nil {
		t.Fatalf("display target identity: %v\n%s", err, identity)
	}
	fields := strings.Split(strings.TrimSpace(identity), "\x1f")
	if len(fields) != 2 {
		t.Fatalf("target identity = %q, want session and window id", identity)
	}
	if out, err := exec.Command(bin, "save", "--reason=test").CombinedOutput(); err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	// Capture events resolve against snapshots strictly before their timestamp.
	time.Sleep(time.Millisecond)
	if out, err := srv.Tmux("kill-window", "-t", "work:doomed"); err != nil {
		t.Fatalf("kill-window: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "capture-event", "window-unlinked", "--session", fields[0], "--session-name", "work", "--window", fields[1]).CombinedOutput(); err != nil {
		t.Fatalf("capture-event: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "undo", "--pop", "--session", "work").CombinedOutput(); err != nil {
		t.Fatalf("undo --pop: %v\n%s", err, out)
	}
	if got := tiledPaneGeometry(t, srv, "work:doomed"); !reflect.DeepEqual(got, wantGeometry) {
		t.Errorf("restored tiled pane geometry = %v, want %v", got, wantGeometry)
	}
	assertOnlyTiledPanes(t, srv, "work:doomed", 2)
	db, err := store.Open(context.Background(), dbPath, srv.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	events, err := db.ListEvents(context.Background(), store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("close events after undo = %v, want restored event popped", events)
	}
}

func assertOnlyTiledPanes(t *testing.T, srv *testutil.Server, target string, want int) {
	t.Helper()
	out, err := srv.Tmux("list-panes", "-t", target, "-F", "#{pane_floating_flag}")
	if err != nil {
		t.Fatalf("list panes: %v\n%s", err, out)
	}
	flags := strings.Fields(out)
	if len(flags) != want {
		t.Fatalf("restored panes = %v, want exactly %d", flags, want)
	}
	for _, flag := range flags {
		if flag != "0" {
			t.Errorf("restored pane floating flag = %q, want all panes tiled", flag)
		}
	}
}

func tiledPaneGeometry(t *testing.T, srv *testutil.Server, target string) []string {
	t.Helper()
	out, err := srv.Tmux("list-panes", "-t", target, "-F", "#{pane_floating_flag}\x1f#{pane_width}x#{pane_height},#{pane_left},#{pane_top}")
	if err != nil {
		t.Fatalf("list panes: %v\n%s", err, out)
	}
	var geometry []string
	for _, line := range strings.Fields(out) {
		floating, pane, ok := strings.Cut(line, "\x1f")
		if !ok {
			t.Fatalf("pane geometry row = %q", line)
		}
		if floating == "0" {
			geometry = append(geometry, pane)
		}
	}
	slices.Sort(geometry)
	return geometry
}

// prefix+x runs `kill-pane`, and no tmux release gives that command hook the
// pane it killed, so closeevent.resolveKilledPane recovers the id by diffing
// survivors against the last snapshot.
func TestTriggersKillPaneResolvesViaSurvivorDiff(t *testing.T) {
	dbPath := remuxEnv(t)
	bin := buildRemux(t)
	srv := testutil.StartServer(t)
	wireTriggers(t, srv, bin)

	if out, err := srv.Tmux("new-session", "-d", "-s", "work", "/bin/sh"); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("split-window", "-d", "-t", "work", "/bin/sh"); err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}
	panes, err := srv.Tmux("list-panes", "-t", "work", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("list-panes: %v\n%s", err, panes)
	}
	ids := strings.Fields(panes)
	if len(ids) != 2 {
		t.Fatalf("want 2 panes, got %v", ids)
	}
	victim := ids[1]

	if out, err := srv.Tmux("run-shell", bin+" save --reason=test"); err != nil {
		t.Fatalf("save: %v\n%s", err, out)
	}
	// -t names the victim, but the hook is after-kill-pane, which sees no pane
	// id regardless — exactly the prefix+x situation.
	if out, err := srv.Tmux("kill-pane", "-t", victim); err != nil {
		t.Fatalf("kill-pane: %v\n%s", err, out)
	}

	m := waitForEvent(t, dbPath, srv.Socket, "pane-died", func(m closeevent.CloseManifest) bool {
		return m.PaneID == victim
	})
	if m.WindowID == "" {
		t.Error("survivor diff resolved the pane but not its window")
	}
	// The event's own embedding must also survive a save landing after the kill.
	if m.Resolved == nil || m.Resolved.Item.Pane == nil || m.Resolved.Item.Pane.ID != victim {
		t.Errorf("embedded entity = %+v, want the killed pane %s", m.Resolved, victim)
	}
}

// The monitor hook watches #{T:@remux_save_tick}, whose format string lives in
// an option precisely so a test can drive it at second granularity instead of
// waiting a minute.
func TestTriggersMonitorSaveTick(t *testing.T) {
	dbPath := remuxEnv(t)
	bin := buildRemux(t)
	srv := testutil.StartServer(t)
	if v := wireTriggers(t, srv, bin); !v.AtLeast(3, 8) {
		t.Skipf("monitor hooks need tmux 3.8, have %s", v)
	}

	if out, err := srv.Tmux("set", "-g", "@remux_save_tick", "%S"); err != nil {
		t.Fatalf("set @remux_save_tick: %v\n%s", err, out)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		db, err := store.Open(context.Background(), dbPath, srv.Socket)
		if err != nil {
			continue
		}
		snaps, err := db.ListEvents(context.Background(), store.ListOpts{
			Kinds: []string{"snapshot"},
			Limit: 20,
		})
		_ = db.Close()
		if err != nil {
			continue
		}
		for _, ev := range snaps {
			if ev.Reason == "timer" {
				return
			}
		}
	}
	t.Fatal("no snapshot with reason=timer within 10s — the monitor hook did not fire")
}

// A close resolves against whichever snapshot is newest before it, and a save
// that lands inside min_save_interval writes one with no scrollback at all —
// so on a busy server the pane's captured output sits a save or two back,
// present in the store but invisible to the close. The event must adopt it.
func TestTriggersCloseAdoptsScrollbackFromAnEarlierSave(t *testing.T) {
	dbPath := remuxEnv(t)
	bin := buildRemux(t)
	srv := testutil.StartServer(t)
	if v := wireTriggers(t, srv, bin); !v.AtLeast(3, 8) {
		t.Skipf("needs the tmux 3.8 pane-exited hook, have %s", v)
	}

	if out, err := srv.Tmux("new-session", "-d", "-s", "work", "/bin/sh"); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	if out, err := srv.Tmux("split-window", "-d", "-t", "work", "/bin/sh"); err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}
	panes, err := srv.Tmux("list-panes", "-t", "work", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("list-panes: %v\n%s", err, panes)
	}
	ids := strings.Fields(panes)
	if len(ids) != 2 {
		t.Fatalf("want 2 panes, got %v", ids)
	}
	victim := ids[1]

	// The session's own first save is the one unthrottled save this store can
	// have — nothing precedes it — so it is what captured the victim. Which
	// screen it holds is not the test's business and is not deterministic
	// anyway: the hooks are backgrounded, so a later `send-keys` may or may not
	// have painted by the time that save reads the pane.
	waitForCapturedScrollback(t, dbPath, srv.Socket)

	// A structural change inside min_save_interval now records structure with
	// no scrollback at all, and becomes the newest snapshot before the close —
	// the one the close resolves against, and the reason it can see no
	// scrollback without looking further back.
	if out, err := srv.Tmux("split-window", "-d", "-t", "work", "/bin/sh"); err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}
	waitForThrottledSnapshot(t, dbPath, srv.Socket)

	// The victim's own program exits, so pane-exited fires and carries its
	// pane id. Killing the pane instead lands on after-kill-pane, which carries
	// no id and has to recover it by diffing the survivors against the newest
	// snapshot — a diff that resolves nothing when a save lands between the kill
	// and the hook, as it does on a loaded runner. That race is
	// TestTriggersKillPaneResolvesViaSurvivorDiff's subject, not this test's.
	if out, err := srv.Tmux("send-keys", "-t", victim, "exit", "Enter"); err != nil {
		t.Fatalf("send-keys exit: %v\n%s", err, out)
	}

	m := waitForEvent(t, dbPath, srv.Socket, "pane-died", func(m closeevent.CloseManifest) bool {
		return m.PaneID == victim && m.Resolved != nil && m.Resolved.Item.Pane != nil &&
			m.Resolved.Item.Pane.ScrollbackSHA != ""
	})

	// The SHA can only have come from a snapshot older than the one the close
	// resolved against, since that one carries none — and the blob it names
	// has to actually be in the store, which is what the capture-time link
	// exists to guarantee.
	sha := m.Resolved.Item.Pane.ScrollbackSHA
	sb := scrollback.New(filepath.Join(filepath.Dir(dbPath), "scrollbacks"))
	rc, err := sb.Stream(context.Background(), sha)
	if err != nil {
		t.Fatalf("adopted scrollback %s is not in the store: %v", sha, err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
}

// waitForCapturedScrollback polls for up to 5s for a snapshot that captured
// pane scrollback — the save a later throttled one hides from a close.
func waitForCapturedScrollback(t *testing.T, dbPath, socket string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		db, err := store.Open(context.Background(), dbPath, socket)
		if err != nil {
			continue
		}
		evs, err := db.ListEvents(context.Background(), store.ListOpts{Kinds: []string{"snapshot"}, Limit: 20})
		_ = db.Close()
		if err != nil {
			continue
		}
		for _, ev := range evs {
			var m snapshot.Manifest
			if json.Unmarshal([]byte(ev.ManifestJSON), &m) != nil {
				continue
			}
			for _, sess := range m.Sessions {
				for _, w := range sess.Windows {
					for _, p := range w.Panes {
						if p.ScrollbackSHA != "" {
							return
						}
					}
				}
			}
		}
	}
	t.Fatal("no snapshot captured any pane scrollback within 5s")
}

// waitForThrottledSnapshot polls for up to 5s for a snapshot that recorded
// structure but skipped scrollback — the state that hid a pane's output from
// the close that followed it.
func waitForThrottledSnapshot(t *testing.T, dbPath, socket string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		db, err := store.Open(context.Background(), dbPath, socket)
		if err != nil {
			continue
		}
		evs, err := db.ListEvents(context.Background(), store.ListOpts{Kinds: []string{"snapshot"}, Limit: 1})
		_ = db.Close()
		if err != nil || len(evs) == 0 {
			continue
		}
		var m snapshot.Manifest
		if json.Unmarshal([]byte(evs[0].ManifestJSON), &m) != nil {
			continue
		}
		if m.ScrollbackSkipped {
			return
		}
	}
	t.Fatal("no scrollback-skipped snapshot within 5s")
}

// TestRestoreStartupCommandsSeeFinalPaneSize restores a two-pane window saved
// on a larger screen. select-layout resizes the window to the saved size, so a
// startup command launched with the panes would observe sizes that are wrong
// by the time the window settles; each must instead read the final size on its
// first look. The respawn that launches them must not look like a pane closing.
func TestRestoreStartupCommandsSeeFinalPaneSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dst := testutil.StartServer(t)
	if _, err := dst.Tmux("new-session", "-d", "-s", "big", "-x", "154", "-y", "40", "/bin/sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Tmux("split-window", "-t", "big:0", "-h", "/bin/sh"); err != nil {
		t.Fatal(err)
	}
	layout, err := dst.Tmux("display-message", "-p", "-t", "big:0", "#{window_layout}")
	if err != nil {
		t.Fatal(err)
	}
	layout = strings.TrimSpace(layout)

	hookLog := filepath.Join(t.TempDir(), "hooks")
	for _, hook := range []string{"pane-died", "after-kill-pane"} {
		if _, err := dst.Tmux("set-hook", "-g", hook, "run-shell 'echo "+hook+" >> "+hookLog+"'"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := dst.Tmux("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	sizeFile := func(n int) string { return filepath.Join(dir, fmt.Sprintf("size%d", n)) }
	startup := func(n int) string {
		return fmt.Sprintf("stty size > %s; exec sleep 30", sizeFile(n))
	}
	plan := []restore.Action{
		restore.CreateWindow{Session: "r", Index: 0, Name: "w", Cwd: "/tmp", StartupCommand: startup(0), NewSession: true},
		restore.SplitPane{Target: "r:0", Cwd: "/tmp", StartupCommand: startup(1)},
		restore.SetLayout{Window: "r:0", Layout: layout},
	}
	t.Setenv("TMUX", dst.Socket+",0,0")
	failed, err := restore.Apply(context.Background(), tmux.NewClient("tmux"), plan)
	if err != nil || len(failed) != 0 {
		t.Fatalf("apply: err=%v failed=%v", err, failed)
	}

	out, err := dst.Tmux("list-panes", "-t", "r:0", "-F", "#{pane_height} #{pane_width}")
	if err != nil {
		t.Fatal(err)
	}
	wantSizes := strings.Fields(strings.ReplaceAll(strings.TrimSpace(out), "\n", " "))
	if len(wantSizes) != 4 {
		t.Fatalf("list-panes = %q", out)
	}
	winSize, err := dst.Tmux("display-message", "-p", "-t", "r:0", "#{window_width}x#{window_height}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(winSize) == "154x40" {
		t.Fatalf("window kept the saved 154x40 size; the restore must fit it to the current size")
	}

	for n := 0; n < 2; n++ {
		var got string
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(sizeFile(n)); err == nil && len(b) > 0 {
				got = strings.TrimSpace(string(b))
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		want := wantSizes[2*n] + " " + wantSizes[2*n+1]
		if got != want {
			t.Errorf("pane %d startup saw size %q, want final %q", n, got, want)
		}
	}

	if b, err := os.ReadFile(hookLog); err == nil {
		t.Errorf("respawn fired close hooks: %q", b)
	}
	if v, _ := dst.Tmux("show-options", "-wv", "-t", "r:0", "window-size"); strings.TrimSpace(v) != "" {
		t.Errorf("window-size left pinned locally: %q", v)
	}
}
