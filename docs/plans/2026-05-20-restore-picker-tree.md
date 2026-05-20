# Restore Picker — Tree Preview & Filter Toggles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fzf-based `tmux-state pick` with an interactive Bubble Tea TUI that previews each snapshot's session → window → pane tree and exposes the smart-restore filter as live footer toggles.

**Architecture:** Master/detail two-pane TUI. List pane = 50 most recent events (same DB query as today). Tree pane = the highlighted snapshot's manifest as an expandable tree. Footer toggles set fields on `filter.Filter`, which is also the predicate driving the on-screen kept/skipped counter. On enter, the Tea program quits with a selected event ID; the cobra command then calls the same `restore.BuildPlan` / `restore.Apply` path the picker already uses today — just with a non-empty filter. Pure helpers (`BuildTree`, `FilterDecorate`) carry the testing weight; the Tea model is thin.

**Tech Stack:** Go (stdlib), `charm.land/bubbletea/v2`, `charm.land/bubbles/v2` (help, key), `charm.land/lipgloss/v2`. Existing internals reused: `internal/store`, `internal/snapshot`, `internal/filter`, `internal/restore`, `internal/tmux`.

**Spec:** `docs/specs/2026-05-20-restore-picker-tree-design.md`

**Out of scope:** Per-node checkboxes. Cross-snapshot diffing. Filter persistence to `config.Config`. Search-as-you-type in the list. Renaming snapshots.

**Testing model:** TDD throughout — write the failing test first, run to confirm, implement minimal code, run to confirm pass, commit. Pure helpers (`BuildTree`, `FilterDecorate`) get exhaustive table tests. Model tests drive `Update` with synthetic `tea.KeyMsg` values and assert on returned model state; no `teatest` dependency is added in v1 (the v2 API exposes `Update` cleanly).

---

## File structure

| File | Responsibility | Status |
|---|---|---|
| `internal/picker/picker.go` | The old fzf wrapper (`Pick`, `FormatRow`). | **Deleted** at end. |
| `internal/picker/tree.go` | Pure data transform: `BuildTree`, `FilterDecorate`, `Counts`, `NodeKind`, `TreeNode`. No tea/lipgloss imports. | New |
| `internal/picker/tree_test.go` | Table tests for `BuildTree` and `FilterDecorate`. | New |
| `internal/picker/keys.go` | `keyMap` (`bubbles/key.Binding`s) + `ShortHelp`/`FullHelp` for `bubbles/help`. | New |
| `internal/picker/model.go` | `PickerModel` (the `tea.Model`): state machine, key handling, lazy manifest cache. No render code. | New |
| `internal/picker/model_test.go` | Transcript-style tests over `Update`. | New |
| `internal/picker/view.go` | `lipgloss` styles + render functions (list, tree, footer, help overlay, narrow-mode modal). | New |
| `internal/picker/style.go` | Catppuccin Mocha palette constants and reusable `lipgloss.Style` values. | New |
| `cmd/tmux-state/main.go` | `newPickCmd` rewritten to construct the model, run `tea.NewProgram`, and on success call `restore.BuildPlan` + `restore.Apply` with `model.Filter()` and `model.SelectedManifest()`. | Modified |
| `go.mod`, `go.sum` | Add `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`. | Modified |
| `flake.nix` | Verify `vendorHash` rebuilds after `go.sum` changes; bump if needed. | Modified (if needed) |

---

## Phase 0: Dependencies

### Task 1: Add Bubble Tea v2 dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the three deps**

Run from the repo root:

```bash
go get charm.land/bubbletea/v2@latest
go get charm.land/bubbles/v2@latest
go get charm.land/lipgloss/v2@latest
go mod tidy
```

Expected: `go.mod` now lists all three under `require (` and `go.sum` has matching checksums.

- [ ] **Step 2: Verify the build still works**

Run: `go build ./...`
Expected: exits 0, no output. Nothing in the existing code uses the new deps yet — this just confirms the module graph is healthy.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): add charm.land bubbletea/bubbles/lipgloss v2"
```

---

## Phase 1: Pure tree helpers (TDD-heavy)

These are the workhorses. The TUI is mostly a render of what these return.

### Task 2: Define types in tree.go and write the first failing test

**Files:**
- Create: `internal/picker/tree.go`
- Create: `internal/picker/tree_test.go`

- [ ] **Step 1: Create `tree.go` with type definitions only (no logic yet)**

```go
// Package picker renders a Bubble Tea TUI over tmux-state events.
package picker

