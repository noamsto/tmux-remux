package restore_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-remux/internal/restore"
)

const newSessionFormat = "#{window_id} #{window_index}"

type recordingTmux struct {
	calls [][]string
	// windowOut stands in for what `new-session -P -F` prints: the window id
	// and the index tmux actually placed it at. Defaults to "@1 1".
	windowOut     string
	newSessionErr error
	// failFlag makes Run return failErr for any call containing that argument,
	// which is how the -b fallback path is exercised.
	failFlag string
	failErr  error
}

func (r *recordingTmux) Run(_ context.Context, args []string) (string, error) {
	r.calls = append(r.calls, args)
	if r.failFlag != "" && slices.Contains(args, r.failFlag) {
		return "", r.failErr
	}
	if len(args) == 0 || args[0] != "new-session" {
		return "", nil
	}
	if r.newSessionErr != nil {
		return "", r.newSessionErr
	}
	if r.windowOut != "" {
		return r.windowOut, nil
	}
	return "@1 1", nil
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
		{"split-window", "-t", "s1:1", "-c", "/b"},
		{"select-layout", "-t", "s1:1", "L"},
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
		{"new-session", "-d", "-s", "s1", "-n", "main", "-c", "/a", "-P", "-F", newSessionFormat, startup},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyMovesFirstWindowToItsRecordedIndex(t *testing.T) {
	rt := &recordingTmux{windowOut: "@7 1"}
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
		{"new-window", "-t", "s1:2", "-n", "main", "-c", "/a"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyAppendsStartupCommandWhenPresent(t *testing.T) {
	rt := &recordingTmux{}
	startup := `'/usr/bin/tmux-remux' cat-scrollback abc; exec /bin/zsh`
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", StartupCommand: startup},
		restore.SplitPane{Target: "s1:1", Cwd: "/b", StartupCommand: "htop"},
	}
	if _, err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a", startup},
		{"split-window", "-t", "s1:1", "-c", "/b", "htop"},
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
		{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a"},
		{"set-window-option", "-t", "s1:1", "automatic-rename", "on"},
		{"new-window", "-t", "s1:2", "-n", "named", "-c", "/a"},
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
	if calls != 3 {
		t.Errorf("expected 3 attempted calls (best-effort), got %d", calls)
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 reported failure, got %d: %v", len(failed), failed)
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
		{"new-window", "-b", "-t", "s1:3", "-n", "docs", "-c", "/a"},
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
		{"new-window", "-t", "s1:3", "-n", "docs", "-c", "/a"},
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
		{"new-window", "-b", "-t", "s1:3", "-n", "docs", "-c", "/a"},
		{"new-window", "-t", "s1:3", "-n", "docs", "-c", "/a"},
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
