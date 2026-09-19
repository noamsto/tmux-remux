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
	at := make([]int, 0, len(rows))
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
	if cwdGone <= badgeGone || badgeGone <= cmdGone {
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
