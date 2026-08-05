# Session-Aware Undo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the close/undo picker group closes by session in an indented tmux-shaped tree, scope `undo --pop` to the invoking session, and restore an undone window to its original index.

**Architecture:** Close events gain a stored session name so every event can be attributed to a session without a snapshot diff. A new `internal/picker/closetree.go` groups events into a two-group hierarchy (this session / other sessions) that the view renders with box-drawing guides and the model navigates. Independently, `restore.CreateWindow` gains an `InsertBefore` flag that emits `new-window -b`, which places a window at its original index instead of appending it past the session's live max.

**Tech Stack:** Go 1.25.5, Bubble Tea v2 (`charm.land/bubbletea/v2`), Lipgloss v2, bubbles/key, kong CLI, modernc SQLite, `github.com/google/go-cmp` for test diffs.

**Spec:** `docs/superpowers/specs/2026-08-05-session-aware-undo-design.md`

## Global Constraints

- Worktree: `~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo`, branch `feat/61-session-aware-undo`. Never commit to `main`.
- Every `git commit` must run inside `nix develop -c`, or the lint/vet pre-commit hooks cannot find `go`.
- Run commands from the worktree root with a leading `cd`, not via `sh -c` or a subshell — the default-branch commit guard false-positives otherwise.
- Test commands: `nix develop -c go test ./...` for the suite, `nix develop -c go test ./internal/picker/ -run TestName -v` for one test.
- `internal/picker` has two test packages: `package picker_test` (external, e.g. `tree_test.go`) and `package picker` (internal, e.g. `view_internal_test.go`). Put a test wherever its subject's visibility requires; do not export something just to test it.
- Tmux session ids (`$N`) are NOT stable across server restarts. Never map a stored `session_id` to a live session.
- Window ids (`@N`) are stable only within one server lifetime, and a restored window always has a fresh id.
- `new-window -b -t sess:N` places at exactly N when N is free and inserts-and-shifts when N is taken. Both verified on tmux next-3.8 with `base-index 1`, `renumber-windows on`.
- Do not add `-b` to the full snapshot-replay path — a mid-plan insert shifts windows that later actions target by index.

---

### Task 1: Store the session name on close events

**Files:**
- Modify: `internal/closeevent/diff.go:17-23` (`CloseManifest`)
- Modify: `internal/closeevent/capture.go:19-28` (`Args`), `internal/closeevent/capture.go:84-91` (the marshal in `Capture`)
- Modify: `cmd/tmux-remux/main.go:586-613` (`CaptureEventCmd` and its `Run`)
- Modify: `tmux-remux.tmux:141-147`, `examples/tmux.conf:14-20`
- Test: `internal/closeevent/capture_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `closeevent.CloseManifest.SessionName string` and `closeevent.Args.SessionName string`. Task 2 reads `CloseManifest.SessionName`.

- [ ] **Step 1: Write the failing test**

Append to `internal/closeevent/capture_test.go`:

```go
func TestCaptureStoresSessionName(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := closeevent.Capture(ctx, db, closeevent.Args{
		Kind: "window-unlinked", WindowID: "@5", SessionID: "$1",
		SessionName: "lazytmux", Host: "h",
	}); err != nil {
		t.Fatal(err)
	}

	all, _ := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 10})
	if len(all) != 1 {
		t.Fatalf("expected one event, got %d", len(all))
	}
	cm, err := closeevent.ParseManifest(all[0].ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if cm.SessionName != "lazytmux" {
		t.Errorf("SessionName = %q, want %q", cm.SessionName, "lazytmux")
	}
}

