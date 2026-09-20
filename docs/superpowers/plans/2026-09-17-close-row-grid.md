# Close List Column Grid Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the close list's ad-hoc row layout with an explicit column grid whose columns auto-size to present content, shed in a declared order, and can carry typed fields sourced from tmux window options.

**Architecture:** Three layers with one seam. `internal/config` owns the column spec (option name → role → width) parsed from a `@remux_columns` tmux option. `internal/tmux` gains one global-option reader shared by the picker and the save path. `internal/picker/rowgrid.go` is a pure layout engine that switches on column role and never on an option name.

**Tech Stack:** Go 1.x, `charm.land/lipgloss/v2`, `github.com/charmbracelet/x/ansi`. Tests are stdlib `testing`, table-driven.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-09-17-close-row-grid-design.md`. Read it before Task 1.
- `internal/picker` must never contain an `@issue_*` or `@pr_*` string literal. Task 6 enforces this with a test.
- `Config.DecorationOptions` keeps its existing non-empty default (`@crew_name`, `@crew_color`). Capture is the **union** of that default and the column spec's options. Deriving it from columns alone silently breaks decoration restore.
- Every rendered close row is exactly `innerWidth` cells. Existing tests `TestRenderCloseList_NeverOverflowsFrame` and `TestRenderClosePreview_NeverOverflowsFrame` guard this and must stay green.
- Widths are measured with `lipgloss.Width` / `ansi.StringWidth` (display cells), never `len()` — window names carry double-width runes and Nerd Font glyphs.
- All commands run inside the devshell: prefix with `nix develop -c`.
- Commit messages: conventional commits, `(#142)` in the subject.

---

### Task 1: Column spec types and parser

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/columns.go`
- Test: `internal/config/columns_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.ColumnRole` (`RoleID`, `RoleBadge`, `RoleText`), `config.DecorationColumn{Option string; Role ColumnRole; Max int}`, `config.ParseDecorationColumns(spec string) []DecorationColumn`, field `Config.DecorationColumns []DecorationColumn`, method `func (c Config) CaptureOptions() []string`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/columns_test.go`:

```go
package config

import (
	"reflect"
	"testing"
)

func TestParseDecorationColumns(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec string
		want []DecorationColumn
	}{
		{"empty", "", nil},
		{
			"two columns",
			"@issue_id:id:10,@pr_number:badge:6",
			[]DecorationColumn{
				{Option: "@issue_id", Role: RoleID, Max: 10},
				{Option: "@pr_number", Role: RoleBadge, Max: 6},
			},
		},
		{
			"text role",
			"@issue_title:text:0",
			[]DecorationColumn{{Option: "@issue_title", Role: RoleText}},
		},
		// A bad entry costs that column, not the whole list: an unparsable
		// spec must not blank the picker.
		{
			"malformed entry skipped",
			"@issue_id:id:10,garbage,@pr_number:badge:6",
			[]DecorationColumn{
				{Option: "@issue_id", Role: RoleID, Max: 10},
				{Option: "@pr_number", Role: RoleBadge, Max: 6},
			},
		},
		{"unknown role skipped", "@x:bogus:4", nil},
		{"non-numeric max skipped", "@x:id:wide", nil},
		{"missing @ prefix skipped", "issue_id:id:10", nil},
		{"whitespace tolerated", " @issue_id : id : 10 ", []DecorationColumn{
			{Option: "@issue_id", Role: RoleID, Max: 10},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDecorationColumns(tc.spec)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseDecorationColumns(%q) = %#v, want %#v", tc.spec, got, tc.want)
			}
		})
	}
}

// RoleText is the flex column; Max is meaningless for it and must not be
// carried, or a later width calculation could silently cap the title.
func TestParseDecorationColumns_TextIgnoresMax(t *testing.T) {
	got := ParseDecorationColumns("@issue_title:text:8")
	if len(got) != 1 || got[0].Max != 0 {
		t.Errorf("got %#v, want a single column with Max 0", got)
	}
}

// Capture must be the union: dropping the restore defaults would stop
// decoration surviving a restart, which is what DecorationOptions exists for.
func TestCaptureOptionsIsUnion(t *testing.T) {
	c := Default()
	c.DecorationColumns = []DecorationColumn{
		{Option: "@issue_id", Role: RoleID, Max: 10},
		{Option: "@crew_name", Role: RoleText}, // already a default; must not duplicate
	}
	got := c.CaptureOptions()
	want := []string{"@crew_name", "@crew_color", "@issue_id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CaptureOptions() = %v, want %v", got, want)
	}
}

func TestDefaultHasNoDecorationColumns(t *testing.T) {
	if got := Default().DecorationColumns; got != nil {
		t.Errorf("Default().DecorationColumns = %#v, want nil — remux must not ship another tool's schema", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop -c go test ./internal/config/ -run 'TestParseDecorationColumns|TestCaptureOptions|TestDefaultHasNoDecorationColumns'`
Expected: FAIL — compile error, `undefined: DecorationColumn`, `undefined: ParseDecorationColumns`, `undefined: RoleID`.

- [ ] **Step 3: Write the implementation**

Create `internal/config/columns.go`:

```go
package config

import (
	"strconv"
	"strings"
)

// ColumnRole is how the close list renders a decoration column. The picker
// switches on the role and never on the option name, so a new field is a
// config change rather than a code change.
type ColumnRole int

// Column roles.
const (
	RoleID    ColumnRole = iota // compact identity, left-anchored ("ENG-8224", "#56")
	RoleBadge                   // short marker, right of the flex column ("#3511")
	RoleText                    // prose; the flex column
)

// DecorationColumn declares one close-list column sourced from a tmux window
// option. Max caps RoleID and RoleBadge; RoleText is the flex column and
// ignores it.
type DecorationColumn struct {
	Option string
	Role   ColumnRole
	Max    int
}

var columnRoles = map[string]ColumnRole{
	"id":    RoleID,
	"badge": RoleBadge,
	"text":  RoleText,
}

// ParseDecorationColumns reads the `@remux_columns` spec: comma-separated
// `option:role:max` triples. An entry that does not parse is skipped — an
// unusable column should cost that column, not blank the picker.
func ParseDecorationColumns(spec string) []DecorationColumn {
	var cols []DecorationColumn
	for _, entry := range strings.Split(spec, ",") {
		parts := strings.Split(entry, ":")
		if len(parts) != 3 {
			continue
		}
		option := strings.TrimSpace(parts[0])
		role, ok := columnRoles[strings.TrimSpace(parts[1])]
		if !ok || !strings.HasPrefix(option, "@") {
			continue
		}
		max, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil || max < 0 {
			continue
		}
		if role == RoleText {
			max = 0
		}
		cols = append(cols, DecorationColumn{Option: option, Role: role, Max: max})
	}
	return cols
}

// CaptureOptions is the allow-list of window options to snapshot: the restore
// allow-list plus every option a column reads. It must stay a union —
// DecorationOptions carries options captured purely so restore can replay
// them, which no column renders.
func (c Config) CaptureOptions() []string {
	seen := make(map[string]bool, len(c.DecorationOptions)+len(c.DecorationColumns))
	out := make([]string, 0, len(c.DecorationOptions)+len(c.DecorationColumns))
	for _, o := range c.DecorationOptions {
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	for _, col := range c.DecorationColumns {
		if !seen[col.Option] {
			seen[col.Option] = true
			out = append(out, col.Option)
		}
	}
	return out
}
```

In `internal/config/config.go`, add the field to the `Config` struct immediately after `DecorationOptions`:

```go
	// DecorationColumns declares which captured options the close list renders
	// as columns, and how. Empty by default: tmux-remux is a published plugin
	// and must not ship another tool's option schema. Set via @remux_columns.
	DecorationColumns []DecorationColumn
```

Leave `Default()` untouched — `DecorationColumns` defaults to nil, which is what `TestDefaultHasNoDecorationColumns` asserts.

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop -c go test ./internal/config/`
Expected: PASS (all config tests, including the pre-existing `TestDefaultDecorationOptions`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/columns.go internal/config/columns_test.go internal/config/config.go
git commit -m "feat(config): decoration column spec and @remux_columns parser (#142)"
```

---

### Task 2: Shared tmux global-option reader

`internal/picker/theme.go` already shells out to `tmux show -g` and parses the result. The save path now needs the same data to resolve `@remux_columns`. Move the one implementation into `internal/tmux` rather than writing a second parser.

**Files:**
- Create: `internal/tmux/options.go`
- Test: `internal/tmux/options_test.go`
- Modify: `internal/picker/theme.go:81-97` (delete `readTmuxOpts`, call the shared one)
- Modify: `cmd/tmux-remux/main.go:168`, `cmd/tmux-remux/main.go:983`

**Interfaces:**
- Consumes: `config.ParseDecorationColumns`, `Config.CaptureOptions` from Task 1.
- Produces: `tmux.GlobalOptions(binary string) map[string]string`, `tmux.ParseOptionLines(out string) map[string]string`.

- [ ] **Step 1: Write the failing test**

Create `internal/tmux/options_test.go`:

```go
package tmux

import "testing"

func TestParseOptionLines(t *testing.T) {
	out := "@thm_bg \"#1e1e2e\"\n" +
		"@remux_columns \"@issue_id:id:10,@pr_number:badge:6\"\n" +
		"status on\n" +
		"malformed\n" +
		"@empty \n"
	got := ParseOptionLines(out)

	for _, tc := range []struct{ key, want string }{
		{"@thm_bg", "#1e1e2e"},
		{"@remux_columns", "@issue_id:id:10,@pr_number:badge:6"},
		{"status", "on"},
	} {
		if got[tc.key] != tc.want {
			t.Errorf("ParseOptionLines()[%q] = %q, want %q", tc.key, got[tc.key], tc.want)
		}
	}
	if _, ok := got["malformed"]; ok {
		t.Error("a line with no separator must not produce a key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop -c go test ./internal/tmux/ -run TestParseOptionLines`
Expected: FAIL — `undefined: ParseOptionLines`.

- [ ] **Step 3: Write the implementation**

Create `internal/tmux/options.go`. The body is lifted verbatim from the current `internal/picker/theme.go:81-97` so behaviour is unchanged:

```go
package tmux

import (
	"os/exec"
	"strings"
)

// GlobalOptions returns `tmux show -g` as a map. Best effort: a failure to
// reach the server yields nil, and every caller treats a missing key as
// "unset" anyway.
func GlobalOptions(binary string) map[string]string {
	out, err := exec.Command(binary, "show", "-g").Output()
	if err != nil {
		return nil
	}
	return ParseOptionLines(string(out))
}

// ParseOptionLines parses `tmux show -g` output. Values are unquoted; a line
// with no space separator is skipped.
func ParseOptionLines(out string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		i := strings.IndexByte(line, ' ')
		if i <= 0 {
			continue
		}
		v := strings.TrimRight(line[i+1:], " \t\r")
		v = strings.Trim(v, "\"")
		m[line[:i]] = v
	}
	return m
}
```

In `internal/picker/theme.go`: delete the whole `readTmuxOpts` function (lines 81-97) and its now-unused `os/exec` and `strings` imports, then change the call site at line 25:

```go
		tmuxOpts: tmux.GlobalOptions("tmux"),
```

adding `"github.com/noamsto/tmux-remux/internal/tmux"` to that file's imports.

In `cmd/tmux-remux/main.go`, change `loadConfig` at line 983 to resolve the column spec:

```go
func loadConfig() config.Config {
	cfg := config.Default()
	cfg.DecorationColumns = config.ParseDecorationColumns(tmux.GlobalOptions("tmux")["@remux_columns"])
	return cfg
}
```

and line 168 to capture the union:

```go
		t := tmux.NewClient("tmux", cfg.CaptureOptions()...)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/tmux/ ./internal/picker/ ./cmd/... && nix develop -c go build ./...`
Expected: PASS, build clean. The picker's existing theme tests cover the moved parser's behaviour.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/options.go internal/tmux/options_test.go internal/picker/theme.go cmd/tmux-remux/main.go
git commit -m "refactor(tmux): share the global-option reader, resolve @remux_columns (#142)"
```

---

### Task 3: The grid engine

Pure layout: no tmux, no store, no frames. This is the task that carries the behaviour, so it gets the most tests.

**Files:**
- Create: `internal/picker/rowgrid.go`
- Test: `internal/picker/rowgrid_test.go`

**Interfaces:**
- Consumes: nothing (deliberately — this file imports only `strings`, `lipgloss`, and `ansi`).
- Produces: `closeCells` struct, `newCloseGrid(rows []closeCells, innerWidth int) closeGrid`, `func (g closeGrid) render(c closeCells) (line string, cmdStart, cmdEnd int)`.

**Column order and behaviour** (from the spec):

```
[glyph] [cwd] [id] [title ···flex··· ] [badge] [cmd] [→ target] [age]
  1cell  auto  auto        flex          auto   auto     auto    fixed
```

`auto` = widest value present across `rows`, and **0 when no row has one** — the column and its separator vanish. Shed order when a row will not fit: drop cwd, title→floor(12), drop badge, drop cmd, clip title below floor, clip target from the left. Age never sheds. The cwd column caps at `min(innerWidth/4, 24)` and is fitted with `fitCwd`.

- [ ] **Step 1: Write the failing test**

Create `internal/picker/rowgrid_test.go`:

```go
package picker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func sampleRows() []closeCells {
	return []closeCells{
		{glyph: "▣", cwd: "noamsto/hookyard", id: "ENG-8224", title: "effort lever",
			badge: "#3511", cmd: "claude", target: "→ mono:2", age: "34m"},
		{glyph: "▣", id: "", title: "dispatch hookyard issues",
			cmd: "claude", target: "→ hookyard:1", age: "22m"},
		{glyph: "□", cwd: "noamsto/dispatcher", id: "#56", title: "doctor report",
			cmd: "codex", target: "→ hookyard:2", age: "27m"},
	}
}

// Every rendered line must be exactly innerWidth: the frame pads short content
// but does not clip overflow, so a long line pushes the border out and desyncs
// the sibling panes.
func TestCloseGrid_EveryLineIsExactlyInnerWidth(t *testing.T) {
	rows := sampleRows()
	for w := 20; w <= 160; w++ {
		g := newCloseGrid(rows, w)
		for i, c := range rows {
			line, _, _ := g.render(c)
			if got := ansi.StringWidth(line); got != w {
				t.Fatalf("width %d row %d: line is %d cells:\n%q", w, i, got, line)
			}
		}
	}
}

// A column no row has a value for must occupy zero cells INCLUDING its
// separator. This is the fix for the reserved-but-empty cwd gutter.
func TestCloseGrid_AbsentColumnVanishes(t *testing.T) {
	rows := []closeCells{
		{glyph: "▣", title: "alpha", target: "→ a:1", age: "1m"},
		{glyph: "▣", title: "beta", target: "→ b:2", age: "2m"},
	}
	g := newCloseGrid(rows, 60)
	line, _, _ := g.render(rows[0])
	// No cwd, id, badge or cmd anywhere in the list, so the glyph is followed
	// by exactly one separator and then the title.
	if !strings.HasPrefix(ansi.Strip(line), "▣ alpha") {
		t.Errorf("absent columns still reserve space: %q", ansi.Strip(line))
	}
}

// Columns line up across rows, including a row that is missing a value for a
// column another row fills.
func TestCloseGrid_ColumnsAlignAcrossRows(t *testing.T) {
	rows := sampleRows()
	g := newCloseGrid(rows, 120)
	var at []int
	for _, c := range rows {
		line, _, _ := g.render(c)
		at = append(at, strings.Index(ansi.Strip(line), "→"))
	}
	for i := 1; i < len(at); i++ {
		if at[i] != at[0] {
			t.Errorf("arrow column drifts: row 0 at %d, row %d at %d", at[0], i, at[i])
		}
	}
}

// The declared shed order, checked by narrowing until each field disappears.
// The title must lose cells before the command is discarded — the old ladder
// had this inverted and dropped "claude" while keeping "…ocal".
func TestCloseGrid_ShedOrder(t *testing.T) {
	rows := sampleRows()
	gone := func(w int, needle string) bool {
		g := newCloseGrid(rows, w)
		line, _, _ := g.render(rows[0])
		return !strings.Contains(ansi.Strip(line), needle)
	}
	cwdGone, badgeGone, cmdGone := -1, -1, -1
	for w := 160; w >= 20; w-- {
		if cwdGone < 0 && gone(w, "hookyard") {
			cwdGone = w
		}
		if badgeGone < 0 && gone(w, "#3511") {
			badgeGone = w
		}
		if cmdGone < 0 && gone(w, "claude") {
			cmdGone = w
		}
	}
	if !(cwdGone > badgeGone && badgeGone > cmdGone) {
		t.Errorf("shed order wrong: cwd gone at %d, badge at %d, cmd at %d — want cwd first, then badge, then cmd",
			cwdGone, badgeGone, cmdGone)
	}
}

// Age is three cells and is the list's sort key; it must survive every width.
func TestCloseGrid_AgeNeverSheds(t *testing.T) {
	rows := sampleRows()
	for w := 20; w <= 160; w++ {
		g := newCloseGrid(rows, w)
		line, _, _ := g.render(rows[0])
		if !strings.Contains(ansi.Strip(line), "34m") {
			t.Fatalf("width %d: age dropped:\n%q", w, ansi.Strip(line))
		}
	}
}

// The target is clipped from the left, since its tail (":2") discriminates.
func TestCloseGrid_TargetClipsFromTheLeft(t *testing.T) {
	rows := []closeCells{{glyph: "▣", title: "t", target: "→ a-very-long-session-name:12", age: "5m"}}
	g := newCloseGrid(rows, 26)
	line, _, _ := g.render(rows[0])
	if !strings.Contains(ansi.Strip(line), ":12") {
		t.Errorf("target lost its discriminating tail: %q", ansi.Strip(line))
	}
}

// The command's cell range is reported so the caller can recolour it with
// lipgloss.StyleRanges. Offsets are display cells, not bytes or runes.
func TestCloseGrid_ReportsCommandRange(t *testing.T) {
	rows := sampleRows()
	g := newCloseGrid(rows, 120)
	line, start, end := g.render(rows[0])
	if start < 0 || end <= start {
		t.Fatalf("no command range reported: (%d, %d)", start, end)
	}
	stripped := []rune(ansi.Strip(line))
	if got := strings.TrimSpace(string(stripped[start:end])); got != "claude" {
		t.Errorf("command range covers %q, want %q", got, "claude")
	}
}

// A row whose command was shed must report no range, or StyleRanges would
// recolour whatever slid into those cells. Swept across widths rather than
// probed at one: a single width that happens to keep the command would make
// this assert nothing, so the sweep also proves the shed case occurred.
func TestCloseGrid_CommandRangeOnlyWhenCommandRendered(t *testing.T) {
	rows := sampleRows()
	sawShed := false
	for w := 20; w <= 160; w++ {
		g := newCloseGrid(rows, w)
		line, start, _ := g.render(rows[0])
		if strings.Contains(ansi.Strip(line), "claude") {
			continue
		}
		sawShed = true
		if start >= 0 {
			t.Errorf("width %d: range %d reported for a shed command", w, start)
		}
	}
	if !sawShed {
		t.Fatal("command never shed across the sweep — widen it so this test has teeth")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop -c go test ./internal/picker/ -run TestCloseGrid`
Expected: FAIL — compile error, `undefined: closeCells`, `undefined: newCloseGrid`.

- [ ] **Step 3: Write the implementation**

Create `internal/picker/rowgrid.go`:

```go
package picker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// closeCells is one close row's column values, in column order. Empty means
// "this row has nothing for that column"; a column no row fills is dropped
// entirely by newCloseGrid.
type closeCells struct {
	glyph  string
	cwd    string
	id     string
	title  string // the flex column
	badge  string
	cmd    string
	target string
	age    string
}

// titleFloor is the width below which the title stops absorbing pressure and
// the grid starts dropping whole columns instead. Under ~12 cells a branch
// name says nothing, so spending the row's last cells on it is waste.
const titleFloor = 12

// closeGrid holds the widths resolved once for a whole list, so every row
// renders against the same columns.
type closeGrid struct {
	innerWidth               int
	cwd, id, badge, cmd      int
	target, age              int
	title                    int
	dropCwd, dropBadge       bool
	dropCmd                  bool
}

// newCloseGrid resolves column widths for rows at innerWidth. Each optional
// column is as wide as the widest value present in the list and vanishes when
// no row has one; the title takes what is left, and columns are dropped in the
// declared shed order when even its floor does not fit.
func newCloseGrid(rows []closeCells, innerWidth int) closeGrid {
	g := closeGrid{innerWidth: innerWidth}
	for _, r := range rows {
		g.cwd = maxWidth(g.cwd, r.cwd)
		g.id = maxWidth(g.id, r.id)
		g.badge = maxWidth(g.badge, r.badge)
		g.cmd = maxWidth(g.cmd, r.cmd)
		g.target = maxWidth(g.target, r.target)
		g.age = maxWidth(g.age, r.age)
	}

	// Glyph is one cell and always present; age never sheds.
	fixed := func() int {
		w := 1 + 1 + g.age // glyph + separator + age
		for _, c := range []struct {
			width   int
			dropped bool
		}{
			{g.cwd, g.dropCwd},
			{g.id, false},
			{g.badge, g.dropBadge},
			{g.cmd, g.dropCmd},
			{g.target, false},
		} {
			if c.width > 0 && !c.dropped {
				w += c.width + 1 // value + its separator
			}
		}
		return w
	}

	// Shed in the declared order until the title clears its floor.
	g.title = innerWidth - fixed() - 1
	for _, shed := range []*bool{&g.dropCwd, &g.dropBadge, &g.dropCmd} {
		if g.title >= titleFloor {
			break
		}
		*shed = true
		g.title = innerWidth - fixed() - 1
	}
	if g.title < 1 {
		g.title = 1
	}
	return g
}

func maxWidth(cur int, s string) int {
	if w := lipgloss.Width(s); w > cur {
		return w
	}
	return cur
}

// render lays one row into the grid and returns the finished line plus the
// command's [start,end) cell range for lipgloss.StyleRanges, or (-1,-1) when
// the command is absent or was shed. Offsets are display cells: StyleRanges
// indexes by width, and glyph-dense names make bytes, runes and cells disagree.
func (g closeGrid) render(c closeCells) (string, int, int) {
	var b strings.Builder
	cmdStart, cmdEnd := -1, -1

	write := func(s string, width int) {
		b.WriteString(pad(s, width))
		b.WriteString(" ")
	}

	write(c.glyph, 1)
	if g.cwd > 0 && !g.dropCwd {
		write(c.cwd, g.cwd)
	}
	if g.id > 0 {
		write(c.id, g.id)
	}
	write(clipName(c.title, g.title), g.title)
	if g.badge > 0 && !g.dropBadge {
		write(c.badge, g.badge)
	}
	if g.cmd > 0 && !g.dropCmd {
		if c.cmd != "" {
			// Right-aligned so the command sits against the arrow, keeping the
			// "claude → mono:2" pairing the old flowing layout produced.
			cmdStart = lipgloss.Width(b.String()) + g.cmd - lipgloss.Width(c.cmd)
			cmdEnd = cmdStart + lipgloss.Width(c.cmd)
		}
		write(padLeft(c.cmd, g.cmd), g.cmd)
	}
	if g.target > 0 {
		write(clipLeft(c.target, g.target), g.target)
	}
	b.WriteString(padLeft(c.age, g.age))

	line := ansi.Truncate(b.String(), g.innerWidth, "")
	if w := ansi.StringWidth(line); w < g.innerWidth {
		line += strings.Repeat(" ", g.innerWidth-w)
	}
	if cmdEnd > ansi.StringWidth(line) {
		cmdStart, cmdEnd = -1, -1
	}
	return line, cmdStart, cmdEnd
}

// pad right-pads s to exactly width cells, truncating from the right when it
// overflows.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	if w := ansi.StringWidth(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// padLeft left-pads s to exactly width cells, so the value sits flush right.
func padLeft(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	if w := ansi.StringWidth(s); w < width {
		s = strings.Repeat(" ", width-w) + s
	}
	return s
}

// clipLeft cuts from the left, for a value whose tail discriminates — a
// restore target's ":12" says more than its session name's first letters.
func clipLeft(s string, width int) string {
	w := ansi.StringWidth(s)
	if w <= width {
		return s
	}
	return ansi.Truncate(ansi.TruncateLeft(s, w-width+1, "…"), width, "")
}
```

Note `clipName` is the existing helper in `view.go` and is reused unchanged — it already picks left-cut vs right-cut per value via `hasGlyphRun`, which is exactly the behaviour the spec wants for configured vs degraded titles.

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/picker/ -run TestCloseGrid -v`
Expected: PASS, all eight tests.

If `TestCloseGrid_ShedOrder` fails because two columns vanish at the same width, widen the sample values in `sampleRows()` so the thresholds separate — do not relax the assertion.

- [ ] **Step 5: Commit**

```bash
git add internal/picker/rowgrid.go internal/picker/rowgrid_test.go
git commit -m "feat(picker): pure column grid for close rows (#142)"
```

---

### Task 4: Route the close list through the grid

Swap the renderer over and delete the code the grid replaces. No decoration values yet — this task must leave the list looking correct with remux's own fields only, which is also exactly the degraded path from the spec.

**Files:**
- Modify: `internal/picker/view.go` — rewrite `renderRow` (~line 796), delete `layoutRow`, `cwdColumnWidth`, `fitCwd`
- Modify: `internal/picker/view.go` — `closeListView` gains a `grid closeGrid` field
- Test: `internal/picker/view_internal_test.go`

**Interfaces:**
- Consumes: `closeCells`, `newCloseGrid`, `closeGrid.render` from Task 3.
- Produces: `func (v closeListView) cells(r CloseRow) closeCells`.

- [ ] **Step 1: Write the failing test**

Append to `internal/picker/view_internal_test.go`:

```go
// The grid is resolved once per list, so two rows rendered from the same view
// agree on where the arrow sits. The old layoutRow joined fields with single
// spaces and let the arrow land wherever the name ended.
func TestRenderCloseList_ArrowColumnIsStable(t *testing.T) {
	rows, ctxs, live, now := closeRowsFixture(t)
	v := newCloseListView(rows, ctxs, live, now)
	var at []int
	for _, r := range rows {
		if !r.Selectable() {
			continue
		}
		line := v.renderRow(r, 100, false)
		at = append(at, strings.Index(ansi.Strip(line), "→"))
	}
	if len(at) < 2 {
		t.Fatal("fixture needs at least two selectable rows")
	}
	for i := 1; i < len(at); i++ {
		if at[i] != at[0] {
			t.Errorf("arrow drifts: row 0 at %d, row %d at %d", at[0], i, at[i])
		}
	}
}
```

Reuse whichever existing fixture helper `view_internal_test.go` already uses to build `(rows, ctxs, live, now)`; if none is factored out, extract one from `TestRenderCloseList_NeverOverflowsFrame` and name it `closeRowsFixture`.

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop -c go test ./internal/picker/ -run TestRenderCloseList_ArrowColumnIsStable`
Expected: FAIL — the arrow column differs per row under the current `layoutRow`.

- [ ] **Step 3: Write the implementation**

In `internal/picker/view.go`, replace the `widest` field on `closeListView` with the grid, resolved once for the whole list. `newCloseListView` gains an `innerWidth` parameter — the sole caller at `view.go:408` already has it in scope, and resolving per row instead would rescan every row's cells for every row drawn.

```go
type closeListView struct {
	ctxs  map[int64]CloseContext
	live  map[string]bool
	now   time.Time
	tails map[int64]string // EventID → cwd tail; absent when elided
	// grid is resolved once for the list, so every row shares one set of
	// column widths — that shared resolution is what makes the columns align.
	grid closeGrid
}
```

Change the signature to `newCloseListView(rows []CloseRow, ctxs map[int64]CloseContext, live map[string]bool, now time.Time, cols []config.DecorationColumn, innerWidth int) closeListView`. The `cols` parameter is unused until Task 5; accept it now so the signature churn happens once.

At the end of `newCloseListView`, after the `tails` loop, delete the `widest` tracking and resolve the grid:

```go
	cells := make([]closeCells, 0, len(rows))
	for _, r := range rows {
		if r.Selectable() {
			cells = append(cells, v.cells(r))
		}
	}
	v.grid = newCloseGrid(cells, innerWidth)
	return v
```

Update the call site at `view.go:408` to `newCloseListView(m.closeRows, m.closeContexts, m.runningSet, time.Now(), m.decorationColumns, innerWidth)` — add the `decorationColumns` field to `PickerModel` now (Task 5 adds its setter), defaulting to nil.

Every existing `newCloseListView(...)` call in `view_internal_test.go` (about ten of them) needs the two new arguments. Pass `nil` for `cols` and a width matching what that test renders at.

Add the extraction function and rewrite `renderRow`:

```go
// cells pulls one row's column values. Everything here is remux's own data;
// decoration columns are added in cells' caller once configured.
func (v closeListView) cells(r CloseRow) closeCells {
	cc := v.ctxs[r.EventID]
	cmd, _ := closedPaneInfo(cc)
	if cmd == "fish" {
		cmd = "" // a default shell says nothing
	}

	title := snapshot.StripFormat(r.Placement.WindowName)
	target := "→ " + r.Session
	if r.Scope == "session" {
		title = fmt.Sprintf("%dw", countWindows(cc.SubManifest))
	} else {
		target += ":" + strconv.Itoa(r.Placement.WindowIndex)
	}

	var extra []string
	if !v.live[r.Session] {
		extra = append(extra, "(gone)")
	}
	if r.Placement.PaneCount > 1 {
		extra = append(extra, fmt.Sprintf("%dp", r.Placement.PaneCount))
	}
	if len(extra) > 0 {
		title += "  " + strings.Join(extra, " ")
	}

	age := columnAge(v.now.Sub(time.UnixMilli(r.Ts)))
	if r.Count > 1 {
		age = fmt.Sprintf("×%d %s", r.Count, age)
	}

	return closeCells{
		glyph:  scopeGlyph(r.Scope),
		cwd:    v.tails[r.EventID],
		title:  title,
		cmd:    cmd,
		target: target,
		age:    age,
	}
}

func (v closeListView) renderRow(r CloseRow, innerWidth int, active bool) string {
	if innerWidth < 1 {
		innerWidth = 1
	}
	if r.Kind == RowDivider {
		return rowDim.Render(strings.Repeat("─", innerWidth))
	}
	if !r.Selectable() {
		return previewHeader.Width(innerWidth).Render(ansi.Truncate(r.Section, innerWidth, "…"))
	}

	line, cmdStart, cmdEnd := v.grid.render(v.cells(r))

	// One flat style over plain text, then StyleRanges punches in the
	// command's own colour: lipgloss v2 resets to the terminal default (not
	// the outer style) at the end of a span rendered separately and spliced in
	// by hand, so a nested Render() leaves a hole in the focused row's
	// background once the span ends.
	rowStyle, cmdStyle := closeRowScopeStyle(r.Scope), closeRowCmd
	if active {
		rowStyle, cmdStyle = rowFocus(rowStyle), cmdStyle.Background(rowFocusBg)
	}
	styled := rowStyle.Width(innerWidth).Render(line)
	if cmdStart < 0 {
		return styled
	}
	return lipgloss.StyleRanges(styled, lipgloss.NewRange(cmdStart, cmdEnd, cmdStyle))
}
```

Then delete these three functions from `view.go` entirely: `layoutRow`, `cwdColumnWidth`, `fitCwd`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./internal/picker/`
Expected: PASS.

Some existing tests assert the old layout's exact spacing and will fail. For each: if it asserts a *geometry invariant* (line width, frame closure, no overflow) it must keep passing — fix the code. If it asserts the *old column arrangement*, update the expectation to the grid's output and note in the test comment that the arrangement changed in #142. Do not delete a test to make it pass.

- [ ] **Step 5: Verify against the real picker**

Run: `nix develop -c go run ./cmd/tmux-remux pick --close` inside a tmux session with recent closes.
Expected: the arrow lines up on every row; no blank 24-cell gutter in a section where no row has a non-modal cwd.

- [ ] **Step 6: Commit**

```bash
git add internal/picker/view.go internal/picker/view_internal_test.go
git commit -m "refactor(picker): render close rows through the column grid (#142)"
```

---

### Task 5: Feed decoration columns into the grid

**Files:**
- Modify: `internal/picker/closelist.go` — add `closedWindow`
- Modify: `internal/picker/view.go` — `cells` reads decoration values
- Modify: `internal/picker/model.go` — `PickerModel` carries the column spec
- Test: `internal/picker/closelist_test.go`, `internal/picker/view_internal_test.go`

**Interfaces:**
- Consumes: `config.DecorationColumn`, `config.RoleID|RoleBadge|RoleText` from Task 1; `closeCells` from Task 3.
- Produces: `closedWindow(cc CloseContext) *snapshot.Window`, `func (m *PickerModel) SetDecorationColumns(cols []config.DecorationColumn)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/picker/closelist_test.go`:

```go
// The close list reads typed fields off the window the event took down, so it
// needs that window — closedPaneInfo only reaches the pane.
func TestClosedWindow(t *testing.T) {
	cc := CloseContext{
		Placement: ClosePlacement{Scope: "window", WindowIndex: 3},
		SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{
			Name: "s",
			Windows: []snapshot.Window{{
				Index:      3,
				Name:       "w",
				Decoration: map[string]string{"@issue_id": "ENG-8224"},
				Panes:      []snapshot.Pane{{Index: 0, Command: "claude"}},
			}},
		}}},
	}
	w := closedWindow(cc)
	if w == nil {
		t.Fatal("closedWindow returned nil for a window-scope close")
	}
	if got := w.Decoration["@issue_id"]; got != "ENG-8224" {
		t.Errorf("Decoration[@issue_id] = %q, want %q", got, "ENG-8224")
	}
}

func TestClosedWindow_NilWhenAbsent(t *testing.T) {
	if w := closedWindow(CloseContext{}); w != nil {
		t.Errorf("closedWindow(zero) = %v, want nil", w)
	}
}
```

Append to `internal/picker/view_internal_test.go`:

```go
// A configured RoleID column moves the ticket id out of the title and into its
// own column; a row with no value for it leaves the column blank rather than
// shifting its neighbours.
func TestCells_DecorationColumnsPopulateCells(t *testing.T) {
	cols := []config.DecorationColumn{
		{Option: "@issue_id", Role: config.RoleID, Max: 10},
		{Option: "@pr_number", Role: config.RoleBadge, Max: 6},
		{Option: "@issue_title", Role: config.RoleText},
	}
	cc := CloseContext{
		Placement: ClosePlacement{Scope: "window", WindowIndex: 1},
		SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{
			Windows: []snapshot.Window{{
				Index: 1,
				Name:  "raw-window-name",
				Decoration: map[string]string{
					"@issue_id":    "ENG-8224",
					"@pr_number":   "#3511",
					"@issue_title": "effort lever",
				},
				Panes: []snapshot.Pane{{Command: "claude"}},
			}},
		}}},
	}
	v := closeListView{
		ctxs: map[int64]CloseContext{7: cc},
		live: map[string]bool{"s": true},
		now:  time.Now(),
		cols: cols,
	}
	got := v.cells(CloseRow{Kind: RowClose, EventID: 7, Scope: "window", Session: "s",
		Placement: cc.Placement, Count: 1})

	if got.id != "ENG-8224" {
		t.Errorf("id = %q, want %q", got.id, "ENG-8224")
	}
	if got.badge != "#3511" {
		t.Errorf("badge = %q, want %q", got.badge, "#3511")
	}
	if got.title != "effort lever" {
		t.Errorf("title = %q, want the @issue_title value", got.title)
	}
}

// With no columns configured — or an event captured before they were — the
// title falls back to the window name through the same column. There is no
// second rendering path for old events.
func TestCells_FallsBackToWindowNameWithoutDecoration(t *testing.T) {
	cc := CloseContext{
		Placement:   ClosePlacement{Scope: "window", WindowIndex: 1, WindowName: "raw-window-name"},
		SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{
			Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Command: "claude"}}}},
		}}},
	}
	v := closeListView{
		ctxs: map[int64]CloseContext{7: cc},
		live: map[string]bool{"s": true},
		now:  time.Now(),
	}
	got := v.cells(CloseRow{Kind: RowClose, EventID: 7, Scope: "window", Session: "s",
		Placement: cc.Placement, Count: 1})

	if got.title != "raw-window-name" {
		t.Errorf("title = %q, want the window name", got.title)
	}
	if got.id != "" || got.badge != "" {
		t.Errorf("id/badge = %q/%q, want empty without decoration", got.id, got.badge)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop -c go test ./internal/picker/ -run 'TestClosedWindow|TestCells_'`
Expected: FAIL — `undefined: closedWindow`, and `closeListView` has no `cols` field.

- [ ] **Step 3: Write the implementation**

In `internal/picker/closelist.go`, beside `closedPaneInfo`:

```go
// closedWindow returns the window a close event took down, or nil when the
// sub-manifest does not contain it. A window- or pane-scope close is matched by
// Placement.WindowIndex; a session-scope close has no single window.
func closedWindow(cc CloseContext) *snapshot.Window {
	if cc.Placement.Scope == "session" {
		return nil
	}
	for _, s := range cc.SubManifest.Sessions {
		for i, w := range s.Windows {
			if w.Index == cc.Placement.WindowIndex {
				return &s.Windows[i]
			}
		}
	}
	return nil
}
```

In `internal/picker/view.go`, add the spec to `closeListView`:

```go
	cols []config.DecorationColumn
```

and thread it through `newCloseListView`'s signature. In `cells`, replace the title/id/badge derivation with a decoration-aware version. Insert after the `cmd` lines, before `title` is computed:

```go
	var id, badge, decoratedTitle string
	if w := closedWindow(cc); w != nil {
		for _, col := range v.cols {
			val := w.Decoration[col.Option]
			if val == "" {
				continue
			}
			switch col.Role {
			case config.RoleID:
				id = ansi.Truncate(val, col.Max, "…")
			case config.RoleBadge:
				badge = ansi.Truncate(val, col.Max, "…")
			case config.RoleText:
				decoratedTitle = val
			}
		}
	}

	title := decoratedTitle
	if title == "" {
		title = snapshot.StripFormat(r.Placement.WindowName)
	}
```

removing the previous `title := snapshot.StripFormat(...)` line, and set `id` and `badge` on the returned `closeCells`.

In `internal/picker/model.go`, add the field and setter so the picker can be configured without `internal/picker` importing anything that names an option:

```go
	decorationColumns []config.DecorationColumn
```

```go
// SetDecorationColumns configures which captured window options the close list
// renders as columns. The picker never names an option itself — the spec comes
// from config, which parses it from @remux_columns.
func (m *PickerModel) SetDecorationColumns(cols []config.DecorationColumn) {
	m.decorationColumns = cols
}
```

Pass `m.decorationColumns` at the `newCloseListView` call site in `view.go:408`.

In `cmd/tmux-remux/main.go`, call the setter wherever the close picker's model is constructed, right after `NewPickerModel`:

```go
		m.SetDecorationColumns(cfg.DecorationColumns)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop -c go test ./... && nix develop -c go build ./...`
Expected: PASS, build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/picker/ internal/config/ cmd/tmux-remux/main.go
git commit -m "feat(picker): render decoration columns in the close list (#142)"
```

---

### Task 6: Seam guard and documentation

**Files:**
- Test: `internal/picker/seam_test.go` (create)
- Modify: `README.md` — document `@remux_columns`
- Modify: `docs/superpowers/specs/2026-09-17-close-row-grid-design.md` — status line

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Create `internal/picker/seam_test.go`:

```go
package picker

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The picker renders decoration columns by ROLE. If an option name ever
// appears here, the config indirection has been bypassed and the coupling this
// package exists to avoid is back — so assert it as a test rather than trusting
// a convention.
func TestPickerNamesNoDecorationOptions(t *testing.T) {
	bad := regexp.MustCompile(`@(issue|pr|crew|window)_[a-z_]+`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		if m := bad.FindAllString(string(src), -1); len(m) > 0 {
			t.Errorf("%s names decoration options %v — route them through config.DecorationColumn instead", name, m)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it passes (and can fail)**

Run: `nix develop -c go test ./internal/picker/ -run TestPickerNamesNoDecorationOptions -v`
Expected: PASS.

Prove it is red-capable: temporarily add `var _ = "@issue_id"` to `internal/picker/rowgrid.go`, re-run, confirm FAIL, then remove it.

- [ ] **Step 3: Document the option**

In `README.md`, alongside the other `@remux_*` options, add:

````markdown
### `@remux_columns`

Declares which captured tmux window options the close picker renders as
columns. Comma-separated `option:role:max` triples. Roles are `id` (compact
identity, left-anchored), `badge` (short marker), and `text` (the flexible
title column; `max` is ignored). Unset by default — tmux-remux does not assume
any particular option schema.

```tmux
set -g @remux_columns '@issue_id:id:10,@pr_number:badge:6,@issue_title:text:0'
```

Declaring a column automatically adds its option to the snapshot allow-list, so
no separate capture configuration is needed. A malformed entry is skipped.
````

- [ ] **Step 4: Update the spec status**

In `docs/superpowers/specs/2026-09-17-close-row-grid-design.md`, change the status line to:

```markdown
**Status:** Implemented — see docs/superpowers/plans/2026-09-17-close-row-grid.md
```

- [ ] **Step 5: Full verification**

Run each and confirm the output before claiming success:

```bash
nix develop -c go test ./...
nix develop -c go vet ./...
nix develop -c golangci-lint run
nix develop -c gofmt -l .
```

Expected: all tests pass, vet silent, `0 issues.`, `gofmt -l` prints nothing.

- [ ] **Step 6: Commit and open the PR**

```bash
git add internal/picker/seam_test.go README.md docs/superpowers/specs/2026-09-17-close-row-grid-design.md
git commit -m "test(picker): guard the decoration seam; document @remux_columns (#142)"
```

Then run the `/deslop` skill over the branch before pushing (the pre-push hook enforces it), and open the PR with `gh pr create --assignee @me`, closing #142.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| Capture is a union, not derived | 1 (`CaptureOptions`, tested) |
| `ColumnRole` / `DecorationColumn` types | 1 |
| `@remux_columns` parsing, malformed entry skipped | 1 |
| Compiled default is no columns | 1 (`TestDefaultHasNoDecorationColumns`) |
| `Max` ignored for `RoleText` | 1 (`TestParseDecorationColumns_TextIgnoresMax`) |
| Shared option reader | 2 |
| Auto-width columns vanishing at zero | 3 (`TestCloseGrid_AbsentColumnVanishes`) |
| Flex title, floor 12 | 3 (`titleFloor`) |
| `cmd → target` + age right-anchored | 3 (`padLeft`, `TestCloseGrid_ColumnsAlignAcrossRows`) |
| Declared shed order | 3 (`TestCloseGrid_ShedOrder`) |
| Age never sheds | 3 (`TestCloseGrid_AgeNeverSheds`) |
| Target clips from the left | 3 (`TestCloseGrid_TargetClipsFromTheLeft`) |
| `layoutRow`/`cwdColumnWidth`/`fitCwd` deleted | 4 |
| `closedWindow` helper | 5 |
| Degradation to window name, one code path | 5 (`TestCells_FallsBackToWindowNameWithoutDecoration`) |
| `clipName` kept as-is | 3 (reused unchanged), 5 (fallback feeds it the glyph-run name) |
| Seam guard test | 6 |
| Frame-geometry tests stay green | 4 Step 4, 6 Step 5 |

**Placeholder scan:** no TBD/TODO; every code step carries complete code; every run step names an exact command and expected output.

**Type consistency:** `closeCells` field names (`glyph`, `cwd`, `id`, `title`, `badge`, `cmd`, `target`, `age`) are used identically in Tasks 3, 4 and 5. `config.RoleID`/`RoleBadge`/`RoleText` are spelled the same in Tasks 1 and 5. `newCloseGrid(rows, innerWidth)` and `render(c) (string, int, int)` match between definition (3) and both call sites (4, 5). `CaptureOptions()` is defined in 1 and called in 2.

**Note on grid lifetime:** the grid is resolved exactly once per list, in `newCloseListView`, and every row renders against it. That shared resolution is what makes columns align; resolving per row would both cost a rescan of all cells per row drawn and, worse, let a column's width differ between rows.

**Pre-flight corrections to this plan** (made before execution, after the drafted code was checked against the call sites):
- `newCloseListView` takes `innerWidth` and stores the resolved grid. An earlier draft stored raw cells and re-resolved inside `renderRow`, which was O(rows²) per frame for no benefit — `view.go:408` already has `innerWidth` in scope.
- `TestCloseGrid_CommandRangeOnlyWhenCommandRendered` replaced a single-width probe that called `t.Skip` when the command survived. A test that can skip its only assertion asserts nothing; the sweep asserts at every width and fails loudly if the shed case never occurs.


---

## Correction during execution (Task 4)

The shed order originally put the title's shrink before the cwd's drop, and
sized the cwd purely to the widest value present. Both were wrong against
decisions already documented in the code, found when Task 4 integrated the grid:

- `closeListMin = 71`'s comment states the floor was read off a list whose rows
  carry every column **including the cwd tail**. Uncapped, one 34-cell path
  sheds the cwd for every row at exactly that width.
- `TestCloseListRow_CwdYieldsBeforeTheName` pins that the cwd is given up whole
  before a single cell is taken from the name.

Resolved with the user in favour of the existing policy: cap the cwd at
`min(innerWidth/4, 24)`, fit it with `fitCwd` (moved into `rowgrid.go` with its
two tests, which keep passing unchanged), and shed it before the title shrinks.
The zero-width-when-absent rule is unchanged — that is the fix this whole change
exists for.