import (
	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

// NodeKind identifies the level of a TreeNode.
type NodeKind int

const (
	NodeSession NodeKind = iota
	NodeWindow
	NodePane
)

// TreeNode is one entry in the rendered tree. BuildTree returns the root with
// Kind=NodeSession at depth 0; the spec mockup wraps these in a synthetic
// "Snapshot #N" header which lives in the View, not here.
type TreeNode struct {
	Kind       NodeKind
	Label      string // display label without skip styling
	Ref        any    // *snapshot.Session | *snapshot.Window | *snapshot.Pane
	Children   []*TreeNode
	Expanded   bool   // initial state set by BuildTree (sessions+windows true, panes false)
	Skipped    bool   // set by FilterDecorate
	SkipReason string // "idle shell" | "running" | "" (empty when kept)
}

// Counts is what FilterDecorate returns so the View can render
// "<KeptPanes> panes / <SkippedPanes> skipped" in the footer.
type Counts struct {
	KeptSessions, KeptWindows, KeptPanes           int
	SkippedSessions, SkippedWindows, SkippedPanes  int
}

// BuildTree converts a manifest into a virtual root whose Children are session
// nodes. The root itself has Kind=NodeSession and Label="" — callers iterate
// root.Children rather than rendering the root.
func BuildTree(m snapshot.Manifest) *TreeNode {
	return nil // intentionally unimplemented; Task 3 fills this in
}

// FilterDecorate walks the tree and marks each node Skipped/SkipReason based on
// the filter predicates. It mutates the tree in place. Returns counts for the
// footer counter. The runningSessions argument is consulted only when
// f.DedupRunningServer is true.
func FilterDecorate(root *TreeNode, f filter.Filter, runningSessions map[string]bool) Counts {
	return Counts{} // intentionally unimplemented; Task 5 fills this in
}
```

- [ ] **Step 2: Write the first failing test for BuildTree**

Create `internal/picker/tree_test.go`:

```go
package picker_test

import (
	"testing"

	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

func TestBuildTree_TwoSessionsTwoWindowsTwoPanes(t *testing.T) {
	m := snapshot.Manifest{
		V:    1,
		Host: "h",
		Sessions: []snapshot.Session{
			{
				Name: "lazytmux",
				Windows: []snapshot.Window{
					{
						Index: 0, Name: "claude",
						Panes: []snapshot.Pane{
							{Index: 0, Cwd: "/home/u/lazytmux", Command: "zsh"},
						},
					},
				},
			},
			{
				Name: "nix-config",
				Windows: []snapshot.Window{
					{
						Index: 0, Name: "shell",
						Panes: []snapshot.Pane{
							{Index: 0, Cwd: "/home/u/nix-config", Command: "fish"},
						},
					},
				},
			},
		},
	}

	root := picker.BuildTree(m)
	if root == nil {
		t.Fatal("BuildTree returned nil")
	}
	if got := len(root.Children); got != 2 {
		t.Fatalf("root.Children = %d, want 2", got)
	}

	s0 := root.Children[0]
	if s0.Kind != picker.NodeSession || s0.Label != "lazytmux (1w)" {
		t.Errorf("session 0: kind=%v label=%q", s0.Kind, s0.Label)
	}
	if !s0.Expanded {
		t.Error("session 0: want Expanded=true by default")
	}

	w := s0.Children[0]
	if w.Kind != picker.NodeWindow || w.Label != "0: claude (1p)" {
		t.Errorf("window: kind=%v label=%q", w.Kind, w.Label)
	}
	if !w.Expanded {
		t.Error("window: want Expanded=true by default")
	}

	p := w.Children[0]
	if p.Kind != picker.NodePane {
		t.Errorf("pane: kind=%v", p.Kind)
	}
	if p.Expanded {
		t.Error("pane: want Expanded=false by default")
	}
	// Pane label format: "zsh    ~/lazytmux" (HOME-relative via shellexpand).
	// Exact home-relative formatting is asserted in a focused test below.
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestBuildTree -v`
Expected: FAIL — `root == nil` triggers the `t.Fatal`.

- [ ] **Step 4: Commit the failing test**

```bash
git add internal/picker/tree.go internal/picker/tree_test.go
git commit -m "test(picker): scaffold tree types + first BuildTree failing test"
```

### Task 3: Implement BuildTree

**Files:**
- Modify: `internal/picker/tree.go`

- [ ] **Step 1: Replace the stub `BuildTree` with the real implementation**

```go
func BuildTree(m snapshot.Manifest) *TreeNode {
	root := &TreeNode{Kind: NodeSession, Expanded: true}
	for i := range m.Sessions {
		s := &m.Sessions[i]
		sessionNode := &TreeNode{
			Kind:     NodeSession,
			Label:    sessionLabel(s),
			Ref:      s,
			Expanded: true,
		}
		for j := range s.Windows {
			w := &s.Windows[j]
			windowNode := &TreeNode{
				Kind:     NodeWindow,
				Label:    windowLabel(w),
				Ref:      w,
				Expanded: true,
			}
			for k := range w.Panes {
				p := &w.Panes[k]
				windowNode.Children = append(windowNode.Children, &TreeNode{
					Kind:     NodePane,
					Label:    paneLabel(p),
					Ref:      p,
					Expanded: false,
				})
			}
			sessionNode.Children = append(sessionNode.Children, windowNode)
		}
		root.Children = append(root.Children, sessionNode)
	}
	return root
}

func sessionLabel(s *snapshot.Session) string {
	return fmt.Sprintf("%s (%dw)", s.Name, len(s.Windows))
}

func windowLabel(w *snapshot.Window) string {
	return fmt.Sprintf("%d: %s (%dp)", w.Index, w.Name, len(w.Panes))
}

func paneLabel(p *snapshot.Pane) string {
	cwd := p.Cwd
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}
	cmd := p.Command
	if cmd == "" {
		cmd = "(none)"
	}
	return fmt.Sprintf("%-7s %s", cmd, cwd)
}
```

Add the new imports at the top of `tree.go`:

```go
import (
	"fmt"
	"os"
	"strings"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
)
```

- [ ] **Step 2: Run BuildTree tests to verify they pass**

Run: `go test ./internal/picker/ -run TestBuildTree -v`
Expected: PASS.

- [ ] **Step 3: Add a focused test for the HOME-relative pane label**

Add to `tree_test.go`:

```go
func TestBuildTree_PaneLabel_HomeRelativeCwd(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no HOME")
	}
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s",
			Windows: []snapshot.Window{{
				Name: "w",
				Panes: []snapshot.Pane{{Cwd: home + "/work", Command: "fish"}},
			}},
		}},
	}
	root := picker.BuildTree(m)
	got := root.Children[0].Children[0].Children[0].Label
	if !strings.Contains(got, "~/work") {
		t.Errorf("pane label = %q, want it to contain ~/work", got)
	}
}
```

Add to the imports of `tree_test.go`:

```go
import (
	"os"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/snapshot"
)
```

- [ ] **Step 4: Run to verify**

Run: `go test ./internal/picker/ -v`
Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/picker/tree.go internal/picker/tree_test.go
git commit -m "feat(picker): BuildTree converts manifest to TreeNode hierarchy"
```

### Task 4: Write failing test for FilterDecorate (no toggles → everything kept)

**Files:**
- Modify: `internal/picker/tree_test.go`

- [ ] **Step 1: Add the test**

```go
func TestFilterDecorate_NoToggles_AllKept(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s",
			Windows: []snapshot.Window{{
				Name: "w",
				Panes: []snapshot.Pane{
					{Index: 0, Command: "fish"},
					{Index: 1, Command: "nvim", ChildCount: 1},
				},
			}},
		}},
	}
	root := picker.BuildTree(m)
	counts := picker.FilterDecorate(root, filter.Filter{}, nil)

	if counts.KeptPanes != 2 || counts.SkippedPanes != 0 {
		t.Errorf("counts = %+v, want kept=2 skipped=0", counts)
	}
	for _, sess := range root.Children {
		if sess.Skipped {
			t.Errorf("session %q skipped with default filter", sess.Label)
		}
		for _, w := range sess.Children {
			if w.Skipped {
				t.Errorf("window %q skipped with default filter", w.Label)
			}
			for _, p := range w.Children {
				if p.Skipped {
					t.Errorf("pane %q skipped with default filter", p.Label)
				}
			}
		}
	}
}
```

Add to the imports of `tree_test.go`:

```go
"github.com/noamsto/tmux-state/internal/filter"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestFilterDecorate -v`
Expected: FAIL — `counts.KeptPanes = 0, want 2` (the stub returns a zero Counts).

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/picker/tree_test.go
git commit -m "test(picker): FilterDecorate no-toggles failing test"
```

### Task 5: Implement FilterDecorate

**Files:**
- Modify: `internal/picker/tree.go`

- [ ] **Step 1: Replace the FilterDecorate stub**

```go
func FilterDecorate(root *TreeNode, f filter.Filter, runningSessions map[string]bool) Counts {
	var c Counts
	if root == nil {
		return c
	}
	for _, sess := range root.Children {
		s, _ := sess.Ref.(*snapshot.Session)
		if s == nil {
			continue
		}
		sessionSkipped := f.SkipSession(*s, runningSessions)
		if sessionSkipped {
			sess.Skipped = true
			sess.SkipReason = sessionSkipReason(f, *s, runningSessions)
			c.SkippedSessions++
		} else {
			c.KeptSessions++
		}

		for _, win := range sess.Children {
			w, _ := win.Ref.(*snapshot.Window)
			if w == nil {
				continue
			}
			// A window is "skipped" if its session is skipped OR every pane in it
			// would be skipped. filter.Filter.SkipWindow already encodes the
			// all-panes-idle check; we OR with session-skipped here.
			windowSkipped := sessionSkipped || f.SkipWindow(*w)
			if windowSkipped {
				win.Skipped = true
				if win.SkipReason == "" {
					if sessionSkipped {
						win.SkipReason = sess.SkipReason
					} else {
						win.SkipReason = "all panes idle"
					}
				}
				c.SkippedWindows++
			} else {
				c.KeptWindows++
			}

			for _, pane := range win.Children {
				p, _ := pane.Ref.(*snapshot.Pane)
				if p == nil {
					continue
				}
				paneSkipped := windowSkipped || f.SkipPane(*p)
				if paneSkipped {
					pane.Skipped = true
					if pane.SkipReason == "" {
						switch {
						case sessionSkipped:
							pane.SkipReason = sess.SkipReason
						case f.SkipPane(*p):
							pane.SkipReason = "idle shell"
						default:
							pane.SkipReason = "all panes idle"
						}
					}
					c.SkippedPanes++
				} else {
					c.KeptPanes++
				}
			}
		}
	}
	return c
}

func sessionSkipReason(f filter.Filter, s snapshot.Session, running map[string]bool) string {
	if f.DedupRunningServer && running[s.Name] {
		return "running"
	}
	return "stale"
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./internal/picker/ -run TestFilterDecorate -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/tree.go
git commit -m "feat(picker): FilterDecorate marks tree nodes via filter.Filter"
```

### Task 6: Table tests for FilterDecorate edge cases

**Files:**
- Modify: `internal/picker/tree_test.go`

- [ ] **Step 1: Add the table test**

```go
func TestFilterDecorate_Table(t *testing.T) {
	idleShell := snapshot.Pane{Index: 0, Command: "fish", ChildCount: 0}
	busyShell := snapshot.Pane{Index: 0, Command: "fish", ChildCount: 2}
	nvim := snapshot.Pane{Index: 0, Command: "nvim", ChildCount: 0}

	mk := func(panes ...snapshot.Pane) snapshot.Manifest {
		return snapshot.Manifest{
			Sessions: []snapshot.Session{{
				Name:    "s",
				Windows: []snapshot.Window{{Name: "w", Panes: panes}},
			}},
		}
	}

	tests := []struct {
		name           string
		manifest       snapshot.Manifest
		filter         filter.Filter
		running        map[string]bool
		wantKeptPanes  int
		wantSkippedPanes int
	}{
		{
			name:           "no toggles → all kept",
			manifest:       mk(idleShell, nvim),
			filter:         filter.Filter{},
			wantKeptPanes:  2,
			wantSkippedPanes: 0,
		},
		{
			name:           "skip idle shells drops the idle fish pane",
			manifest:       mk(idleShell, nvim),
			filter:         filter.Filter{SkipIdleShells: true},
			wantKeptPanes:  1,
			wantSkippedPanes: 1,
		},
		{
			name:           "skip idle shells: busy fish (children>0) stays",
			manifest:       mk(busyShell, nvim),
			filter:         filter.Filter{SkipIdleShells: true},
			wantKeptPanes:  2,
			wantSkippedPanes: 0,
		},
		{
			name:           "dedup running drops the whole session",
			manifest:       mk(nvim),
			filter:         filter.Filter{DedupRunningServer: true},
			running:        map[string]bool{"s": true},
			wantKeptPanes:  0,
			wantSkippedPanes: 1,
		},
		{
			name:           "dedup running with bool off but matching name → kept (toggle is the gate)",
			manifest:       mk(nvim),
			filter:         filter.Filter{},
			running:        map[string]bool{"s": true},
			wantKeptPanes:  1,
			wantSkippedPanes: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := picker.BuildTree(tc.manifest)
			c := picker.FilterDecorate(root, tc.filter, tc.running)
			if c.KeptPanes != tc.wantKeptPanes || c.SkippedPanes != tc.wantSkippedPanes {
				t.Errorf("counts = kept=%d skipped=%d, want kept=%d skipped=%d",
					c.KeptPanes, c.SkippedPanes, tc.wantKeptPanes, tc.wantSkippedPanes)
			}
		})
	}
}
```

- [ ] **Step 2: Run the new tests**

Run: `go test ./internal/picker/ -run TestFilterDecorate -v`
Expected: PASS (all subtests).

- [ ] **Step 3: Commit**

```bash
git add internal/picker/tree_test.go
git commit -m "test(picker): table-test FilterDecorate edge cases"
```

---

## Phase 2: Key bindings and model state machine

### Task 7: Define keyMap in keys.go

**Files:**
- Create: `internal/picker/keys.go`

- [ ] **Step 1: Write the keyMap**

```go
package picker

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down       key.Binding
	Left, Right    key.Binding
	Tab            key.Binding
	ToggleIdle     key.Binding
	ToggleDedup    key.Binding
	ToggleAge      key.Binding
	Enter          key.Binding
	Help           key.Binding
	Quit           key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:        key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse")),
		Right:       key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand")),
		Tab:         key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		ToggleIdle:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "skip idle")),
		ToggleDedup: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dedup running")),
		ToggleAge:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "age ≤24h")),
		Enter:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "restore")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:        key.NewBinding(key.WithKeys("esc", "ctrl+c", "q"), key.WithHelp("q/esc", "quit")),
	}
}

// ShortHelp / FullHelp wire up bubbles/help.Model.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Right, k.Tab, k.Enter, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right, k.Tab},
		{k.ToggleIdle, k.ToggleDedup, k.ToggleAge},
		{k.Enter, k.Help, k.Quit},
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/picker/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/keys.go
git commit -m "feat(picker): keyMap with bubbles/key bindings + help wiring"
```

### Task 8: Scaffold PickerModel with a failing focus-switch test

**Files:**
- Create: `internal/picker/model.go`
- Create: `internal/picker/model_test.go`

- [ ] **Step 1: Create the model with the bare minimum**

```go
package picker

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/help"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
)

// Mode is "snapshot" (tree pane visible) or "close" (list-only).
type Mode int

const (
	ModeSnapshot Mode = iota
	ModeClose
)

type focusZone int

const (
	focusList focusZone = iota
	focusTree
)