// A pre-change event has no stored name; parsing must leave it empty rather
// than fail, so the snapshot-diff fallback can take over.
func TestParseManifestWithoutSessionNameLeavesItEmpty(t *testing.T) {
	cm, err := closeevent.ParseManifest(`{"session_id":"$1","window_id":"@5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if cm.SessionName != "" {
		t.Errorf("SessionName = %q, want empty", cm.SessionName)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/closeevent/ -run 'TestCaptureStoresSessionName|TestParseManifestWithoutSessionName' -v`

Expected: FAIL to compile — `unknown field SessionName in struct literal of type closeevent.Args`.

- [ ] **Step 3: Add the field to `CloseManifest`**

In `internal/closeevent/diff.go`, replace the `CloseManifest` struct:

```go
type CloseManifest struct {
	SessionID string `json:"session_id"`
	// SessionName is the session the closed entity belonged to, captured from
	// #{hook_session_name} at hook time. Empty on events recorded before it was
	// stored, and on after-kill-pane events (a command hook gets no hook_*
	// formats) — both fall back to the snapshot diff for attribution.
	SessionName string    `json:"session_name,omitempty"`
	WindowID    string    `json:"window_id"`
	PaneID      string    `json:"pane_id"`
	Index       IndexPost `json:"index"`
}
```

- [ ] **Step 4: Thread it through `Capture`**

In `internal/closeevent/capture.go`, add to `Args` after `SessionID`:

```go
	SessionID   string
	SessionName string
```

and in `Capture`, replace the marshal call:

```go
	wrapped, err := json.Marshal(CloseManifest{
		SessionID:   a.SessionID,
		SessionName: a.SessionName,
		WindowID:    a.WindowID,
		PaneID:      a.PaneID,
		Index:       a.Index,
	})
```

`resolveKilledPane` copies `a` into `lost`, so the name carries through the id-less `after-kill-pane` path without further changes.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/closeevent/ -run 'TestCaptureStoresSessionName|TestParseManifestWithoutSessionName' -v`

Expected: PASS (both tests).

- [ ] **Step 6: Add the CLI flag**

In `cmd/tmux-remux/main.go`, replace `CaptureEventCmd`:

```go
// CaptureEventCmd records a close event (called from tmux hooks).
type CaptureEventCmd struct {
	Kind        string `arg:"" help:"event kind"`
	Session     string `help:"tmux session id ($N)"`
	SessionName string `name:"session-name" help:"tmux session name (#{hook_session_name})"`
	Window      string `help:"tmux window id (@N)"`
	Pane        string `help:"tmux pane id (%N)"`
}
```

and in its `Run`, add the field to the `closeevent.Args` literal:

```go
		_, err := closeevent.Capture(ctx, db, closeevent.Args{
			Kind:        c.Kind,
			SessionID:   c.Session,
			SessionName: c.SessionName,
			WindowID:    c.Window,
			PaneID:      c.Pane,
			Host:        hostname(),
			Index:       post,
		})
```

- [ ] **Step 7: Pass the format from the hooks**

In `tmux-remux.tmux`, in `wire_plugin`, add `--session-name=#{hook_session_name}` to the three notify hooks. Leave `after-kill-pane` alone — it is a command hook, so `hook_*` formats are empty there and `#{session_name}` would name the client's session rather than the victim pane's:

```bash
  tmux set-hook -g pane-exited     "run-shell -b \"'${bin}' capture-event pane-died --pane=#{hook_pane} --window=#{hook_window} --session=#{hook_session} --session-name=#{hook_session_name}\""
  tmux set-hook -g window-unlinked "run-shell -b \"'${bin}' capture-event window-unlinked --window=#{hook_window} --session=#{hook_session} --session-name=#{hook_session_name}\""
  tmux set-hook -g session-closed  "run-shell -b \"'${bin}' capture-event session-closed --session=#{hook_session} --session-name=#{hook_session_name}\""
```

Mirror the same three additions in `examples/tmux.conf` lines 14, 19 and 20, keeping that file's existing quoting and column alignment.

- [ ] **Step 8: Run the full suite**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./...`

Expected: PASS (all packages).

- [ ] **Step 9: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/closeevent cmd/tmux-remux/main.go tmux-remux.tmux examples/tmux.conf
nix develop -c git commit -m "feat(closeevent): record the session name on close events

An unrecoverable close has no snapshot diff and therefore no resolved
entity, so nothing could attribute it to a session. hook_session_name
survives session destruction, which is exactly the case where a session
id can no longer be looked up.

Refs #61"
```

---

### Task 2: Session attribution chain

**Files:**
- Create: `internal/closeevent/owner.go`
- Test: `internal/closeevent/owner_test.go`

**Interfaces:**
- Consumes: `closeevent.CloseManifest.SessionName` (Task 1).
- Produces: `closeevent.OwnerSession(post CloseManifest, item *ClosedItem) string` and the exported constant `closeevent.UnknownSession = "(unknown session)"`. Tasks 5 and 8 both call it.

- [ ] **Step 1: Write the failing test**

Create `internal/closeevent/owner_test.go`:

```go
package closeevent_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
)

func TestOwnerSession(t *testing.T) {
	tests := []struct {
		name string
		post closeevent.CloseManifest
		item *closeevent.ClosedItem
		want string
	}{
		{
			name: "stored name wins",
			post: closeevent.CloseManifest{SessionName: "lazytmux"},
			item: &closeevent.ClosedItem{SessionName: "stale"},
			want: "lazytmux",
		},
		{
			name: "falls back to the snapshot diff",
			post: closeevent.CloseManifest{SessionID: "$3"},
			item: &closeevent.ClosedItem{SessionName: "mono"},
			want: "mono",
		},
		{
			name: "unrecoverable event with a stored name is still attributed",
			post: closeevent.CloseManifest{SessionName: "mono"},
			item: nil,
			want: "mono",
		},
		{
			name: "no name anywhere is unknown, never guessed from the id",
			post: closeevent.CloseManifest{SessionID: "$3"},
			item: nil,
			want: closeevent.UnknownSession,
		},
		{
			name: "an item with an empty name does not mask the unknown case",
			post: closeevent.CloseManifest{},
			item: &closeevent.ClosedItem{Session: &snapshot.Session{}},
			want: closeevent.UnknownSession,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := closeevent.OwnerSession(tc.post, tc.item); got != tc.want {
				t.Errorf("OwnerSession = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/closeevent/ -run TestOwnerSession -v`

Expected: FAIL to compile — `undefined: closeevent.OwnerSession`.

- [ ] **Step 3: Write the implementation**

Create `internal/closeevent/owner.go`:

```go
package closeevent

// UnknownSession labels a close event whose owning session cannot be
// determined. Such an event is still restorable — the sub-manifest carries the
// session name it will be restored into — it just cannot be grouped.
const UnknownSession = "(unknown session)"

// OwnerSession reports which tmux session a close event belonged to.
//
// The stored name comes first: it was captured at hook time, so it is correct
// even for a session that no longer exists. The snapshot diff is second, which
// covers events recorded before the name was stored and every after-kill-pane
// event (a command hook carries no hook_* formats).
//
// The event's session_id is deliberately never consulted. tmux restarts session
// ids at $0 with every server, so a stale id can collide with a live session's
// and attribute a close to the wrong one. Unknown beats wrong.
func OwnerSession(post CloseManifest, item *ClosedItem) string {
	if post.SessionName != "" {
		return post.SessionName
	}
	if item != nil && item.SessionName != "" {
		return item.SessionName
	}
	return UnknownSession
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/closeevent/ -run TestOwnerSession -v`

Expected: PASS (all five subtests).

- [ ] **Step 5: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/closeevent/owner.go internal/closeevent/owner_test.go
nix develop -c git commit -m "feat(closeevent): attribute a close event to its session

Stored name first, snapshot diff second, unknown third. The event's
session_id is never consulted: tmux restarts session ids at \$0 with
every server, so a stale id can collide with a live session's.

Refs #61"
```

---

### Task 3: Restore a window to its original index

**Files:**
- Modify: `internal/restore/plan.go:18-36` (`CreateWindow`)
- Modify: `internal/restore/apply.go:73-99` (`createWindow`), plus a new `isUsageError` helper
- Modify: `cmd/tmux-remux/main.go:356-366` (`buildRestorePlan`); delete `reindexIntoLiveSessions` at `cmd/tmux-remux/main.go:368-406`
- Test: `internal/restore/apply_test.go`; delete `TestReindexIntoLiveSessions` and `TestReindexLeavesFreeIndexAndDeadSessionAlone` from `cmd/tmux-remux/undo_test.go:130-167`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `restore.CreateWindow.InsertBefore bool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/restore/apply_test.go`:

```go
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
```

Extend `recordingTmux` (at `internal/restore/apply_test.go:15-35`) so a test can fail only the call carrying a given flag:

```go
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
```

Add `"slices"` to that file's import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/restore/ -run 'TestApplyInsertsWindow|TestApplyOmitsInsertBefore|TestApplyRetriesWithout|TestApplyDoesNotRetry' -v`

Expected: FAIL to compile — `unknown field InsertBefore in struct literal of type restore.CreateWindow`.

- [ ] **Step 3: Add the field**

In `internal/restore/plan.go`, add to `CreateWindow` after `NewSession`:

```go
	// InsertBefore emits new-window -b, which places the window at Index
	// exactly: tmux inserts and shifts the survivors up when Index is taken,
	// and places it directly when Index is free. Set only for single-entity
	// restores (undo, close picker) — inserting mid-plan would shift windows
	// that later actions in the same plan target by index.
	InsertBefore bool
```

- [ ] **Step 4: Emit and guard the flag**

In `internal/restore/apply.go`, replace the tail of `createWindow` (from `args := []string{"new-window", ...}` to the end of the function) with:

```go
	newWindowArgs := func(insertBefore bool) []string {
		a := []string{"new-window"}
		if insertBefore {
			a = append(a, "-b")
		}
		a = append(a, "-t", target, "-n", v.Name, "-c", v.Cwd)
		if v.StartupCommand != "" {
			a = append(a, v.StartupCommand)
		}
		return a
	}
	if _, err := t.Run(ctx, newWindowArgs(v.InsertBefore)); err != nil {
		// A tmux without new-window -b rejects the command line itself. Drop
		// the flag and let tmux place the window, rather than losing it.
		if !v.InsertBefore || !isUsageError(err) {
			return err
		}
		if _, err := t.Run(ctx, newWindowArgs(false)); err != nil {
			return err
		}
	}
	reenableAutomaticRename(ctx, t, v.AutomaticRename, target)
	return nil
}

// isUsageError reports whether err is tmux rejecting the command line — an
// unknown flag — rather than refusing the operation. tmux answers a bad flag
// with "usage:" and the command synopsis.
func isUsageError(err error) bool {
	return strings.Contains(err.Error(), "usage:")
}
```

`strings` is already imported by that file.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/restore/ -v`

Expected: PASS, including the four new tests.

- [ ] **Step 6: Set the flag on the single-entity path and delete the reindex hack**

In `cmd/tmux-remux/main.go`, replace `buildRestorePlan`'s window branch so it stops reindexing and marks the windows for exact placement:

```go
func buildRestorePlan(ctx context.Context, t *tmux.Client, item *closeevent.ClosedItem, prior snapshot.Manifest, opts restore.BuildOptions) ([]restore.Action, snapshot.Manifest) {
	m := item.SubManifest(prior.Host, prior.SavedAt)
	if item.Pane != nil {
		return restore.BuildPaneRestore(*item.Pane, *item.Window, item.SessionName, windowLive(ctx, t, item.Window.ID), opts), m
	}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, opts)
	// A single restored window goes back to the index it was closed at.
	// new-window -b shifts whatever renumbering moved into that slot, so the
	// window lands where the user remembers it rather than past the live max.
	for i, a := range plan {
		if cw, ok := a.(restore.CreateWindow); ok && !cw.NewSession {
			cw.InsertBefore = true
			plan[i] = cw
		}
	}
	return plan, m
}
```

Then delete `reindexIntoLiveSessions` entirely (`cmd/tmux-remux/main.go:368-406`), and delete `TestReindexIntoLiveSessions` and `TestReindexLeavesFreeIndexAndDeadSessionAlone` from `cmd/tmux-remux/undo_test.go`. Remove the now-unused `tmux` import from `undo_test.go` if nothing else in the file uses it.

`NewSession` windows are skipped because `createWindow` creates those through `new-session` plus a `move-window` to the recorded index, which already places them exactly.

- [ ] **Step 7: Run the full suite**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./...`

Expected: PASS with no changes to `integration_test.go`. It never asserted the
append-past-max behavior, and its `TestRestoreFirstWindowAtBaseIndex` covers the
`NewSession` path, which this change deliberately skips.

- [ ] **Step 8: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/restore cmd/tmux-remux
nix develop -c git commit -m "feat(restore): put an undone window back at its original index

renumber-windows shifts a survivor into the closed window's slot, so the
recorded index is nearly always taken and the window was being appended
past the session's live max. new-window -b inserts and shifts instead,
and places exactly when the index is free.

Refs #61"
```

---

### Task 4: Resolve a lost pane's parent window by identity chain

**Files:**
- Modify: `internal/restore/plan.go:212-228` (`BuildPaneRestore`)
- Modify: `cmd/tmux-remux/main.go:356-366` (`buildRestorePlan`); replace `windowLive` at `cmd/tmux-remux/main.go:420-431` with `parentWindowTarget`
- Test: `internal/restore/pane_restore_test.go`, `cmd/tmux-remux/undo_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `restore.BuildPaneRestore(lost snapshot.Pane, win snapshot.Window, session, liveTarget string, opts BuildOptions) []Action` — the `windowLive bool` parameter is replaced by `liveTarget string`, where `""` means the window is not live and must be recreated.

- [ ] **Step 1: Write the failing test**

Append to `internal/restore/pane_restore_test.go`:

```go
// The caller resolves the live window, so BuildPaneRestore splits into exactly
// the target it is handed — including a window id that differs from the
// snapshot's, which is what a re-created window has.
func TestBuildPaneRestoreSplitsIntoResolvedTarget(t *testing.T) {
	lost := snapshot.Pane{Index: 2, Cwd: "/b"}
	win := snapshot.Window{Index: 3, Name: "docs", Layout: "L", ID: "@9"}

	plan := restore.BuildPaneRestore(lost, win, "mono", "@42", restore.BuildOptions{})

	want := []restore.Action{
		restore.SplitPane{Target: "@42", Cwd: "/b"},
		restore.SetLayout{Window: "@42", Layout: "L"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPaneRestoreRecreatesWindowWhenNoLiveTarget(t *testing.T) {
	lost := snapshot.Pane{Index: 2, Cwd: "/b"}
	win := snapshot.Window{Index: 3, Name: "docs", Layout: "L", ID: "@9",
		Panes: []snapshot.Pane{{Index: 1, Cwd: "/a"}}}

	plan := restore.BuildPaneRestore(lost, win, "mono", "", restore.BuildOptions{})

	if len(plan) == 0 {
		t.Fatal("expected a window-recreating plan, got none")
	}
	cw, ok := plan[0].(restore.CreateWindow)
	if !ok {
		t.Fatalf("plan[0] = %T, want restore.CreateWindow", plan[0])
	}
	if cw.Session != "mono" || cw.Index != 3 {
		t.Errorf("CreateWindow = %+v, want session mono index 3", cw)
	}
}
```

Add to `cmd/tmux-remux/undo_test.go`:

```go
// A window that was itself restored carries a fresh @id, so matching only on
// the snapshot's id would report "not live" and recreate the window a second
// time. Name and index are the fallbacks.
func TestMatchParentWindow(t *testing.T) {
	live := []tmux.WindowRow{
		{Session: "mono", Index: 1, Name: "shell", ID: "@1"},
		{Session: "mono", Index: 7, Name: "docs", ID: "@42"},
		{Session: "other", Index: 3, Name: "docs", ID: "@50"},
	}
	tests := []struct {
		name    string
		session string
		win     snapshot.Window
		want    string
	}{
		{"id match wins", "mono", snapshot.Window{ID: "@42", Name: "renamed", Index: 99}, "@42"},
		{"stale id falls back to name in session", "mono", snapshot.Window{ID: "@9", Name: "docs", Index: 99}, "@42"},
		{"name miss falls back to index in session", "mono", snapshot.Window{ID: "@9", Name: "gone", Index: 7}, "@42"},
		{"never crosses sessions", "mono", snapshot.Window{ID: "@9", Name: "nothing", Index: 3}, ""},
		{"no match is not live", "mono", snapshot.Window{ID: "@9", Name: "nothing", Index: 88}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchParentWindow(live, tc.session, tc.win); got != tc.want {
				t.Errorf("matchParentWindow = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/restore/ ./cmd/tmux-remux/ -run 'TestBuildPaneRestore|TestMatchParentWindow' -v`

Expected: FAIL to compile — `undefined: matchParentWindow`, and too many arguments to `BuildPaneRestore`.

- [ ] **Step 3: Change `BuildPaneRestore`**

In `internal/restore/plan.go`, replace the whole function:

```go
// BuildPaneRestore plans the recovery of one lost pane. liveTarget is the tmux
// target of its parent window as resolved by the caller, or "" when that window
// is gone — in which case the window is rebuilt from the snapshot and brings
// its panes with it.
func BuildPaneRestore(lost snapshot.Pane, win snapshot.Window, session, liveTarget string, opts BuildOptions) []Action {
	if liveTarget == "" {
		plan, _ := BuildPlan(snapshot.Manifest{
			V:        1,
			Sessions: []snapshot.Session{{Name: session, Windows: []snapshot.Window{win}}},
		}, filter.Filter{}, nil, opts)
		return plan
	}
	return []Action{
		SplitPane{Target: liveTarget, Cwd: lost.Cwd, StartupCommand: paneStartup(lost, opts)},
		SetLayout{Window: liveTarget, Layout: win.Layout},
	}
}
```

- [ ] **Step 4: Replace `windowLive` with the resolver**

In `cmd/tmux-remux/main.go`, delete `windowLive` and add:

```go
// parentWindowTarget resolves the live tmux target of a lost pane's parent
// window, or "" when no live window matches. Returns a window id, which is
// unambiguous for split-window -t.
func parentWindowTarget(ctx context.Context, t *tmux.Client, session string, win snapshot.Window) string {
	live, err := t.ListWindows(ctx)
	if err != nil {
		return ""
	}
	return matchParentWindow(live, session, win)
}

// matchParentWindow picks the live window that is `win`, trying id, then name
// within the session, then index within the session. The id is only stable
// within one server lifetime and a window that was itself restored carries a
// fresh one, so an id miss must not be read as "the window is gone" — that
// would recreate a window that is sitting right there. Name and index are
// scoped to the session so a same-named window elsewhere can never match.
func matchParentWindow(live []tmux.WindowRow, session string, win snapshot.Window) string {
	if win.ID != "" {
		for _, w := range live {
			if w.ID == win.ID {
				return w.ID
			}
		}
	}
	if name := snapshot.StripFormat(win.Name); name != "" {
		for _, w := range live {
			if w.Session == session && snapshot.StripFormat(w.Name) == name {
				return w.ID
			}
		}
	}
	for _, w := range live {
		if w.Session == session && w.Index == win.Index {
			return w.ID
		}
	}
	return ""
}
```

Then update the pane branch of `buildRestorePlan`:

```go
	if item.Pane != nil {
		target := parentWindowTarget(ctx, t, item.SessionName, *item.Window)
		return restore.BuildPaneRestore(*item.Pane, *item.Window, item.SessionName, target, opts), m
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/restore/ ./cmd/tmux-remux/ -v`

Expected: PASS. Update any other `BuildPaneRestore` call site the compiler flags — pass `""` where a test previously passed `false`, and a window id where it passed `true`.

- [ ] **Step 6: Run the full suite**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/restore cmd/tmux-remux
nix develop -c git commit -m "fix(restore): resolve a lost pane's parent window by id, name, then index

A restored window has a fresh @id, so matching only the snapshot's id
reported the window as gone and recreated it a second time when a pane
from it was restored afterwards.

Refs #61"
```

---

### Task 5: Group close events into a session tree

**Files:**
- Create: `internal/picker/closetree.go`
- Modify: `internal/picker/model.go:80-84` (`CloseContext`)
- Modify: `cmd/tmux-remux/main.go:536-567` (`buildCloseContexts`)
- Test: `internal/picker/closetree_test.go`

**Interfaces:**
- Consumes: `closeevent.OwnerSession` and `closeevent.UnknownSession` (Task 2).
- Produces:
  - `picker.CloseNode` with fields `Kind CloseNodeKind`, `Label string`, `Ts int64`, `EventID int64`, `State string`, `Parent *CloseNode`, `Children []*CloseNode`, `Expanded bool`
  - `picker.CloseNodeKind` constants `GroupThis`, `GroupOther`, `CSession`, `CWindow`, `CPane`
  - `picker.ClosePlacement` with fields `Session string`, `WindowIndex int`, `WindowName string`, `Scope string`, `PaneCount int`
  - `picker.CloseContext.Placement ClosePlacement`
  - `picker.BuildCloseTree(evs []store.Event, ctxs map[int64]CloseContext, current string, live map[string]bool) *CloseNode`
  - `picker.IsCloseGroup(n *CloseNode) bool`
  - `picker.FlattenClose(root *CloseNode) []*CloseNode`

  Tasks 6 and 7 consume all of these.

- [ ] **Step 1: Write the failing test**

Create `internal/picker/closetree_test.go`:

```go
package picker_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/picker"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// ctx builds a CloseContext whose sub-manifest is non-empty (the recoverable
// gate) with the given placement.
func closeCtx(session string, idx int, winName, scope string, panes int) picker.CloseContext {
	return picker.CloseContext{
		Label: scope + " label",
		Placement: picker.ClosePlacement{
			Session: session, WindowIndex: idx, WindowName: winName,
			Scope: scope, PaneCount: panes,
		},
		SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{Name: session}}},
	}
}

