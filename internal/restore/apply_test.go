package restore_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-remux/internal/restore"
)

const newSessionFormat = "#{window_id} #{window_index} #{pane_id}"

type recordingTmux struct {
	calls [][]string
	// windowOut stands in for what `new-session -P -F` prints: the window id
	// and the index tmux actually placed it at, plus its first pane. Defaults to
	// "@1 1 %1".
	windowOut     string
	newSessionErr error
	// failFlag makes Run return failErr for any call containing that argument,
	// which is how the -b fallback path is exercised.
	failFlag string
	failErr  error
	nextPane int
}

func (r *recordingTmux) Run(_ context.Context, args []string) (string, error) {
	r.calls = append(r.calls, args)
	if r.failFlag != "" && slices.Contains(args, r.failFlag) {
		return "", r.failErr
	}
	switch args[0] {
	case "new-session":
		if r.newSessionErr != nil {
			return "", r.newSessionErr
		}
		if r.windowOut != "" {
			return r.windowOut, nil
		}
		return "@1 1 %1", nil
	case "new-window", "split-window":
		r.nextPane++
		return fmt.Sprintf("%%%d", 10+r.nextPane), nil
	case "display-message":
		return "138 39", nil
	}
	return "", nil
}

func TestApplyEmitsTmuxCallsWithoutStartup(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", NewSession: true},
		restore.SplitPane{Target: "s1:1", Cwd: "/b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("unexpected action failures: %v", failed)
	}
	want := [][]string{
		{"new-session", "-d", "-s", "s1", "-n", "main", "-c", "/a", "-P", "-F", newSessionFormat},
		{"split-window", "-t", "s1:1", "-c", "/b", "-P", "-F", "#{pane_id}"},
		{"display-message", "-p", "-t", "s1:1", "#{window_width} #{window_height}"},
		{"select-layout", "-t", "s1:1", "L"},
		{"show-options", "-wv", "-t", "s1:1", "window-size"},
		{"resize-window", "-t", "s1:1", "-x", "138", "-y", "39"},
		{"set-window-option", "-u", "-t", "s1:1", "window-size"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyCreatesSessionAndFirstWindowInOneCall(t *testing.T) {
	rt := &recordingTmux{}
	startup := "nvim; exec /bin/zsh"
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", StartupCommand: startup, NewSession: true},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-session", "-d", "-s", "s1", "-n", "main", "-c", "/a", "-P", "-F", newSessionFormat},
		{"clear-history", "-t", "%1"},
		{"respawn-pane", "-k", "-t", "%1", startup},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyMovesFirstWindowToItsRecordedIndex(t *testing.T) {
	rt := &recordingTmux{windowOut: "@7 1 %7"}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 3, Name: "main", Cwd: "/a", NewSession: true},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-session", "-d", "-s", "s1", "-n", "main", "-c", "/a", "-P", "-F", newSessionFormat},
		{"move-window", "-s", "@7", "-t", "s1:3"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

// Restoring a single window into a session that outlived it (the undo path)
// plans a NewSession window even though the session is already there.
func TestApplyFallsBackToNewWindowWhenSessionExists(t *testing.T) {
	rt := &recordingTmux{newSessionErr: errors.New("tmux new-session: exit status 1 (stderr: duplicate session: s1)")}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 2, Name: "main", Cwd: "/a", NewSession: true},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("unexpected action failures: %v", failed)
	}
	want := [][]string{
		{"new-session", "-d", "-s", "s1", "-n", "main", "-c", "/a", "-P", "-F", newSessionFormat},
		{"new-window", "-t", "s1:2", "-n", "main", "-c", "/a", "-P", "-F", "#{pane_id}"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyRespawnsStartupCommandsOnlyAfterLayoutAndFit(t *testing.T) {
	rt := &recordingTmux{}
	startup := `'/usr/bin/tmux-remux' cat-scrollback abc; exec /bin/zsh`
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", StartupCommand: startup},
		restore.SplitPane{Target: "s1:1", Cwd: "/b", StartupCommand: "htop"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a", "-P", "-F", "#{pane_id}"},
		{"split-window", "-t", "s1:1", "-c", "/b", "-P", "-F", "#{pane_id}"},
		{"display-message", "-p", "-t", "s1:1", "#{window_width} #{window_height}"},
		{"select-layout", "-t", "s1:1", "L"},
		{"show-options", "-wv", "-t", "s1:1", "window-size"},
		{"resize-window", "-t", "s1:1", "-x", "138", "-y", "39"},
		{"set-window-option", "-u", "-t", "s1:1", "window-size"},
		{"clear-history", "-t", "%11"},
		{"respawn-pane", "-k", "-t", "%11", startup},
		{"clear-history", "-t", "%12"},
		{"respawn-pane", "-k", "-t", "%12", "htop"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyReenablesAutomaticRename(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", AutomaticRename: true},
		restore.CreateWindow{Session: "s1", Index: 2, Name: "named", Cwd: "/a"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a", "-P", "-F", "#{pane_id}"},
		{"set-window-option", "-t", "s1:1", "automatic-rename", "on"},
		{"new-window", "-t", "s1:2", "-n", "named", "-c", "/a", "-P", "-F", "#{pane_id}"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyContinuesPastIndividualFailures(t *testing.T) {
	calls := 0
	failOn := 1
	rt := failingTmux{
		runFn: func(_ []string) (string, error) {
			calls++
			if calls == failOn+1 {
				return "", context.Canceled
			}
			return "", nil
		},
	}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Cwd: "/a"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatalf("Apply should swallow per-action errors, got %v", err)
	}
	if calls < 3 {
		t.Errorf("expected the layout to still be attempted after the failure, got %d calls", calls)
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 reported failure, got %d: %v", len(failed), failed)
	}
	if _, ok := failed[0].Action.(restore.SplitPane); !ok {
		t.Errorf("failed action = %T, want restore.SplitPane", failed[0].Action)
	}
}

func TestApplySetOptionRunsSetWindowOption(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.SetOption{Target: "s:1", Name: "@crew_color", Value: "colour141"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"set-window-option", "-q", "-t", "s:1", "@crew_color", "colour141"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplySetOptionPaneRunsSetOption(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.SetOption{Target: "s:1", Pane: true, Name: "@crew_color", Value: "colour141"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"set-option", "-pq", "-t", "s:1", "@crew_color", "colour141"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

type failingTmux struct {
	runFn func(args []string) (string, error)
}

func (f failingTmux) Run(_ context.Context, args []string) (string, error) {
	return f.runFn(args)
}

func TestApplyInsertsWindowAtOriginalIndex(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 3, Name: "docs", Cwd: "/a", InsertBefore: true},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-window", "-b", "-t", "s1:3", "-n", "docs", "-c", "/a", "-P", "-F", "#{pane_id}"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyOmitsInsertBeforeByDefault(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 3, Name: "docs", Cwd: "/a"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-window", "-t", "s1:3", "-n", "docs", "-c", "/a", "-P", "-F", "#{pane_id}"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

// Old tmux has no -b on new-window. A usage error must degrade to the plain
// call rather than losing the window.
func TestApplyRetriesWithoutInsertBeforeOnUsageError(t *testing.T) {
	rt := &recordingTmux{failFlag: "-b", failErr: errors.New("usage: new-window [-adkP]")}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 3, Name: "docs", Cwd: "/a", InsertBefore: true},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("unexpected action failures: %v", failed)
	}
	want := [][]string{
		{"new-window", "-b", "-t", "s1:3", "-n", "docs", "-c", "/a", "-P", "-F", "#{pane_id}"},
		{"new-window", "-t", "s1:3", "-n", "docs", "-c", "/a", "-P", "-F", "#{pane_id}"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

// A real refusal (not a usage error) must surface, not silently retry.
func TestApplyDoesNotRetryOnRealFailure(t *testing.T) {
	rt := &recordingTmux{failFlag: "-b", failErr: errors.New("create window failed: index 3 in use")}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 3, Name: "docs", Cwd: "/a", InsertBefore: true},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %v, want exactly one action failure", failed)
	}
	if len(rt.calls) != 1 {
		t.Errorf("made %d calls, want 1 (no retry)", len(rt.calls))
	}
}

// When a CreateWindow's index collides with a live window (e.g. renumber-windows
// moved a survivor into the vacated slot), the plan's own SplitPane/SetLayout
// for that same index must not run — they would otherwise land on that
// unrelated live window instead of the one that failed to get created.
func TestApplySkipsActionsTargetingAFailedCreateWindow(t *testing.T) {
	rt := &recordingTmux{failFlag: "new-window", failErr: errors.New("create window failed: index 1 in use")}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "docs", Cwd: "/a"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %v, want exactly one action failure (the CreateWindow)", failed)
	}
	if len(rt.calls) != 1 {
		t.Errorf("calls = %v, want only the failed new-window call", rt.calls)
	}
}

// A pane restored into a live window the user has pinned to a window-size must
// get that setting back after the fit.
func TestApplyRestoresPinnedWindowSizeAfterFit(t *testing.T) {
	rt := &windowSizeTmux{recordingTmux: &recordingTmux{}, pinned: "smallest"}
	plan := []restore.Action{
		restore.SplitPane{Target: "@7", Cwd: "/b", StartupCommand: "htop"},
		restore.SetLayout{Window: "@7", Layout: "L"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"split-window", "-t", "@7", "-c", "/b", "-P", "-F", "#{pane_id}"},
		{"display-message", "-p", "-t", "@7", "#{window_width} #{window_height}"},
		{"select-layout", "-t", "@7", "L"},
		{"show-options", "-wv", "-t", "@7", "window-size"},
		{"resize-window", "-t", "@7", "-x", "138", "-y", "39"},
		{"set-window-option", "-t", "@7", "window-size", "smallest"},
		{"clear-history", "-t", "%11"},
		{"respawn-pane", "-k", "-t", "%11", "htop"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

type windowSizeTmux struct {
	*recordingTmux
	pinned string
}

func (w *windowSizeTmux) Run(ctx context.Context, args []string) (string, error) {
	out, err := w.recordingTmux.Run(ctx, args)
	if args[0] == "show-options" {
		return w.pinned, err
	}
	return out, err
}

// A failed split must neither respawn nor disturb a sibling pane's launch.
func TestApplyDoesNotRespawnFailedSplit(t *testing.T) {
	rt := &recordingTmux{failFlag: "/b", failErr: errors.New("no space for new pane")}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Cwd: "/a", StartupCommand: "a"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b", StartupCommand: "b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	failed, err := restore.Apply(context.Background(), rt, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %v, want only the split", failed)
	}
	var respawns [][]string
	for _, c := range rt.calls {
		if c[0] == "respawn-pane" {
			respawns = append(respawns, c)
		}
	}
	if diff := cmp.Diff([][]string{{"respawn-pane", "-k", "-t", "%11", "a"}}, respawns); diff != "" {
		t.Errorf("respawns mismatch (-want +got):\n%s", diff)
	}
}

// Panes without a startup command keep tmux's default-command: nothing to
// respawn.
func TestApplyDoesNotRespawnPanesWithoutStartupCommand(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Cwd: "/a"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	for _, c := range rt.calls {
		if c[0] == "respawn-pane" {
			t.Errorf("unexpected respawn: %v", c)
		}
	}
}