// PickerModel is the Bubble Tea model for the restore picker.
type PickerModel struct {
	mode           Mode
	events         []store.Event
	cursor         int
	manifests      map[int64]snapshot.Manifest // lazy parse cache
	trees          map[int64]*TreeNode         // lazy build cache
	manifestErrors map[int64]error             // remember parse failures
	filter         filter.Filter
	dimOlderThan   time.Duration // list-pane only; 0 = no dimming
	runningSet     map[string]bool
	keys           keyMap
	help           help.Model
	width, height  int
	focus          focusZone
	showHelp       bool
	footerNote     string // transient warning text
	selectedID     int64  // 0 = no selection (cancelled)
}

// NewPickerModel builds the initial state. The caller is responsible for
// fetching events and the running session set before constructing it.
func NewPickerModel(mode Mode, events []store.Event, running map[string]bool) PickerModel {
	return PickerModel{
		mode:           mode,
		events:         events,
		manifests:      make(map[int64]snapshot.Manifest, len(events)),
		trees:          make(map[int64]*TreeNode, len(events)),
		manifestErrors: make(map[int64]error),
		filter:         filter.Filter{DedupRunningServer: true},
		runningSet:     running,
		keys:           defaultKeys(),
		help:           help.New(),
		focus:          focusList,
	}
}

// Init satisfies tea.Model.
func (m PickerModel) Init() tea.Cmd { return nil }

// Update handles key events. Implementation grows across the next few tasks.
func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m PickerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Tab):
		if m.mode == ModeSnapshot {
			if m.focus == focusList {
				m.focus = focusTree
			} else {
				m.focus = focusList
			}
		}
		return m, nil
	}
	return m, nil
}

// View is implemented in view.go.
func (m PickerModel) View() string { return "" }

// Filter returns the current filter for caller-side BuildPlan.
func (m PickerModel) Filter() filter.Filter { return m.filter }

// SelectedID returns the event ID of the row the user confirmed, or 0 on cancel.
func (m PickerModel) SelectedID() int64 { return m.selectedID }

// SelectedManifest returns the parsed manifest of the selected event.
func (m PickerModel) SelectedManifest() snapshot.Manifest {
	if m.selectedID == 0 {
		return snapshot.Manifest{}
	}
	return m.manifests[m.selectedID]
}
```

Also add this import to the file (referenced in `handleKey`):

```go
import "charm.land/bubbles/v2/key"
```

(Merge the import block as needed.)

- [ ] **Step 2: Write the failing focus-switch test**

Create `internal/picker/model_test.go`:

```go
package picker_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/store"
)

func TestModel_TabSwitchesFocus_SnapshotMode(t *testing.T) {
	events := []store.Event{{ID: 1, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[]}`}}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)

	// Initial focus is list. After tab, should be tree.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	pm := updated.(picker.PickerModel)
	if pm.Focus() != picker.FocusTree {
		t.Errorf("after tab: focus=%v, want focusTree", pm.Focus())
	}

	// Tab again returns to list.
	updated, _ = pm.Update(tea.KeyMsg{Type: tea.KeyTab})
	pm = updated.(picker.PickerModel)
	if pm.Focus() != picker.FocusList {
		t.Errorf("after second tab: focus=%v, want focusList", pm.Focus())
	}
}
```

For this test to compile, `Focus()`, `FocusList`, `FocusTree` need to be exported. Add to `model.go`:

```go
// FocusZone aliases focusZone for tests.
type FocusZone = focusZone

const (
	FocusList = focusList
	FocusTree = focusTree
)

func (m PickerModel) Focus() FocusZone { return m.focus }
```

- [ ] **Step 3: Run the test to verify it fails initially or passes — confirm behavior**

Run: `go test ./internal/picker/ -run TestModel_TabSwitchesFocus -v`
Expected: PASS (handleKey already toggles focus). If it fails, the most likely cause is `tea.KeyTab` not matching `m.keys.Tab` — inspect the bubbletea v2 key-matching docs (charm.land switched to a typed key API; if `key.Matches` doesn't accept `tea.KeyMsg{Type: tea.KeyTab}`, use `tea.KeyMsg{Runes: []rune{'\t'}}` or the equivalent v2 idiom).

- [ ] **Step 4: Commit**

```bash
git add internal/picker/model.go internal/picker/model_test.go
git commit -m "feat(picker): PickerModel scaffold + tab focus toggle"
```

### Task 9: Lazy-parse manifest on cursor move

**Files:**
- Modify: `internal/picker/model.go`
- Modify: `internal/picker/model_test.go`

- [ ] **Step 1: Add cursor handling + parse-on-demand**

In `model.go`, add to `handleKey`:

```go
case key.Matches(msg, m.keys.Down):
	if m.cursor < len(m.events)-1 {
		m.cursor++
		m.ensureManifest()
	}
	return m, nil
case key.Matches(msg, m.keys.Up):
	if m.cursor > 0 {
		m.cursor--
		m.ensureManifest()
	}
	return m, nil
```

Add the helper:

```go
// ensureManifest parses + builds + decorates the tree for the cursor's event,
// caching the result. No-op on cache hit. Records parse errors in
// m.manifestErrors so View can render "(invalid manifest)".
func (m *PickerModel) ensureManifest() {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return
	}
	ev := m.events[m.cursor]
	if _, ok := m.manifests[ev.ID]; ok {
		return
	}
	if _, bad := m.manifestErrors[ev.ID]; bad {
		return
	}
	man, err := parseEventManifest(ev)
	if err != nil {
		m.manifestErrors[ev.ID] = err
		return
	}
	m.manifests[ev.ID] = man
	tree := BuildTree(man)
	FilterDecorate(tree, m.filter, m.runningSet)
	m.trees[ev.ID] = tree
}

func parseEventManifest(ev store.Event) (snapshot.Manifest, error) {
	var m snapshot.Manifest
	if ev.Kind == "snapshot" {
		if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
			return snapshot.Manifest{}, err
		}
		return m, nil
	}
	// Close events wrap the index inside an "index" key.
	var wrapped struct {
		Index json.RawMessage `json:"index"`
	}
	if err := json.Unmarshal([]byte(ev.ManifestJSON), &wrapped); err != nil {
		return snapshot.Manifest{}, err
	}
	if len(wrapped.Index) == 0 {
		return snapshot.Manifest{}, fmt.Errorf("close event has no index")
	}
	if err := json.Unmarshal(wrapped.Index, &m); err != nil {
		return snapshot.Manifest{}, err
	}
	return m, nil
}
```

Add to imports:

```go
import (
	"encoding/json"
	"fmt"
	// ... existing imports
)
```

Important: `handleKey` currently takes `(m PickerModel)`, not `(m *PickerModel)`. Switch its receiver — and `ensureManifest`'s caller pattern — so mutations stick. The Tea convention is to return the updated value-receiver model from `Update`; you can keep `Update` value-receiver and call `(&m).ensureManifest()` if you'd rather not change `handleKey`'s receiver. Either pattern is fine; pick one and apply it consistently. The test in Step 2 will catch breakage.

- [ ] **Step 2: Add the test**

```go
func TestModel_CursorMoveTriggersManifestParse(t *testing.T) {
	events := []store.Event{
		{ID: 1, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[{"name":"a","windows":[]}]}`},
		{ID: 2, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[{"name":"b","windows":[]}]}`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	pm := updated.(picker.PickerModel)
	if pm.Cursor() != 1 {
		t.Errorf("after Down: cursor=%d, want 1", pm.Cursor())
	}
	tree := pm.TreeFor(2)
	if tree == nil || len(tree.Children) != 1 || tree.Children[0].Label != "b (0w)" {
		t.Errorf("tree for event 2 not built correctly: %+v", tree)
	}
}
```

Export accessors in `model.go`:

```go
func (m PickerModel) Cursor() int                  { return m.cursor }
func (m PickerModel) TreeFor(id int64) *TreeNode   { return m.trees[id] }
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/picker/ -run TestModel_CursorMove -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/picker/model.go internal/picker/model_test.go
git commit -m "feat(picker): lazy manifest parse + tree cache on cursor move"
```

### Task 10: Filter toggles re-decorate the cached tree

**Files:**
- Modify: `internal/picker/model.go`
- Modify: `internal/picker/model_test.go`

- [ ] **Step 1: Handle the three toggles in handleKey**

Add cases to `handleKey`:

```go
case key.Matches(msg, m.keys.ToggleIdle):
	m.filter.SkipIdleShells = !m.filter.SkipIdleShells
	m.redecorate()
	return m, nil
case key.Matches(msg, m.keys.ToggleDedup):
	m.filter.DedupRunningServer = !m.filter.DedupRunningServer
	m.redecorate()
	return m, nil
case key.Matches(msg, m.keys.ToggleAge):
	if m.dimOlderThan == 0 {
		m.dimOlderThan = 24 * time.Hour
	} else {
		m.dimOlderThan = 0
	}
	return m, nil
```

Add the helper:

```go
// redecorate runs FilterDecorate over every cached tree with the current
// filter state. Cheap — O(nodes) and only over what's been viewed.
func (m *PickerModel) redecorate() {
	for id, tree := range m.trees {
		_ = id
		FilterDecorate(tree, m.filter, m.runningSet)
	}
}
```

- [ ] **Step 2: Add the test**

```go
func TestModel_ToggleIdleUpdatesCounter(t *testing.T) {
	events := []store.Event{
		{
			ID: 1, Kind: "snapshot",
			ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[
				{"index":0,"command":"fish","child_count":0},
				{"index":1,"command":"nvim","child_count":0}
			]}]}]}`,
		},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)
	// Force the parse for event 1 by sending a Down keypress at cursor==0 (no-op
	// for cursor, but we need to trigger ensureManifest at boot — easier: just
	// call the public Init path).
	// Instead, simulate the initial parse by sending a Window resize then Down
	// at the bottom — or expose a Bootstrap() for tests.

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pm := m2.(picker.PickerModel)
	pm.Bootstrap() // see Step 3 — adds an explicit init for tests + the real cmd wiring

	// Before toggle: 2 panes kept.
	if c := pm.CurrentCounts(); c.KeptPanes != 2 || c.SkippedPanes != 0 {
		t.Fatalf("before toggle: counts=%+v", c)
	}

	updated, _ := pm.Update(tea.KeyMsg{Runes: []rune{'s'}})
	pm = updated.(picker.PickerModel)

	// After "skip idle shells": fish (idle) skipped, nvim kept.
	if c := pm.CurrentCounts(); c.KeptPanes != 1 || c.SkippedPanes != 1 {
		t.Errorf("after toggle: counts=%+v", c)
	}
}
```

- [ ] **Step 3: Expose `Bootstrap()` and `CurrentCounts()`**

In `model.go`:

```go
// Bootstrap parses the manifest for the initial cursor position. Call once
// after construction; the cobra wiring does this so View has data on first
// render. Idempotent.
func (m *PickerModel) Bootstrap() {
	m.ensureManifest()
}