// childLabels returns the labels of n's children, in order.
func childLabels(n *picker.CloseNode) []string {
	out := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.Label)
	}
	return out
}

// findChild returns the child of n whose label matches, or nil.
func findChild(n *picker.CloseNode, label string) *picker.CloseNode {
	for _, c := range n.Children {
		if c.Label == label {
			return c
		}
	}
	return nil
}

func TestBuildCloseTree_PaneNestsUnderItsWindow(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 300, Kind: "window-unlinked"},
		{ID: 2, Ts: 200, Kind: "pane-died"},
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("mono", 2, "main", "pane", 0),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupThis {
		t.Fatalf("root children = %v, want a single this-session group", childLabels(root))
	}
	group := root.Children[0]
	if len(group.Children) != 1 {
		t.Fatalf("group children = %v, want one window node", childLabels(group))
	}
	win := group.Children[0]
	if win.Kind != picker.CWindow || win.EventID != 1 {
		t.Errorf("window node = %+v, want CWindow carrying event 1", win)
	}
	if len(win.Children) != 1 || win.Children[0].EventID != 2 || win.Children[0].Kind != picker.CPane {
		t.Errorf("window children = %+v, want the pane event nested", win.Children)
	}
}

func TestBuildCloseTree_LiveWindowIsAHeaderNotSelectable(t *testing.T) {
	evs := []store.Event{{ID: 2, Ts: 200, Kind: "pane-died"}}
	ctxs := map[int64]picker.CloseContext{2: closeCtx("mono", 5, "docs", "pane", 0)}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	win := root.Children[0].Children[0]
	if win.EventID != 0 {
		t.Errorf("header window EventID = %d, want 0 (not selectable)", win.EventID)
	}
	if win.State != "live" {
		t.Errorf("header window State = %q, want \"live\"", win.State)
	}
}

func TestBuildCloseTree_GoneSessionHeader(t *testing.T) {
	evs := []store.Event{{ID: 3, Ts: 100, Kind: "pane-died"}}
	ctxs := map[int64]picker.CloseContext{3: closeCtx("lazytmux", 0, "shell", "pane", 0)}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupOther {
		t.Fatalf("root children = %v, want only the other-sessions group", childLabels(root))
	}
	sess := root.Children[0].Children[0]
	if sess.Kind != picker.CSession || sess.Label != "lazytmux" {
		t.Fatalf("session node = %+v, want CSession lazytmux", sess)
	}
	if sess.State != "gone" {
		t.Errorf("State = %q, want \"gone\" (session is not live)", sess.State)
	}
	if sess.EventID != 0 {
		t.Errorf("EventID = %d, want 0 — no session-close event was recorded", sess.EventID)
	}
}

func TestBuildCloseTree_SessionCloseIsSelectableAndParentsOlderWindows(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 400, Kind: "session-closed"},
		{ID: 2, Ts: 100, Kind: "window-unlinked"},
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("lazytmux", 0, "", "session", 0),
		2: closeCtx("lazytmux", 1, "shell", "window", 2),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	sess := root.Children[0].Children[0]
	if sess.EventID != 1 {
		t.Errorf("session node EventID = %d, want 1 (the session close)", sess.EventID)
	}
	if len(sess.Children) != 1 || sess.Children[0].EventID != 2 {
		t.Errorf("session children = %+v, want the older window close nested", sess.Children)
	}
}

func TestBuildCloseTree_ThisSessionFirstAndExpanded(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 100, Kind: "window-unlinked"}, // older, but this session
		{ID: 2, Ts: 900, Kind: "window-unlinked"}, // newer, other session
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("lazytmux", 3, "docs", "window", 1),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true, "lazytmux": true})

	if len(root.Children) != 2 {
		t.Fatalf("root children = %v, want both groups", childLabels(root))
	}
	if root.Children[0].Kind != picker.GroupThis {
		t.Error("this-session group must come first even when another session has a newer close")
	}
	if !root.Children[0].Expanded {
		t.Error("this-session group must start expanded")
	}
	if root.Children[1].Expanded {
		t.Error("other-sessions group must start collapsed")
	}
}