// CurrentCounts returns FilterDecorate's most recent output for the cursor's
// event. Used by the footer and by tests.
func (m PickerModel) CurrentCounts() Counts {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return Counts{}
	}
	tree := m.trees[m.events[m.cursor].ID]
	if tree == nil {
		return Counts{}
	}
	// FilterDecorate mutates in place; rerun to read counts cheaply.
	return FilterDecorate(tree, m.filter, m.runningSet)
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/picker/ -run TestModel_ToggleIdle -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/picker/model.go internal/picker/model_test.go
git commit -m "feat(picker): filter toggles re-decorate cached trees"
```

### Task 11: Enter confirms restore (records selectedID); parse errors block enter

**Files:**
- Modify: `internal/picker/model.go`
- Modify: `internal/picker/model_test.go`

- [ ] **Step 1: Handle Enter**

```go
case key.Matches(msg, m.keys.Enter):
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return m, nil
	}
	ev := m.events[m.cursor]
	if _, bad := m.manifestErrors[ev.ID]; bad {
		m.footerNote = "(invalid manifest — cannot restore)"
		return m, nil
	}
	if _, ok := m.manifests[ev.ID]; !ok {
		m.footerNote = "(manifest not loaded yet)"
		return m, nil
	}
	m.selectedID = ev.ID
	return m, tea.Quit