func TestBuildCloseTree_SortsByNewestDescendant(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 100, Kind: "window-unlinked"},
		{ID: 2, Ts: 500, Kind: "pane-died"},
		{ID: 3, Ts: 300, Kind: "window-unlinked"},
	}
	ctxs := map[int64]picker.CloseContext{
		// Window 2 has an OLD close but a NEW pane inside it, so its subtree is
		// the newest and it must sort ahead of window 4.
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("mono", 2, "main", "pane", 0),
		3: closeCtx("mono", 4, "docs", "window", 1),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	group := root.Children[0]
	if len(group.Children) != 2 {
		t.Fatalf("group children = %v, want two windows", childLabels(group))
	}
	if group.Children[0].EventID != 1 {
		t.Errorf("first window EventID = %d, want 1 (newest descendant wins)", group.Children[0].EventID)
	}
}

func TestBuildCloseTree_UnattributedEventGroupsUnderUnknown(t *testing.T) {
	evs := []store.Event{{ID: 1, Ts: 100, Kind: "window-unlinked"}}
	ctxs := map[int64]picker.CloseContext{1: closeCtx("", 2, "main", "window", 1)}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupOther {
		t.Fatalf("root children = %v, want the other-sessions group", childLabels(root))
	}
	if got := root.Children[0].Children[0].Label; got != closeevent.UnknownSession {
		t.Errorf("session label = %q, want %q", got, closeevent.UnknownSession)
	}
}