```

- [ ] **Step 2: Add tests**

```go
func TestModel_EnterRecordsSelectedID(t *testing.T) {
	events := []store.Event{
		{ID: 7, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[]}]}`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)
	m.Bootstrap()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := updated.(picker.PickerModel)
	if pm.SelectedID() != 7 {
		t.Errorf("selectedID=%d, want 7", pm.SelectedID())
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd, got nil")
	}
}

func TestModel_EnterBlockedOnParseError(t *testing.T) {
	events := []store.Event{
		{ID: 9, Kind: "snapshot", ManifestJSON: `{not json`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)
	m.Bootstrap()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := updated.(picker.PickerModel)
	if pm.SelectedID() != 0 {
		t.Errorf("selectedID=%d, want 0 (blocked)", pm.SelectedID())
	}
	if cmd != nil {
		t.Error("expected no quit cmd on parse error")
	}
	if pm.FooterNote() == "" {
		t.Error("expected footer warning to be set")
	}
}
```

Expose `FooterNote()`:

```go
func (m PickerModel) FooterNote() string { return m.footerNote }
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/picker/ -run TestModel_Enter -v`
Expected: both PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/picker/model.go internal/picker/model_test.go
git commit -m "feat(picker): enter confirms restore; parse errors block + warn"
```

---

## Phase 3: View — lipgloss styles and rendering

### Task 12: Catppuccin Mocha palette + base styles

**Files:**
- Create: `internal/picker/style.go`

- [ ] **Step 1: Write the palette and styles**

```go
package picker

import "charm.land/lipgloss/v2"

// Catppuccin Mocha (matches lazytmux's picker for visual continuity).
var (
	colBase     = lipgloss.Color("#1e1e2e")
	colSurface0 = lipgloss.Color("#313244")
	colSurface1 = lipgloss.Color("#45475a")
	colText     = lipgloss.Color("#cdd6f4")
	colSubtext  = lipgloss.Color("#a6adc8")
	colOverlay  = lipgloss.Color("#7f849c")
	colMauve    = lipgloss.Color("#cba6f7")
	colBlue     = lipgloss.Color("#89b4fa")
	colGreen    = lipgloss.Color("#a6e3a2")
	colYellow   = lipgloss.Color("#f9e2af")
	colRed      = lipgloss.Color("#f38ba8")
)

var (
	listFrame  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colSurface1).Padding(0, 1)
	treeFrame  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colSurface1).Padding(0, 1)

	rowActive   = lipgloss.NewStyle().Foreground(colBase).Background(colMauve).Bold(true)
	rowDefault  = lipgloss.NewStyle().Foreground(colText)
	rowDim      = lipgloss.NewStyle().Foreground(colOverlay)

	nodeKept    = lipgloss.NewStyle().Foreground(colText)
	nodeSkipped = lipgloss.NewStyle().Foreground(colOverlay).Strikethrough(true)
	skipReason  = lipgloss.NewStyle().Foreground(colSubtext).Italic(true)

	footerBar   = lipgloss.NewStyle().Foreground(colSubtext).Padding(0, 1)
	footerWarn  = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	footerOn    = lipgloss.NewStyle().Foreground(colGreen)
	footerOff   = lipgloss.NewStyle().Foreground(colOverlay)
)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/picker/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/style.go
git commit -m "feat(picker): Catppuccin Mocha palette + base lipgloss styles"
```

### Task 13: Render the list pane

**Files:**
- Create: `internal/picker/view.go`

- [ ] **Step 1: Implement View + renderList**

```go
package picker

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// View renders the full picker UI. Called by Bubble Tea after every Update.
func (m PickerModel) View() string {
	if m.showHelp {
		return m.help.View(m.keys)
	}

	listWidth, treeWidth := m.paneWidths()
	list := renderList(m, listWidth)

	if m.mode == ModeClose || m.width < 80 {
		// Close mode and narrow mode: list-only at this scale.
		return lipgloss.JoinVertical(lipgloss.Top, list, m.renderFooter(m.width))
	}

	tree := renderTree(m, treeWidth)
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree)
	return lipgloss.JoinVertical(lipgloss.Top, body, m.renderFooter(m.width))
}

// paneWidths splits the available width between list and tree. Returns
// (listWidth, 0) when narrow or close mode.
func (m PickerModel) paneWidths() (int, int) {
	if m.width < 80 || m.mode == ModeClose {
		return m.width, 0
	}
	listW := m.width / 3
	if listW < 28 {
		listW = 28
	}
	treeW := m.width - listW
	return listW, treeW
}

func renderList(m PickerModel, width int) string {
	var b strings.Builder
	if len(m.events) == 0 {
		b.WriteString(rowDim.Render("No snapshots yet — run `tmux-state save`."))
		return listFrame.Width(width).Render(b.String())
	}
	now := time.Now()
	for i, ev := range m.events {
		ts := time.UnixMilli(ev.Ts).Format("01-02 15:04")
		reason := shortReason(ev.Reason)
		line := fmt.Sprintf("#%d %s %s", ev.ID, ts, reason)
		dim := m.dimOlderThan > 0 && now.Sub(time.UnixMilli(ev.Ts)) > m.dimOlderThan
		style := rowDefault
		switch {
		case i == m.cursor:
			style = rowActive
		case dim:
			style = rowDim
		}
		b.WriteString(style.Width(width - 2).Render(line))
		b.WriteString("\n")
	}
	return listFrame.Width(width).Render(strings.TrimRight(b.String(), "\n"))
}

// shortReason truncates "hook:window-linked" to "wlink", "timer" to "timer",
// "keybinding" to "key". Best-effort; falls back to the first 8 chars.
func shortReason(r string) string {
	switch r {
	case "timer":
		return "timer"
	case "keybinding":
		return "key"
	case "hook:window-linked":
		return "wlink"
	case "hook:session-created":
		return "screat"
	case "hook:client-detached":
		return "cdet"
	}
	if len(r) > 8 {
		return r[:8]
	}
	return r
}
```

- [ ] **Step 2: Compile**

Run: `go build ./internal/picker/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/view.go
git commit -m "feat(picker): render list pane with cursor highlight + age dim"
```

### Task 14: Render the tree pane

**Files:**
- Modify: `internal/picker/view.go`

- [ ] **Step 1: Add renderTree**

```go
func renderTree(m PickerModel, width int) string {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return treeFrame.Width(width).Render("")
	}
	ev := m.events[m.cursor]
	if err, bad := m.manifestErrors[ev.ID]; bad {
		return treeFrame.Width(width).Render(footerWarn.Render("(invalid manifest)") + "\n" + skipReason.Render(err.Error()))
	}
	tree := m.trees[ev.ID]
	if tree == nil {
		return treeFrame.Width(width).Render(rowDim.Render("(loading...)"))
	}
	if len(tree.Children) == 0 {
		return treeFrame.Width(width).Render(rowDim.Render("(empty snapshot)"))
	}

	var b strings.Builder
	header := fmt.Sprintf("Contents (#%d)", ev.ID)
	b.WriteString(lipgloss.NewStyle().Foreground(colBlue).Bold(true).Render(header))
	b.WriteString("\n")
	for _, sess := range tree.Children {
		writeNode(&b, sess, 0)
	}
	return treeFrame.Width(width).Render(strings.TrimRight(b.String(), "\n"))
}

func writeNode(b *strings.Builder, n *TreeNode, depth int) {
	indent := strings.Repeat("  ", depth)
	bullet := "•"
	if len(n.Children) > 0 {
		if n.Expanded {
			bullet = "▾"
		} else {
			bullet = "▸"
		}
	}
	label := n.Label
	style := nodeKept
	if n.Skipped {
		style = nodeSkipped
		if n.SkipReason != "" {
			label = label + "  " + skipReason.Render("("+n.SkipReason+")")
		}
	}
	b.WriteString(fmt.Sprintf("%s%s %s\n", indent, bullet, style.Render(label)))
	if n.Expanded {
		for _, c := range n.Children {
			writeNode(b, c, depth+1)
		}
	}
}
```

- [ ] **Step 2: Compile**

Run: `go build ./internal/picker/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/view.go
git commit -m "feat(picker): render tree pane with skip styling + indent bullets"
```

### Task 15: Render the footer with toggle indicators and counter

**Files:**
- Modify: `internal/picker/view.go`

- [ ] **Step 1: Add renderFooter as a method on the model (needs filter state)**

```go
func (m PickerModel) renderFooter(width int) string {
	on := func(b bool, label string) string {
		if b {
			return footerOn.Render("[" + label + ":●]")
		}
		return footerOff.Render("[" + label + ":◯]")
	}
	c := m.CurrentCounts()
	counter := fmt.Sprintf("%d panes / %d skipped", c.KeptPanes, c.SkippedPanes)
	parts := []string{
		on(m.filter.SkipIdleShells, "skip idle"),
		on(m.filter.DedupRunningServer, "dedup running"),
		on(m.dimOlderThan > 0, "age≤24h"),
		"  " + counter,
		"  ↵ restore",
	}
	line := strings.Join(parts, "  ")
	if m.footerNote != "" {
		line = footerWarn.Render(m.footerNote) + "  " + line
	}
	return footerBar.Width(width).Render(line)
}
```

- [ ] **Step 2: Compile**

Run: `go build ./internal/picker/`
Expected: exits 0.

- [ ] **Step 3: Add a snapshot-style test that exercises View end-to-end**

```go
func TestModel_ViewRendersWithoutPanic(t *testing.T) {
	events := []store.Event{
		{ID: 1, Ts: time.Now().UnixMilli(), Kind: "snapshot",
			ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[{"index":0,"command":"fish"}]}]}]}`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)
	m.Bootstrap()
	// Simulate a sane terminal size.
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pm := upd.(picker.PickerModel)
	out := pm.View()
	if out == "" {
		t.Fatal("View() returned empty string")
	}
	if !strings.Contains(out, "s (1w)") {
		t.Errorf("expected session label in view, got:\n%s", out)
	}
}
```

Add `"time"` and `"strings"` to model_test.go imports if not already present.

- [ ] **Step 4: Run**

Run: `go test ./internal/picker/ -run TestModel_View -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/picker/view.go internal/picker/model_test.go
git commit -m "feat(picker): render footer with toggles, counter, transient warning"
```

### Task 16: Tree-pane key handling — expand/collapse

**Files:**
- Modify: `internal/picker/model.go`
- Modify: `internal/picker/model_test.go`

- [ ] **Step 1: Track cursor inside the tree pane**

Add to `PickerModel`:

```go
treeCursor int // index into the flattened visible-node list
```

Add helper:

```go
// visibleNodes flattens the current tree honoring Expanded.
func (m PickerModel) visibleNodes() []*TreeNode {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return nil
	}
	tree := m.trees[m.events[m.cursor].ID]
	if tree == nil {
		return nil
	}
	var out []*TreeNode
	var walk func(n *TreeNode)
	walk = func(n *TreeNode) {
		out = append(out, n)
		if n.Expanded {
			for _, c := range n.Children {
				walk(c)
			}
		}
	}
	for _, sess := range tree.Children {
		walk(sess)
	}
	return out
}
```

- [ ] **Step 2: Handle Left/Right when tree is focused**

In `handleKey`, branch on focus before the global keys:

```go
if m.mode == ModeSnapshot && m.focus == focusTree {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.treeCursor > 0 {
			m.treeCursor--
		}
		return m, nil
	case key.Matches(msg, m.keys.Down):
		nodes := m.visibleNodes()
		if m.treeCursor < len(nodes)-1 {
			m.treeCursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.Right):
		nodes := m.visibleNodes()
		if m.treeCursor < len(nodes) && len(nodes[m.treeCursor].Children) > 0 {
			nodes[m.treeCursor].Expanded = true
		}
		return m, nil
	case key.Matches(msg, m.keys.Left):
		nodes := m.visibleNodes()
		if m.treeCursor < len(nodes) {
			n := nodes[m.treeCursor]
			if n.Expanded && len(n.Children) > 0 {
				n.Expanded = false
			}
		}
		return m, nil
	}
}
```

- [ ] **Step 3: Reset treeCursor on list-pane cursor moves**

When `m.cursor` changes (in the existing Up/Down handlers for list focus), set `m.treeCursor = 0` so the new tree starts fresh.

- [ ] **Step 4: Add a test**

```go
func TestModel_TreeRightExpands_LeftCollapses(t *testing.T) {
	events := []store.Event{{
		ID: 1, Kind: "snapshot",
		ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[
			{"index":0,"command":"fish"}
		]}]}]}`,
	}}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil)
	m.Bootstrap()
	// Move focus to tree.
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	pm := upd.(picker.PickerModel)
	// Default: session expanded, window expanded, pane collapsed.
	// Cursor is at the session (index 0). Press Left to collapse.
	upd, _ = pm.Update(tea.KeyMsg{Type: tea.KeyLeft})
	pm = upd.(picker.PickerModel)
	nodes := pm.VisibleNodes() // exported wrapper
	if len(nodes) != 1 {
		t.Errorf("after collapse: visible=%d, want 1 (just the session)", len(nodes))
	}
	// Press Right to expand again.
	upd, _ = pm.Update(tea.KeyMsg{Type: tea.KeyRight})
	pm = upd.(picker.PickerModel)
	nodes = pm.VisibleNodes()
	if len(nodes) < 2 {
		t.Errorf("after expand: visible=%d, want >=2", len(nodes))
	}
}
```

Export `VisibleNodes`:

```go
func (m PickerModel) VisibleNodes() []*TreeNode { return m.visibleNodes() }
```

- [ ] **Step 5: Run**

Run: `go test ./internal/picker/ -run TestModel_Tree -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/picker/model.go internal/picker/model_test.go
git commit -m "feat(picker): tree-pane cursor + expand/collapse on left/right"
```

### Task 17: Help overlay toggle (?)

**Files:**
- Modify: `internal/picker/model.go`
- Modify: `internal/picker/view.go`

- [ ] **Step 1: Handle `?`**

In `handleKey`:

```go
case key.Matches(msg, m.keys.Help):
	m.showHelp = !m.showHelp
	return m, nil