func TestBuildCloseTree_NoSessionContextPutsEverythingInOther(t *testing.T) {
	evs := []store.Event{{ID: 1, Ts: 100, Kind: "window-unlinked"}}
	ctxs := map[int64]picker.CloseContext{1: closeCtx("mono", 2, "main", "window", 1)}

	root := picker.BuildCloseTree(evs, ctxs, "", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupOther {
		t.Fatalf("root children = %v, want only the other-sessions group", childLabels(root))
	}
	if findChild(root.Children[0], "mono") == nil {
		t.Error("expected a session node for mono")
	}
}

func TestBuildCloseTree_SkipsUnrecoverableEvents(t *testing.T) {
	evs := []store.Event{{ID: 1, Ts: 100, Kind: "window-unlinked"}}
	// No entry in ctxs at all: the event never resolved to an entity.
	root := picker.BuildCloseTree(evs, map[int64]picker.CloseContext{}, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 0 {
		t.Errorf("root children = %v, want none", childLabels(root))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/picker/ -run TestBuildCloseTree -v`

Expected: FAIL to compile — `undefined: picker.BuildCloseTree`.

- [ ] **Step 3: Add `Placement` to `CloseContext`**

In `internal/picker/model.go`, replace the `CloseContext` struct:

```go
// CloseContext is the picker-facing summary of a single close event, used to
// render rich row labels and a preview-pane tree. The Label is shown alongside
// the timestamp; SubManifest is rendered as the close-mode preview tree and
// is also what restore.BuildPlan operates on when Enter is pressed.
type CloseContext struct {
	Label       string
	Placement   ClosePlacement
	SubManifest snapshot.Manifest
}
```

- [ ] **Step 4: Write the implementation**

Create `internal/picker/closetree.go`:

```go
package picker

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// CloseNodeKind identifies the level of a CloseNode.
type CloseNodeKind int

// Close-node kinds. The two Group kinds are the top-level section headers.
const (
	GroupThis CloseNodeKind = iota
	GroupOther
	CSession
	CWindow
	CPane
)

// ClosePlacement locates a close event in the tmux hierarchy so the tree can
// nest it. Filled by the caller from the resolved closeevent.ClosedItem plus
// the event's own stored session name.
type ClosePlacement struct {
	Session     string
	WindowIndex int
	WindowName  string
	Scope       string // "session" | "window" | "pane"
	PaneCount   int    // panes in the closed window; 0 for other scopes
}

// CloseNode is one row of the close picker's tree.
//
// A node is either an event or a header. An event node carries Ts and EventID
// and can be restored; a header node exists only to parent something else and
// carries State ("live" | "gone") instead. A window or session node can be
// both: a session close that also parents older window closes, for instance.
type CloseNode struct {
	Kind     CloseNodeKind
	Label    string
	Ts       int64  // 0 on a pure header
	EventID  int64  // 0 = not selectable
	State    string // "live" | "gone" | ""
	Parent   *CloseNode
	Children []*CloseNode
	Expanded bool
}

// IsCloseGroup reports whether n is one of the two top-level section headers.
// Guides are not drawn for these, and the walk that builds a child's guide
// prefix stops here.
func IsCloseGroup(n *CloseNode) bool {
	return n.Kind == GroupThis || n.Kind == GroupOther
}

// BuildCloseTree groups recoverable close events under "this session" and
// "other sessions".
//
// The current session gets no session node: its group header already names it,
// so its windows hang directly off the group. Only other sessions need the
// extra level.
//
// evs is expected newest-first (store.ListEvents orders by ts DESC), but the
// result does not depend on that — every level is sorted by newest descendant
// at the end.
func BuildCloseTree(evs []store.Event, ctxs map[int64]CloseContext, current string, live map[string]bool) *CloseNode {
	root := &CloseNode{Expanded: true}
	thisGroup := &CloseNode{Kind: GroupThis, Label: "this session · " + current, Parent: root, Expanded: true}
	otherGroup := &CloseNode{Kind: GroupOther, Label: "other sessions", Parent: root}

	sessions := map[string]*CloseNode{}
	windows := map[string]*CloseNode{}

	for _, ev := range evs {
		cc, ok := ctxs[ev.ID]
		// No context, or nothing to restore: the caller counts these as hidden
		// rather than listing a dead row.
		if !ok || len(cc.SubManifest.Sessions) == 0 {
			continue
		}
		p := cc.Placement
		name := p.Session
		if name == "" {
			name = closeevent.UnknownSession
		}
		// A session close means that session is not the one we are sitting in,
		// so it always belongs to the other group — which also keeps the
		// this-group header from ever having to carry an event id.
		mine := current != "" && name == current && p.Scope != "session"

		if p.Scope == "session" {
			n := sessionNode(sessions, otherGroup, name, live)
			n.Ts, n.EventID, n.State = ev.Ts, ev.ID, ""
			continue
		}

		parent := thisGroup
		if !mine {
			parent = sessionNode(sessions, otherGroup, name, live)
		}
		wkey := name + "\x1f" + strconv.Itoa(p.WindowIndex) + "\x1f" + p.WindowName
		w, ok := windows[wkey]
		if !ok {
			// A pure header window: it exists because a pane died inside it and
			// no close event for the window itself was recorded, so the window
			// is still there whenever its session is.
			state := "gone"
			if live[name] {
				state = "live"
			}
			w = &CloseNode{
				Kind:   CWindow,
				Label:  windowLabel(p.WindowIndex, p.WindowName, 0),
				State:  state,
				Parent: parent,
			}
			parent.Children = append(parent.Children, w)
			windows[wkey] = w
		}
		if p.Scope == "window" {
			w.Ts, w.EventID, w.State = ev.Ts, ev.ID, ""
			w.Label = windowLabel(p.WindowIndex, p.WindowName, p.PaneCount)
			continue
		}
		w.Children = append(w.Children, &CloseNode{
			Kind: CPane, Label: cc.Label, Ts: ev.Ts, EventID: ev.ID, Parent: w,
		})
	}

	for _, g := range []*CloseNode{thisGroup, otherGroup} {
		if len(g.Children) == 0 {
			continue
		}
		sortCloseTree(g)
		root.Children = append(root.Children, g)
	}
	return root
}

// sessionNode returns the cached session node for name, creating it under
// group on first use.
func sessionNode(cache map[string]*CloseNode, group *CloseNode, name string, live map[string]bool) *CloseNode {
	if n, ok := cache[name]; ok {
		return n
	}
	state := "gone"
	if live[name] {
		state = "live"
	}
	n := &CloseNode{Kind: CSession, Label: name, State: state, Parent: group}
	group.Children = append(group.Children, n)
	cache[name] = n
	return n
}

// windowLabel renders "<index>: <name>", with " (Np)" appended when the window
// itself was closed and its pane count is known.
func windowLabel(index int, name string, panes int) string {
	label := fmt.Sprintf("%d: %s", index, snapshot.StripFormat(name))
	if panes > 0 {
		label += fmt.Sprintf(" (%dp)", panes)
	}
	return label
}

// newestTs returns the newest timestamp in the subtree rooted at n, so a header
// sorts by the freshest thing inside it.
func newestTs(n *CloseNode) int64 {
	t := n.Ts
	for _, c := range n.Children {
		if ct := newestTs(c); ct > t {
			t = ct
		}
	}
	return t
}

// sortCloseTree orders every level newest-first. The root's children are the
// two groups and keep their fixed order, so callers sort each group instead.
func sortCloseTree(n *CloseNode) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		return newestTs(n.Children[i]) > newestTs(n.Children[j])
	})
	for _, c := range n.Children {
		sortCloseTree(c)
	}
}

// FlattenClose returns the visible rows of the tree, honoring Expanded. The
// synthetic root is not included.
func FlattenClose(root *CloseNode) []*CloseNode {
	if root == nil {
		return nil
	}
	var out []*CloseNode
	var walk func(n *CloseNode)
	walk = func(n *CloseNode) {
		out = append(out, n)
		if !n.Expanded {
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, g := range root.Children {
		walk(g)
	}
	return out
}
```

The guide-prefix helpers (`closeGuidePrefix`, `hasLaterSibling`) deliberately do
NOT belong to this task — see Task 7 Step 0. They are unexported and their only
caller is Task 7's row renderer, so adding them here would ship uncalled code
that the `unused` linter rejects, and suppressing it with `//nolint` is not
allowed in this repo.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/picker/ -run TestBuildCloseTree -v`

Expected: PASS (all ten tests).

- [ ] **Step 6: Populate `Placement` from the resolved item**

In `cmd/tmux-remux/main.go`, replace the tail of `buildCloseContexts`'s loop body (the `out[ev.ID] = ...` assignment) with:

```go
		out[ev.ID] = picker.CloseContext{
			Label:       item.Describe(),
			Placement:   placementFor(closeMan, item),
			SubManifest: item.SubManifest(prior.Host, prior.SavedAt),
		}
```

and add below it:

```go
// placementFor locates a resolved close in the tmux hierarchy for the picker's
// tree. Scope is read off which field of the item is set — Pane before Window,
// since a pane-died carries both.
func placementFor(closeMan closeevent.CloseManifest, item *closeevent.ClosedItem) picker.ClosePlacement {
	p := picker.ClosePlacement{Session: closeevent.OwnerSession(closeMan, item)}
	if p.Session == closeevent.UnknownSession {
		p.Session = ""
	}
	switch {
	case item.Session != nil:
		p.Scope = "session"
	case item.Pane != nil:
		p.Scope = "pane"
		p.WindowIndex, p.WindowName = item.WindowIndex, item.Window.Name
	case item.Window != nil:
		p.Scope = "window"
		p.WindowIndex, p.WindowName = item.WindowIndex, item.Window.Name
		p.PaneCount = len(item.Window.Panes)
	}
	return p
}
```

`BuildCloseTree` maps an empty `Session` back to `closeevent.UnknownSession`, so blanking it here keeps one owner of that label.

- [ ] **Step 7: Run the full suite**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./...`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/picker cmd/tmux-remux/main.go
nix develop -c git commit -m "feat(picker): group close events into a session tree

Sections closes into this-session and other-sessions and nests them the
way tmux does, so a dead pane sits under the window it died in. The
current session needs no session node — its group header names it.

Refs #61"
```

---

### Task 6: Navigate the close tree

**Files:**
- Modify: `internal/picker/model.go` — add the `closeTree` field, `SetCloseTree`, `CloseVisible`, `currentEventID`, and close-mode key handling in `handleKey`
- Test: `internal/picker/model_test.go`

**Interfaces:**
- Consumes: `picker.CloseNode`, `picker.FlattenClose`, `picker.IsCloseGroup` (Task 5).
- Produces: `(*PickerModel).SetCloseTree(root *CloseNode)`, `(PickerModel).CloseVisible() []*CloseNode`, `(PickerModel).CurrentEventID() int64`. Task 7 renders from `CloseVisible` and highlights `m.cursor`; Task 8 calls `SetCloseTree`.

- [ ] **Step 1: Write the failing test**

Append to `internal/picker/model_test.go`. It is `package picker_test`, the same
package as `closetree_test.go`, so `closeCtx` from Task 5 is already in scope —
do not redefine it. Key messages are built inline as `tea.KeyPressMsg{Code:
tea.KeyDown}`, matching every existing test in the file; there is no `keyPress`
helper and adding one would diverge from the file's style.

```go
// closeTreeModel builds a close-mode model over two sessions: mono (this
// session) with window 2 holding a dead pane, and lazytmux with its own close.
func closeTreeModel() picker.PickerModel {
	evs := []store.Event{
		{ID: 1, Ts: 300, Kind: "window-unlinked"},
		{ID: 2, Ts: 200, Kind: "pane-died"},
		{ID: 3, Ts: 100, Kind: "window-unlinked"},
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("mono", 2, "main", "pane", 0),
		3: closeCtx("lazytmux", 3, "docs", "window", 1),
	}
	m := picker.NewPickerModel(picker.ModeClose, evs, map[string]bool{"mono": true}, nil)
	m.SetCloseContexts(ctxs)
	m.SetCloseTree(picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true}))
	m.Bootstrap()
	return m
}

func TestCloseTreeCursorStartsOnNewestSelectableClose(t *testing.T) {
	m := closeTreeModel()
	if got := m.CurrentEventID(); got != 1 {
		t.Errorf("CurrentEventID = %d, want 1 (this session's newest close)", got)
	}
}

func TestCloseTreeDownSkipsGroupHeaders(t *testing.T) {
	m := closeTreeModel()
	// From the window close, Down must land on the nested pane, not on a header.
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got := next.(picker.PickerModel)
	if id := got.CurrentEventID(); id != 2 {
		t.Errorf("after Down, CurrentEventID = %d, want the nested pane event 2", id)
	}
}

func TestCloseTreeEnterOnHeaderDoesNotSelect(t *testing.T) {
	m := closeTreeModel()
	// Collapse this-session so the cursor sits on the group header itself.
	vis := m.CloseVisible()
	if len(vis) == 0 || !picker.IsCloseGroup(vis[0]) {
		t.Fatalf("expected a group header first, got %+v", vis)
	}
	m.SetCursor(0)
	after, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := after.(picker.PickerModel)
	if got.SelectedID() != 0 {
		t.Errorf("SelectedID = %d, want 0 — a header carries nothing to restore", got.SelectedID())
	}
	if got.FooterNote() == "" {
		t.Error("expected a footer note explaining the header is not restorable")
	}
}

func TestCloseTreeRightExpandsCollapsedGroup(t *testing.T) {
	m := closeTreeModel()
	before := len(m.CloseVisible())
	// The other-sessions group starts collapsed; step onto it and expand.
	vis := m.CloseVisible()
	idx := -1
	for i, n := range vis {
		if n.Kind == picker.GroupOther {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("expected an other-sessions group header")
	}
	m.SetCursor(idx)
	after, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := len(after.(picker.PickerModel).CloseVisible()); got <= before {
		t.Errorf("visible rows = %d, want more than %d after expanding", got, before)
	}
}
```

Add a test-only cursor setter to `internal/picker/model.go`:

```go
// SetCursor moves the cursor. Exported for tests; production code moves the
// cursor through key handling.
func (m *PickerModel) SetCursor(i int) { m.cursor = i }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/picker/ -run TestCloseTree -v`

Expected: FAIL to compile — `m.SetCloseTree undefined`.

- [ ] **Step 3: Add the tree state and accessors**

In `internal/picker/model.go`, add to `PickerModel` after `closeContexts`:

```go
	// closeTree is the grouped close hierarchy rendered in close mode. When
	// set, m.cursor indexes FlattenClose(closeTree) rather than m.events.
	closeTree *CloseNode
```

and add these methods:

```go
// SetCloseTree attaches the grouped close hierarchy. Call between
// NewPickerModel and Bootstrap. Close mode only.
func (m *PickerModel) SetCloseTree(root *CloseNode) { m.closeTree = root }

// CloseVisible returns the currently visible close-tree rows.
func (m PickerModel) CloseVisible() []*CloseNode { return FlattenClose(m.closeTree) }

// CurrentEventID returns the event id under the cursor, or 0 when the cursor is
// on a grouping header or there is nothing to point at.
func (m PickerModel) CurrentEventID() int64 {
	if m.closeTree == nil {
		if m.cursor < 0 || m.cursor >= len(m.events) {
			return 0
		}
		return m.events[m.cursor].ID
	}
	vis := m.CloseVisible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return 0
	}
	return vis[m.cursor].EventID
}

// isCloseNavTarget reports whether the cursor may land on n: an event row
// (restorable) or a collapsible header (so it can be opened).
func isCloseNavTarget(n *CloseNode) bool {
	return n.EventID != 0 || len(n.Children) > 0
}

// nextCloseIdx walks from start in dir (+1/-1) to the next navigable row, or
// -1 when there is none that way.
func (m PickerModel) nextCloseIdx(start, dir int) int {
	vis := m.CloseVisible()
	for i := start + dir; i >= 0 && i < len(vis); i += dir {
		if isCloseNavTarget(vis[i]) {
			return i
		}
	}
	return -1
}

// firstCloseTarget returns the first navigable row, or 0.
func (m PickerModel) firstCloseTarget() int {
	for i, n := range m.CloseVisible() {
		if isCloseNavTarget(n) {
			return i
		}
	}
	return 0
}
```

- [ ] **Step 4: Handle keys in close mode**

In `handleKey`, insert this block immediately after the `m.footerNote = ""` line and before the `if m.mode == ModeSnapshot` preview-scroll block:

```go
	// Close mode with a grouped tree: the cursor walks tree rows, so Up/Down
	// skip headers and Left/Right collapse and expand them.
	if m.mode == ModeClose && m.closeTree != nil {
		vis := m.CloseVisible()
		switch {
		case key.Matches(msg, m.keys.Up):
			if idx := m.nextCloseIdx(m.cursor, -1); idx >= 0 {
				m.cursor = idx
			}
			return m, nil
		case key.Matches(msg, m.keys.Down):
			if idx := m.nextCloseIdx(m.cursor, +1); idx >= 0 {
				m.cursor = idx
			}
			return m, nil
		case key.Matches(msg, m.keys.Right):
			if m.cursor >= 0 && m.cursor < len(vis) {
				if n := vis[m.cursor]; len(n.Children) > 0 {
					n.Expanded = true
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.Left):
			if m.cursor >= 0 && m.cursor < len(vis) {
				// Collapse the row itself when it is an open parent, otherwise
				// collapse its parent and step onto it.
				n := vis[m.cursor]
				if n.Expanded && len(n.Children) > 0 {
					n.Expanded = false
					return m, nil
				}
				if p := n.Parent; p != nil && p.Parent != nil {
					p.Expanded = false
					m.cursor = closeIndexOf(m.CloseVisible(), p)
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.Enter):
			if m.cursor < 0 || m.cursor >= len(vis) {
				return m, nil
			}
			if id := vis[m.cursor].EventID; id != 0 {
				m.selectedID = id
				return m, tea.Quit
			}
			m.footerNote = "(group — nothing to restore here)"
			return m, nil
		}
	}
```

and add:

```go
// closeIndexOf returns the position of target in vis, or 0 when absent.
func closeIndexOf(vis []*CloseNode, target *CloseNode) int {
	for i, n := range vis {
		if n == target {
			return i
		}
	}
	return 0
}
```

- [ ] **Step 5: Point the contents pane and Bootstrap at the tree**

In `ensureManifest`, replace the leading bounds check and `ev := m.events[m.cursor]` with an event-id lookup so the right-hand pane follows the tree cursor:

```go
func (m *PickerModel) ensureManifest() {
	id := m.CurrentEventID()
	if id == 0 {
		return
	}
	if _, ok := m.manifests[id]; ok {
		return
	}
	if _, bad := m.manifestErrors[id]; bad {
		return
	}
	var man snapshot.Manifest
	if m.mode == ModeClose {
		man = m.closeContexts[id].SubManifest
		if len(man.Sessions) == 0 {
			m.manifestErrors[id] = fmt.Errorf("close event has no recoverable entity")
			return
		}
	} else {
		ev := m.events[m.cursor]
		var err error
		man, err = parseEventManifest(ev)
		if err != nil {
			m.manifestErrors[id] = err
			return
		}
	}
	m.manifests[id] = man
	tree := BuildTree(man)
	FilterDecorate(tree, m.filter, m.runningSet)
	m.trees[id] = tree
}
```

In `Bootstrap`, place the cursor on the first navigable row before parsing:

```go
func (m *PickerModel) Bootstrap() {
	if m.mode == ModeClose && m.closeTree != nil {
		m.cursor = m.firstCloseTarget()
	}
	m.ensureManifest()
}
```

Then update the two remaining places that assume `m.events[m.cursor]` in close mode — `CurrentCounts` and `renderTree` (`internal/picker/view.go`) — to look up by `m.CurrentEventID()` and render the empty frame when it is 0. Also call `(&m).ensureManifest()` after each close-mode cursor move added in Step 4, mirroring the snapshot-mode handlers, so the contents pane keeps up.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/picker/ -v`

Expected: PASS, including the four new close-tree tests.

- [ ] **Step 7: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/picker
nix develop -c git commit -m "feat(picker): navigate the close tree

The cursor walks tree rows in close mode: Up/Down skip grouping headers,
Left/Right collapse and expand them, and Enter on a header notes that
there is nothing to restore rather than selecting a zero id.

Refs #61"
```

---

### Task 7: Render the close tree with guide glyphs

**Files:**
- Modify: `internal/picker/view.go` — add `renderCloseTree`, route close mode to it, drop the close branch of `renderList`
- Modify: `internal/picker/closetree.go` — add `closeGuidePrefix` and `hasLaterSibling` (see Step 0)
- Test: `internal/picker/view_internal_test.go`

**Interfaces:**
- Consumes: `CloseVisible`, `IsCloseGroup`, `CloseNode` (Tasks 5–6).
- Produces: `closeGuidePrefix(n *CloseNode) string` and `hasLaterSibling(n *CloseNode) bool`, both unexported and used only by this task's renderer.

- [ ] **Step 0: Add the guide-prefix helpers**

These live in `internal/picker/closetree.go` but belong to this task, not Task 5:
they are unexported and their only caller is `closeRow` below, so adding them
earlier would have meant shipping uncalled code past the `unused` linter behind a
`//nolint` — an escape hatch this repo does not allow. Add them now, with their
caller and their test arriving in the same commit.

```go
// closeGuidePrefix returns the box-drawing prefix for n: one "│  " or "   "
// per ancestor below the group level, then the node's own branch glyph.
func closeGuidePrefix(n *CloseNode) string {
	if n.Parent == nil || IsCloseGroup(n) {
		return ""
	}
	var segs []string
	for a := n.Parent; a != nil && !IsCloseGroup(a); a = a.Parent {
		if hasLaterSibling(a) {
			segs = append(segs, "│  ")
		} else {
			segs = append(segs, "   ")
		}
	}
	slices.Reverse(segs)
	branch := "└─ "
	if hasLaterSibling(n) {
		branch = "├─ "
	}
	out := branch
	for i := len(segs) - 1; i >= 0; i-- {
		out = segs[i] + out
	}
	return out
}

func hasLaterSibling(n *CloseNode) bool {
	if n.Parent == nil {
		return false
	}
	sibs := n.Parent.Children
	return len(sibs) > 0 && sibs[len(sibs)-1] != n
}
```

Add `"slices"` to `closetree.go`'s import block if Task 5's cleanup removed it.

- [ ] **Step 1: Write the failing test**

Append to `internal/picker/view_internal_test.go`:

```go
// closeTreeFixture builds the tree used by the rendering tests: mono (current)
// with window 2 closed and a pane inside it, plus a gone lazytmux session.
func closeTreeFixture() *CloseNode {
	evs := []store.Event{
		{ID: 1, Ts: 300, Kind: "window-unlinked"},
		{ID: 2, Ts: 200, Kind: "pane-died"},
		{ID: 3, Ts: 100, Kind: "window-unlinked"},
	}
	one := snapshot.Manifest{Sessions: []snapshot.Session{{Name: "mono"}}}
	ctxs := map[int64]CloseContext{
		1: {Label: "w", Placement: ClosePlacement{Session: "mono", WindowIndex: 2, WindowName: "main", Scope: "window", PaneCount: 1}, SubManifest: one},
		2: {Label: "pane: nvim", Placement: ClosePlacement{Session: "mono", WindowIndex: 2, WindowName: "main", Scope: "pane"}, SubManifest: one},
		3: {Label: "w", Placement: ClosePlacement{Session: "lazytmux", WindowIndex: 3, WindowName: "docs", Scope: "window", PaneCount: 1}, SubManifest: one},
	}
	return BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})
}

func TestCloseGuidePrefixes(t *testing.T) {
	root := closeTreeFixture()
	// Expand the other-sessions group so its subtree is visible.
	for _, g := range root.Children {
		g.Expanded = true
		for _, c := range g.Children {
			c.Expanded = true
		}
	}
	want := map[string]string{
		"this session · mono": "",
		"2: main (1p)":        "└─ ",
		"pane: nvim":          "   └─ ",
		"other sessions":      "",
		"lazytmux":            "└─ ",
		"3: docs (1p)":        "   └─ ",
	}
	for _, n := range FlattenClose(root) {
		exp, ok := want[n.Label]
		if !ok {
			t.Errorf("unexpected row %q", n.Label)
			continue
		}
		if got := closeGuidePrefix(n); got != exp {
			t.Errorf("prefix for %q = %q, want %q", n.Label, got, exp)
		}
	}
}

// The guide prefix is part of the row, so it must be inside the truncation
// budget: a deep row with a long label must not widen the frame.
func TestRenderCloseTree_NeverOverflowsFrame(t *testing.T) {
	applyTheme(NewTheme())
	root := closeTreeFixture()
	for _, g := range root.Children {
		g.Expanded = true
		for _, c := range g.Children {
			c.Expanded = true
			c.Label = "a-really-long-window-name-that-will-not-fit-in-any-narrow-pane (3p)"
		}
	}
	m := PickerModel{mode: ModeClose, closeTree: root}
	for _, size := range []struct{ w, h int }{{32, 8}, {40, 6}, {80, 12}, {28, 5}} {
		m.width, m.height = size.w, size.h
		out := renderCloseTree(m, size.w, size.h)
		if got := lipgloss.Width(out); got != size.w {
			t.Errorf("width %d height %d: rendered width %d, want %d", size.w, size.h, got, size.w)
		}
		if got := lipgloss.Height(out); got != size.h {
			t.Errorf("width %d height %d: rendered height %d, want %d", size.w, size.h, got, size.h)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/picker/ -run 'TestCloseGuidePrefixes|TestRenderCloseTree' -v`

Expected: FAIL to compile — `undefined: renderCloseTree`.

- [ ] **Step 3: Write the renderer**

Add to `internal/picker/view.go`:

```go
// renderCloseTree renders the grouped close hierarchy into the list pane.
// One physical row per visible node: every row is truncated to the frame's
// inner width — guide prefix included — because a lipgloss frame wraps
// overflow instead of clipping it, which would desync scrollWindow.
func renderCloseTree(m PickerModel, width, height int) string {
	frame := listFrame.Width(width).Height(height).MaxHeight(height)
	vis := m.CloseVisible()
	if len(vis) == 0 {
		msg := "No close events yet."
		if m.hiddenCount > 0 {
			msg = fmt.Sprintf("No recoverable closes (%d hidden).", m.hiddenCount)
		}
		return frame.Render(rowDim.Render(msg))
	}

	innerWidth := width - listFrame.GetHorizontalFrameSize()
	if innerWidth < 1 {
		innerWidth = 1
	}
	rows := height - 2
	if rows < 1 {
		rows = 1
	}
	showFooter := m.hiddenCount > 0 && rows > 1
	nodeRows := rows
	if showFooter {
		nodeRows--
	}
	start, end := scrollWindow(m.cursor, len(vis), nodeRows)

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(closeRow(vis[i], innerWidth, i == m.cursor))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	if showFooter {
		for pad := end - start; pad < nodeRows; pad++ {
			b.WriteString("\n")
		}
		b.WriteString("\n")
		text := ansi.Truncate(fmt.Sprintf("— %s hidden —", hiddenPhrase(m.hiddenCount)), innerWidth, "…")
		b.WriteString(rowDim.Width(innerWidth).Align(lipgloss.Center).Render(text))
	}
	return frame.Render(b.String())
}

// closeRow renders one tree row: guide prefix, expand marker, label, and a
// right-aligned timestamp for event rows. State markers ("· live" / "· gone")
// mark headers that carry nothing to restore.
func closeRow(n *CloseNode, innerWidth int, active bool) string {
	left := closeGuidePrefix(n)
	switch {
	case len(n.Children) > 0 && n.Expanded:
		left += "▾ "
	case len(n.Children) > 0:
		left += "▸ "
	}
	left += n.Label
	if n.State != "" {
		left += " · " + n.State
	}

	right := ""
	if n.Ts != 0 {
		right = time.UnixMilli(n.Ts).Format("15:04")
	}
	// Reserve the timestamp plus one separating space, then pad the gap.
	budget := innerWidth
	if right != "" {
		budget -= len(right) + 1
	}
	if budget < 1 {
		budget = 1
	}
	left = ansi.Truncate(left, budget, "…")
	line := left
	if right != "" {
		if gap := innerWidth - lipgloss.Width(left) - len(right); gap > 0 {
			line += strings.Repeat(" ", gap)
		} else {
			line += " "
		}
		line += right
	}
	line = ansi.Truncate(line, innerWidth, "…")

	if active {
		// One flat style: lipgloss v2 strips ESC bytes from pre-styled input,
		// so nesting a role color inside rowActive's background can collapse to
		// invisible. Same reason appendNodeRows renders the active row plain.
		return rowActive.Width(innerWidth).Render(line)
	}
	style := nodePane
	switch n.Kind {
	case GroupThis, GroupOther:
		style = previewHeader
	case CSession:
		style = nodeSession
	case CWindow:
		style = nodeWindow
	}
	if n.EventID == 0 {
		style = style.Faint(true).Italic(true)
	}
	return style.Width(innerWidth).Render(line)
}
```

- [ ] **Step 4: Route close mode to it**

In `View`, replace the close-mode arm and the narrow arm so close mode always renders the tree:

```go
	var content string
	switch {
	case m.mode == ModeClose && m.closeTree != nil && m.width < 80:
		content = lipgloss.JoinVertical(lipgloss.Left, renderCloseTree(m, m.width, bodyHeight), footer)
	case m.mode == ModeClose && m.closeTree != nil:
		closes := renderCloseTree(m, listWidth, bodyHeight)
		tree := renderTree(m, m.width-listWidth, bodyHeight)
		body := lipgloss.JoinHorizontal(lipgloss.Top, closes, tree)
		content = lipgloss.JoinVertical(lipgloss.Left, body, footer)
	case m.width < 80:
		content = lipgloss.JoinVertical(lipgloss.Left, list, footer)
	case m.mode == ModeClose:
		tree := renderTree(m, m.width-listWidth, bodyHeight)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree)
		content = lipgloss.JoinVertical(lipgloss.Left, body, footer)
	case previewWidth == 0:
		tree := renderTree(m, treeWidth, bodyHeight)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree)
		content = lipgloss.JoinVertical(lipgloss.Left, body, footer)
	default:
		tree := renderTree(m, treeWidth, bodyHeight)
		preview := m.renderPreview(previewWidth)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree, preview)
		content = lipgloss.JoinVertical(lipgloss.Left, body, footer)
	}
```

`list` is only computed for the non-tree arms — move its assignment into those arms if the compiler reports it unused. The `m.closeTree == nil` close arm stays as the fallback for callers that never set a tree, which is what `TestRenderList_NeverOverflowsFrame` exercises.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./internal/picker/ -v`

Expected: PASS, including both new tests and the pre-existing frame-overflow tests.

- [ ] **Step 6: Commit**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add internal/picker
nix develop -c git commit -m "feat(picker): render the close tree with box-drawing guides

Depth is carried by guide glyphs rather than whitespace, which is what
made the old flat list read as flat in a pane this narrow. The guide
prefix counts against the truncation budget so a deep row cannot widen
the frame.

Refs #61"
```

---

### Task 8: Scope undo and the picker to the invoking session

**Files:**
- Modify: `cmd/tmux-remux/main.go` — `UndoCmd`, `PickCmd`, `restorableClose`, `discardSummary`, plus a `currentSession` helper
- Modify: `tmux-remux.tmux` (the `u` and `U` binds), `examples/tmux.conf`
- Test: `cmd/tmux-remux/undo_test.go`

**Interfaces:**
- Consumes: `closeevent.OwnerSession` (Task 2), `picker.BuildCloseTree` and `(*PickerModel).SetCloseTree` (Tasks 5–6).
- Produces: `restorableClose(ctx, db, session string) (undoTarget, error)` — the signature gains a session; `undoTarget` gains `FromSession string`.

- [ ] **Step 1: Write the failing test**

Replace the three existing `restorableClose` call sites in `cmd/tmux-remux/undo_test.go` with `restorableClose(ctx, db, "")` (no session context reproduces today's behavior), then append:

```go
// seedTwoSessionStore snapshots two sessions so closes in either can resolve:
// mono (window @9) and lazytmux (window @20).
func seedTwoSessionStore(ctx context.Context, t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	snap := snapshot.Manifest{V: 1, Host: "h", SavedAt: 100, Sessions: []snapshot.Session{
		{Name: "mono", Windows: []snapshot.Window{{
			Index: 4, Name: "win", Layout: "L", ID: "@9",
			Panes: []snapshot.Pane{{Index: 1, Cwd: "/m", Command: "fish", ID: "%9"}},
		}}},
		{Name: "lazytmux", Windows: []snapshot.Window{{
			Index: 2, Name: "docs", Layout: "L", ID: "@20",
			Panes: []snapshot.Pane{{Index: 1, Cwd: "/l", Command: "fish", ID: "%20"}},
		}}},
	}}
	insertEvent(ctx, t, db, 100, "snapshot", string(mustJSON(t, snap)))
	return db
}

// namedCloseManifest builds a window close that carries its own session name,
// the way a post-change hook records it.
func namedCloseManifest(t *testing.T, closedID, session string) string {
	t.Helper()
	return string(mustJSON(t, closeevent.CloseManifest{WindowID: closedID, SessionName: session}))
}

func TestRestorableClosePrefersTheCurrentSession(t *testing.T) {
	ctx := context.Background()
	db := seedTwoSessionStore(ctx, t)

	mine := insertEvent(ctx, t, db, 200, "window-unlinked", namedCloseManifest(t, "@9", "mono"))
	// Newer, but it belongs to another session — pressing u in mono must not
	// reach across and resurrect it.
	insertEvent(ctx, t, db, 300, "window-unlinked", namedCloseManifest(t, "@20", "lazytmux"))

	target, err := restorableClose(ctx, db, "mono")
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if !target.OK || target.Event.ID != mine {
		t.Fatalf("popped event %d ok=%v, want mono's event %d", target.Event.ID, target.OK, mine)
	}
	if target.FromSession != "" {
		t.Errorf("FromSession = %q, want empty — this was not a cross-session fallback", target.FromSession)
	}
}

func TestRestorableCloseFallsBackAcrossSessions(t *testing.T) {
	ctx := context.Background()
	db := seedTwoSessionStore(ctx, t)

	other := insertEvent(ctx, t, db, 300, "window-unlinked", namedCloseManifest(t, "@20", "lazytmux"))

	target, err := restorableClose(ctx, db, "mono")
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if !target.OK || target.Event.ID != other {
		t.Fatalf("popped event %d ok=%v, want the fallback event %d", target.Event.ID, target.OK, other)
	}
	if target.FromSession != "lazytmux" {
		t.Errorf("FromSession = %q, want \"lazytmux\" so the message can name it", target.FromSession)
	}
}

func TestRestorableCloseDiscardsOnlyThisSessionsDeadRows(t *testing.T) {
	ctx := context.Background()
	db := seedTwoSessionStore(ctx, t)

	mine := insertEvent(ctx, t, db, 200, "window-unlinked", namedCloseManifest(t, "@9", "mono"))
	// Unrecoverable and in mono: discard it, since it can never come back.
	dead := insertEvent(ctx, t, db, 400, "window-unlinked", namedCloseManifest(t, "@77", "mono"))
	// Unrecoverable but in lazytmux: leave it for a press over there, so that
	// session still gets its own "never made it into a snapshot" message.
	insertEvent(ctx, t, db, 500, "window-unlinked", namedCloseManifest(t, "@88", "lazytmux"))

	target, err := restorableClose(ctx, db, "mono")
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if len(target.Discarded) != 1 || target.Discarded[0].ID != dead {
		t.Fatalf("Discarded = %+v, want just mono's dead event %d", target.Discarded, dead)
	}
	if !target.OK || target.Event.ID != mine {
		t.Errorf("popped event %d, want %d behind the discarded row", target.Event.ID, mine)
	}
}

// Discarding this session's dead rows while another session still holds a
// restorable close must promise a next press, not claim the history is
// exhausted — the next press falls back and restores that close.
func TestRestorableCloseReportsMoreWhenOnlyAFallbackSurvives(t *testing.T) {
	ctx := context.Background()
	db := seedTwoSessionStore(ctx, t)

	dead := insertEvent(ctx, t, db, 400, "window-unlinked", namedCloseManifest(t, "@77", "mono"))
	insertEvent(ctx, t, db, 300, "window-unlinked", namedCloseManifest(t, "@20", "lazytmux"))

	target, err := restorableClose(ctx, db, "mono")
	if err != nil {
		t.Fatalf("restorableClose: %v", err)
	}
	if len(target.Discarded) != 1 || target.Discarded[0].ID != dead {
		t.Fatalf("Discarded = %+v, want just %d", target.Discarded, dead)
	}
	if target.OK {
		t.Error("OK = true, want false — nothing in mono is restorable")
	}
	if !target.MoreAvailable {
		t.Error("MoreAvailable = false, want true — lazytmux's close survives for the next press")
	}
	if got := discardSummary(target.Discarded, target.MoreAvailable); !strings.Contains(got, "prefix+u again") {
		t.Errorf("summary = %q, want a hint to press again", got)
	}
}

func TestDiscardSummaryNamesTheFallbackSession(t *testing.T) {
	if got := undoMessage("lazytmux"); !strings.Contains(got, "lazytmux") {
		t.Errorf("message = %q, want it to name the source session", got)
	}
	if got := undoMessage(""); got != "" {
		t.Errorf("message = %q, want empty for a same-session undo", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./cmd/tmux-remux/ -run 'TestRestorableClose|TestDiscardSummary' -v`

Expected: FAIL to compile — too many arguments to `restorableClose`, and `undefined: undoMessage`.

- [ ] **Step 3: Scope the scan**

In `cmd/tmux-remux/main.go`, add `FromSession` to `undoTarget`:

```go
	// FromSession names the session an event was borrowed from when the current
	// session had nothing restorable. Empty for a same-session undo.
	FromSession string
	// MoreAvailable reports whether anything restorable survives behind the
	// discarded run — in this session or, via the cross-session fallback, in
	// another. It drives the "press again" half of the discard message, which
	// would otherwise claim nothing older is recoverable while a fallback close
	// is sitting there waiting for the next press.
	MoreAvailable bool
```

and replace `restorableClose`:

```go
// restorableClose finds the close event to undo. It prefers the newest
// restorable close owned by `session`, falling back to the newest anywhere when
// that session has none — reaching across is better than refusing to restore
// something the user can see is gone, as long as the message says where it came
// from. An empty `session` means no session context and scans server-wide.
//
// Unrecoverable events are discarded only when they belong to `session`.
// Discarding is garbage collection — a close no snapshot captured can never
// become restorable — but scoping it keeps the message honest: consuming another
// session's dead rows here would rob that session of its own explanation.
func restorableClose(ctx context.Context, db *store.Store, session string) (undoTarget, error) {
	evs, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: undoScanLimit})
	if err != nil {
		return undoTarget{}, err
	}
	var t undoTarget
	var fallback *undoTarget
	for _, ev := range evs {
		item, prior, ok := resolveEvent(ctx, db, ev)
		owner := eventOwner(ev, item)
		mine := session == "" || owner == session
		// Defense-in-depth on the sub-manifest: every item FindClosed returns
		// now yields a non-empty one, but guard against a future resolver that
		// can't build a restore plan rather than popping an un-restorable head.
		if !ok || len(item.SubManifest(prior.Host, prior.SavedAt).Sessions) == 0 {
			if mine {
				t.Discarded = append(t.Discarded, ev)
			}
			continue
		}
		if mine {
			t.Event, t.Item, t.Prior, t.OK, t.MoreAvailable = ev, item, prior, true, true
			return t, nil
		}
		if fallback == nil {
			fallback = &undoTarget{Event: ev, Item: item, Prior: prior, OK: true, FromSession: owner}
		}
	}
	// Nothing restorable in this session. Discarded rows are reported first —
	// this press explains them and the next one restores — so a pending fallback
	// only sets MoreAvailable here rather than being returned.
	if len(t.Discarded) > 0 {
		t.MoreAvailable = fallback != nil
		return t, nil
	}
	if fallback != nil {
		fallback.MoreAvailable = true
		return *fallback, nil
	}
	return t, nil
}

// eventOwner reports which session a close event belonged to.
func eventOwner(ev store.Event, item *closeevent.ClosedItem) string {
	closeMan, err := closeevent.ParseManifest(ev.ManifestJSON)
	if err != nil {
		return closeevent.UnknownSession
	}
	return closeevent.OwnerSession(closeMan, item)
}

// undoMessage returns the note to print after a cross-session undo, or "" when
// the restore came from the session the user is in.
func undoMessage(fromSession string) string {
	if fromSession == "" {
		return ""
	}
	return fmt.Sprintf("restored from session %s — nothing was closed in this one", fromSession)
}
```

- [ ] **Step 4: Add the flags and wire the picker**

Add the flag to both commands:

```go
// UndoCmd restores the most recent close event.
type UndoCmd struct {
	Pop     bool   `help:"restore most recent close event and remove it from history"`
	Session string `help:"session to prefer (#{session_name}); falls back to the attached client's"`
}
```

```go
// PickCmd opens an interactive picker over events.
type PickCmd struct {
	Kind    string `default:"snapshot" enum:"snapshot,close" help:"snapshot|close"`
	Session string `help:"session to group by (#{session_name}); falls back to the attached client's"`
}
```

and add the resolver:

```go
// currentSession resolves the session the user is acting from: the flag when the
// keybinding passed one, else the attached client's session. Installs that wire
// tmux-remux from their own config rather than tmux-remux.tmux will not pass the
// flag, so the client lookup is the path that keeps them session-aware. Empty
// means no context, which scans server-wide.
func currentSession(ctx context.Context, t *tmux.Client, flag string) string {
	if flag != "" {
		return flag
	}
	out, err := t.Run(ctx, []string{"display-message", "-p", "#{client_session}"})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
```

Add `"strings"` to the import block.

In `UndoCmd.Run`, resolve the session and report a cross-session restore:

```go
		t := tmux.NewClient("tmux")
		target, err := restorableClose(ctx, db, currentSession(ctx, t, c.Session))
		if err != nil {
			return err
		}
		if len(target.Discarded) > 0 {
			if err := deleteEvents(ctx, db, target.Discarded); err != nil {
				return err
			}
			return fmt.Errorf("%s", discardSummary(target.Discarded, target.MoreAvailable))
		}
		if !target.OK {
			return fmt.Errorf("nothing to undo — no recoverable close event")
		}
		opts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
		plan, m := buildRestorePlan(ctx, t, target.Item, target.Prior, opts)
		if _, err := restore.Apply(ctx, t, plan); err != nil {
			return err
		}
		focusRestored(ctx, t, m)
		if err := deleteEvents(ctx, db, []store.Event{target.Event}); err != nil {
			return err
		}
		if note := undoMessage(target.FromSession); note != "" {
			_, _ = t.Run(ctx, []string{"display-message", note})
		}
		return nil
```

Note the `t := tmux.NewClient("tmux")` moves above `restorableClose`, so delete the later duplicate declaration.

In `PickCmd.Run`, build and attach the tree in the close-mode branch:

```go
		if mode == picker.ModeClose {
			ctxs = buildCloseContexts(ctx, db, evs)
			evs, hidden = partitionRecoverable(evs, ctxs)
		}
		m := picker.NewPickerModel(mode, evs, runningSet, sb)
		if mode == picker.ModeClose {
			m.SetCloseContexts(ctxs)
			m.SetHiddenCount(hidden)
			m.SetCloseTree(picker.BuildCloseTree(evs, ctxs, currentSession(ctx, t, c.Session), runningSet))
		}
		m.Bootstrap()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./cmd/tmux-remux/ -v`

Expected: PASS, including the four new tests.

- [ ] **Step 6: Pass the session from the keybindings**

In `tmux-remux.tmux`, in `wire_plugin`:

```bash
  tmux bind-key u   run-shell "'${bin}' undo --pop --session='#{session_name}'"
  tmux bind-key U   display-popup -E -w 90% -h 85% "'${bin}' pick --kind=close --session='#{session_name}'"
```

Mirror both in `examples/tmux.conf`, keeping that file's quoting style.

- [ ] **Step 7: Run the full suite and vet**

Run: `cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo && nix develop -c go test ./... && nix develop -c go vet ./...`

Expected: PASS, no vet findings.

- [ ] **Step 8: Manual verification**

In a live tmux server with `renumber-windows on`:

1. `tmux source-file <your tmux.conf>` so the new binds are active.
2. In session A, create windows 1–4, kill window 3, press `prefix+u`. The window must return at index 3 with the survivor shifted to 4.
3. Close a window in session B, switch to session A, press `prefix+u`. It must report nothing to undo in A only if A has no closes — otherwise it must restore A's, not B's.
4. Press `prefix+U`. The tree must show `this session · A` expanded with guide glyphs, `other sessions` collapsed, and a pane close nested under its window.
5. Press `Enter` on the `other sessions` header — expect the footer note, no restore.

- [ ] **Step 9: Commit and open the PR**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/feat-61-session-aware-undo
git add cmd/tmux-remux tmux-remux.tmux examples/tmux.conf
nix develop -c git commit -m "feat(undo): scope undo and the close picker to the invoking session

prefix+u restored the server-wide newest close, so pressing it in one
session could resurrect a window from another. It now prefers the current
session and says so when it has to reach across. Dead rows are discarded
per-session, so each session keeps its own explanation.

Closes #61"
git push -u origin feat/61-session-aware-undo
gh pr create --assignee @me --fill
```

---

## Notes for the implementer

- **Do not add `-b` to the snapshot-replay path.** `restore.BuildPlan` emits windows in ascending index order; an insert shifts windows that later actions target by `session:index`, so a multi-window restore would scramble. Task 3 sets `InsertBefore` only on the single-entity plan built by `buildRestorePlan`.
- **`internal/picker` frames clip nothing.** `lipgloss` pads short content but lets long content wrap onto extra physical lines, which desyncs every one-row-per-item assumption in `scrollWindow`. Any new row must be `ansi.Truncate`d to the frame's inner width *including* its guide prefix. The two `NeverOverflowsFrame` tests are the guard.
- **`rowActive` must render a row flat.** lipgloss v2 strips ESC bytes from pre-styled input, so nesting a role color inside the active row's background can collapse to invisible. `appendNodeRows` already documents this; `closeRow` follows it.
- Tasks 3 and 4 both touch `buildRestorePlan`. Doing them in order keeps the diffs small; if they are split across sessions, re-read the function before editing.