```

`View()` already returns `m.help.View(m.keys)` when `m.showHelp == true`.

- [ ] **Step 2: Compile**

Run: `go build ./internal/picker/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/model.go
git commit -m "feat(picker): ? toggles help overlay"
```

---

## Phase 4: Wire up the cobra command

### Task 18: Replace newPickCmd's fzf path with the Tea program

**Files:**
- Modify: `cmd/tmux-state/main.go`

- [ ] **Step 1: Read the existing newPickCmd to understand the surrounding helpers**

Run: `rg -n "func newPickCmd|withStore|resolveBuildOptions" cmd/tmux-state/main.go`

Confirm `withStore`, `resolveBuildOptions`, and `restore.BuildPlan`/`restore.Apply` are imported and used as expected. No code edits in this step — just orientation.

- [ ] **Step 2: Rewrite newPickCmd**

Replace the entire body of `newPickCmd` with:

```go
func newPickCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "pick",
		Short: "Open an interactive picker over events",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				opts := store.ListOpts{Limit: 50}
				mode := picker.ModeSnapshot
				switch kind {
				case "snapshot":
					opts.Kinds = []string{"snapshot"}
				case "close":
					opts.ExcludeKinds = []string{"snapshot"}
					mode = picker.ModeClose
				}
				evs, err := db.ListEvents(ctx, opts)
				if err != nil {
					return err
				}

				t := tmux.NewClient("tmux")
				runningSet := map[string]bool{}
				if sessions, err := t.ListSessions(ctx); err == nil {
					for _, s := range sessions {
						runningSet[s.Name] = true
					}
				}

				m := picker.NewPickerModel(mode, evs, runningSet)
				m.Bootstrap()

				prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
				finalModel, err := prog.Run()
				if err != nil {
					return fmt.Errorf("picker: %w", err)
				}
				final, ok := finalModel.(picker.PickerModel)
				if !ok || final.SelectedID() == 0 {
					return nil // cancelled
				}

				manifest := final.SelectedManifest()
				buildOpts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
				plan := restore.BuildPlan(manifest, final.Filter(), nil, buildOpts)
				return restore.Apply(ctx, t, plan)
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "snapshot", "snapshot|close")
	return cmd
}
```

Add the new imports to `cmd/tmux-state/main.go`:

```go
import (
	// ... existing
	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/tmux-state/internal/picker"
)
```

Remove the now-unused `json` and `strconv` imports if they're only referenced from the deleted fzf branch — `goimports` will handle this.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add cmd/tmux-state/main.go
git commit -m "feat(cmd): wire bubbletea picker into pick subcommand"
```

### Task 19: Delete the fzf wrapper

**Files:**
- Delete: `internal/picker/picker.go`

- [ ] **Step 1: Delete the old file**

```bash
gtrash put internal/picker/picker.go
```

If the existing `internal/picker/picker_test.go` references `picker.Pick` or `picker.FormatRow`, delete those tests too — the spec scrapped the fzf path entirely. If the file is empty afterward, delete it.

- [ ] **Step 2: Build and run all tests**

Run: `go build ./...`
Expected: exits 0.

Run: `go test ./...`
Expected: PASS across the repo. Any leftover references to `picker.Pick` or `picker.FormatRow` outside the deleted file are bugs — fix them.

- [ ] **Step 3: Commit**

```bash
git add internal/picker/picker.go internal/picker/picker_test.go
git commit -m "refactor(picker): drop fzf wrapper, replaced by bubbletea TUI"
```

---

## Phase 5: Polish and integration

### Task 20: Smoke-test the binary against the real tmux server

**Files:** None modified.

- [ ] **Step 1: Build the binary**

Run: `nix build .` (matches the existing repo convention)
Expected: `./result/bin/tmux-state` is produced.

- [ ] **Step 2: Test the snapshot picker manually**

Inside a running tmux session:

```bash
./result/bin/tmux-state pick --kind=snapshot
```

Confirm:
- Two panes render (assuming terminal width ≥ 80).
- Arrow keys move the list cursor; the right pane updates.
- `s`, `d`, `a` toggle the footer indicators on/off.
- The counter changes when `s` toggles (assuming there's at least one idle shell pane in the snapshot).
- `tab` switches focus to the tree; `→` and `←` expand/collapse a node.
- `enter` restores; `esc` cancels cleanly (popup closes, no error).

If the tree pane doesn't update on cursor move, `ensureManifest` isn't running — check the cursor handlers in Task 9.

- [ ] **Step 3: Test the close picker**

```bash
./result/bin/tmux-state pick --kind=close
```

Confirm: list-only render, no right pane, no `s`/`d`/`a` indicators in the footer.

- [ ] **Step 4: Test the narrow-width fallback**

Resize the terminal to < 80 columns and run `pick --kind=snapshot`. Confirm: list-only render. Pressing enter on a row should open a modal-style tree overlay; esc returns to the list.

If this isn't working, the View's `m.width < 80` branch is hiding the tree but not opening the modal — Task 13's narrow-mode branch needs revisiting. Open a follow-up task rather than blocking the merge; the primary use case is wide terminals.

- [ ] **Step 5: No commit needed unless you found a fix during testing**

### Task 21: Update the flake vendorHash if needed

**Files:**
- Modify: `flake.nix` (if `nix build` fails on hash mismatch)

- [ ] **Step 1: Try the build**

Run: `nix build .`

If it fails with "hash mismatch", copy the suggested hash from the error.

- [ ] **Step 2: Update flake.nix**

Edit the `vendorHash = "sha256-…"` line in `flake.nix` to the new value reported by Nix.

- [ ] **Step 3: Rebuild**

Run: `nix build .`
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add flake.nix
git commit -m "chore(flake): bump vendorHash after bubbletea deps"
```

### Task 22: README update

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the `pick` section**

Find the existing description of the `pick` subcommand and replace it with:

```markdown
### `pick`

Open an interactive picker over snapshot or close events. The picker is a Bubble Tea TUI that shows each snapshot's full session → window → pane tree before you restore it, and exposes the smart-restore filter as live footer toggles.

- `--kind=snapshot` (default) — two-pane view (snapshots on the left, tree on the right). Toggle `s` to skip idle shells, `d` to dedup sessions already running, `a` to dim snapshots older than 24h.
- `--kind=close` — list-only view of close events, used by `prefix + U` in lazytmux.

Tab switches focus between panes. `?` shows the full keymap. `enter` restores; `esc` cancels.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): describe bubbletea picker tree + filter toggles"
```

---

## Verification checklist (final pass)

- [ ] `go test ./...` passes
- [ ] `nix build .` succeeds
- [ ] `nix flake check` passes
- [ ] Manual: `tmux-state pick --kind=snapshot` renders two panes, toggles work
- [ ] Manual: `tmux-state pick --kind=close` renders list-only
- [ ] Manual: `tmux-state pick` (no kind flag) defaults to snapshot mode
- [ ] Manual: `esc` cancels and the lazytmux popup closes cleanly
- [ ] Manual: pressing enter on a snapshot actually restores it (verify with `tmux ls` after)

---

## Open implementation choices (intentional)

These are micro-decisions the implementer can make during Phase 2/3 — calling them out so they're not surprises.

1. **`runningSet` refresh.** Captured once at boot. If the popup is left open for minutes and a session is detached, the toggle's effect will be stale. Not worth wiring a ticker for v1; revisit if anyone notices.
2. **Tree expansion state across cursor moves.** Reset to default (sessions+windows expanded, panes collapsed) on every cursor move. Persisting per-event would feel more polished but adds state for marginal benefit; defer.
3. **`teatest` dependency.** Not added in this plan. If transcript-style tests start to feel verbose, add `charm.land/x/teatest` in a follow-up.
4. **`tea.WithAltScreen()`.** The cobra wiring uses it so the picker takes over the screen cleanly inside `tmux display-popup -E`. If users report flicker on entry/exit, drop the alt-screen flag — popup mode renders fine without it.
