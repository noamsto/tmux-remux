package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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

// wideAgeRows makes the age wider than the row at plausible widths: a grouped
// close list prefixes the age with its repeat count, so "×12 1234m" is a real
// value and the sweep must reach widths below it.
func wideAgeRows() []closeCells {
	return []closeCells{
		{glyph: "▣", cwd: "noamsto/hookyard", id: "ENG-8224", title: "effort lever",
			badge: "#3511", cmd: "claude", target: "→ mono:2", age: "×12 1234m"},
		{glyph: "□", id: "#56", title: "doctor report",
			cmd: "codex", target: "→ hookyard:2", age: "×3 34m"},
	}
}

// wideRuneRows makes bytes, runes and display cells disagree: CJK is two cells
// per rune, and a Private Use Area Nerd Font glyph is one cell across three
// bytes. An offset that is secretly byte- or rune-based survives the ASCII
// fixture and dies here.
func wideRuneRows() []closeCells {
	return []closeCells{
		{glyph: "\ue0a0", cwd: "noamsto/世界", id: "ENG-8224", title: "\uf07b 日本語のタイトル",
			badge: "#3511", cmd: "claude", target: "→ セッション:12", age: "34m"},
		{glyph: "\ue0a0", cwd: "noamsto/dispatcher", id: "#56", title: "doctor report",
			cmd: "codex", target: "→ hookyard:2", age: "27m"},
	}
}

// Every rendered line must be exactly innerWidth: the frame pads short content
// but does not clip overflow, so a long line pushes the border out and desyncs
// the sibling panes.
func TestCloseGrid_EveryLineIsExactlyInnerWidth(t *testing.T) {
	for name, rows := range map[string][]closeCells{
		"sample":   sampleRows(),
		"wideAge":  wideAgeRows(),
		"wideRune": wideRuneRows(),
	} {
		for w := 0; w <= 160; w++ {
			g := newCloseGrid(rows, w)
			for i, c := range rows {
				line, _, _ := g.render(c)
				if got := ansi.StringWidth(line); got != w {
					t.Fatalf("%s width %d row %d: line is %d cells:\n%q", name, w, i, got, line)
				}
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
	for name, rows := range map[string][]closeCells{
		"sample":   sampleRows(),
		"wideRune": wideRuneRows(),
	} {
		g := newCloseGrid(rows, 120)
		at := make([]int, 0, len(rows))
		for _, c := range rows {
			line, _, _ := g.render(c)
			stripped := ansi.Strip(line)
			arrow := strings.Index(stripped, "→")
			if arrow < 0 {
				t.Fatalf("%s: no target column in %q", name, stripped)
			}
			// Cells, not the byte index: two rows whose titles differ in rune
			// width can share a byte offset while their columns are askew.
			at = append(at, lipgloss.Width(stripped[:arrow]))
		}
		for i := 1; i < len(at); i++ {
			if at[i] != at[0] {
				t.Errorf("%s: arrow column drifts: row 0 at %d, row %d at %d", name, at[0], i, at[i])
			}
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

// The age is the list's sort key, so it survives every width wide enough to
// hold it — and below that it takes the whole row, rather than buying itself
// room by overflowing the line.
func TestCloseGrid_AgeNeverSheds(t *testing.T) {
	for name, rows := range map[string][]closeCells{
		"sample":  sampleRows(),
		"wideAge": wideAgeRows(),
	} {
		age := rows[0].age
		for w := 0; w <= 160; w++ {
			g := newCloseGrid(rows, w)
			line, _, _ := g.render(rows[0])
			stripped := ansi.Strip(line)
			if w >= lipgloss.Width(age) {
				if !strings.Contains(stripped, age) {
					t.Fatalf("%s width %d: age dropped:\n%q", name, w, stripped)
				}
				continue
			}
			if !strings.HasPrefix(age, stripped) {
				t.Fatalf("%s width %d: row is %q, want the age %q cut to fit", name, w, stripped, age)
			}
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
	for name, rows := range map[string][]closeCells{
		"sample":   sampleRows(),
		"wideRune": wideRuneRows(),
	} {
		g := newCloseGrid(rows, 120)
		line, start, end := g.render(rows[0])
		if start < 0 || end <= start {
			t.Fatalf("%s: no command range reported: (%d, %d)", name, start, end)
		}
		// Sliced exactly as lipgloss.StyleRanges slices it, by display cells.
		stripped := ansi.Strip(line)
		got := ansi.Truncate(ansi.TruncateLeft(stripped, start, ""), end-start, "")
		if got != "claude" {
			t.Errorf("%s: command range covers %q, want %q", name, got, "claude")
		}
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

// The cwd column is cut from the left, since the end of a path is what
// discriminates. A cut that lands mid-segment reads as a mangled word rather
// than a path, so it is nudged forward to the next "/" — but only while that
// is cheap: giving up a long leading segment whole costs more than the ragged
// edge does. Either way the column is exactly as wide as it was asked for,
// which is what keeps the row's other columns on the grid's resolved widths.
func TestFitCwd_PrefersAPathBoundary(t *testing.T) {
	for _, tc := range []struct {
		tail  string
		width int
		want  string
	}{
		{"noamsto/tmux-remux", 22, "noamsto/tmux-remux    "},
		{"noamsto/tmux-remux", 14, "…/tmux-remux  "},
		{"noamsto/tmux-remux/internal/picker", 18, "…/internal/picker "},
		// Snapping here would drop "services" as well — eight of eighteen
		// cells — so the ragged cut is the lesser loss.
		{"factify/services/document", 18, "…services/document"},
	} {
		if got := fitCwd(tc.tail, tc.width); got != tc.want {
			t.Errorf("fitCwd(%q, %d) = %q, want %q", tc.tail, tc.width, got, tc.want)
		}
	}
}

// A double-width rune straddling the truncation cut can leave the cut one
// cell over budget; fitCwd must still land on exactly width cells rather than
// panicking on a negative strings.Repeat count. Covers the cwd column's whole
// production range, cwdFloor to cwdCap, against tails with CJK and emoji runes.
func TestFitCwd_ExactWidthAcrossWideRunes(t *testing.T) {
	for _, tail := range []string{
		"git/日本語プロジェクト/internal",
		"emoji/📁folder/sub",
		"noamsto/tmux-remux/internal/picker",
		"factify/services/document",
	} {
		for width := cwdFloor; width <= cwdCap; width++ {
			got := fitCwd(tail, width)
			if w := lipgloss.Width(got); w != width {
				t.Errorf("fitCwd(%q, %d) = %q, width %d, want %d", tail, width, got, w, width)
			}
		}
	}
}

// A double-width rune straddling a truncation cut can leave the cut one cell
// over budget, so a column fitter must clamp back rather than hand the row a
// cell it did not ask for or panic on a negative strings.Repeat count. pad and
// padLeft land on exactly the width asked for; clipLeft never exceeds it, and
// hits it exactly whenever it had to cut, since the grid squares up what it
// returns afterwards.
//
// TestFitCwd_ExactWidthAcrossWideRunes covers the same ground for the cwd
// column's own fitter; these three are the ones every other column goes
// through. TestCloseGrid_EveryLineIsExactlyInnerWidth does not reach them:
// it measures the finished line, where one column a cell over cancels another
// a cell under.
func TestGridPadding_ExactWidthAcrossWideRunes(t *testing.T) {
	for _, s := range []string{
		"git/日本語プロジェクト/internal",
		"emoji/📁folder/sub",
		"noamsto/tmux-remux/internal/picker",
		"factify/services/document",
	} {
		// Not the cwd's range: pad, padLeft and clipLeft square up every
		// column, so this sweeps from a single cell to wider than any value
		// here needs.
		for width := 1; width <= 40; width++ {
			if got := pad(s, width); lipgloss.Width(got) != width {
				t.Errorf("pad(%q, %d) = %q, width %d", s, width, got, lipgloss.Width(got))
			}
			if got := padLeft(s, width); lipgloss.Width(got) != width {
				t.Errorf("padLeft(%q, %d) = %q, width %d", s, width, got, lipgloss.Width(got))
			}
			want := min(width, lipgloss.Width(s))
			if got := clipLeft(s, width); lipgloss.Width(got) != want {
				t.Errorf("clipLeft(%q, %d) = %q, width %d, want %d", s, width, got, lipgloss.Width(got), want)
			}
		}
	}
}

// Every column but the title and the age has a rung in the shed ladder. The id
// needs one most: its width is whatever the column spec asked for, so without
// a rung a wide declared id holds its full width while the title is crushed to
// a single cell. The title identifies the row; no other column may starve it.
func TestCloseGrid_WideIdDoesNotStarveTheTitle(t *testing.T) {
	rows := []closeCells{
		{glyph: "▣", id: strings.Repeat("X", 40), title: "a-window-name-worth-reading",
			cmd: "claude", target: "→ mono:2", age: "4m"},
	}
	for w := 30; w <= 70; w++ {
		g := newCloseGrid(rows, w)
		if g.id > 0 && g.title < titleFloor {
			t.Errorf("width %d: id holds %d cells while the title is down to %d, under its floor of %d",
				w, g.id, g.title, titleFloor)
		}
	}
}

// The section header is rendered through the same grid as the rows beneath
// it, so a label sits over its own column by construction rather than by a
// second width calculation that could disagree. Pinning it here means a change
// to the grid cannot move the values out from under their labels.
func TestCloseGrid_LabelRowSharesTheRowsColumns(t *testing.T) {
	rows := sampleRows()
	for w := 40; w <= 160; w++ {
		g := newCloseGrid(rows, w)
		labels, _, _ := g.render(closeCells{cwd: "path", cmd: "cmd", target: "to", age: "age"})
		row, _, _ := g.render(rows[0])
		if ansi.StringWidth(labels) != ansi.StringWidth(row) {
			t.Fatalf("w=%d: label row is %d cells, row is %d", w, ansi.StringWidth(labels), ansi.StringWidth(row))
		}
		// The command column is right-aligned, so its label ends where the
		// value does; that shared edge is what makes the label read as a
		// heading rather than as a stray word.
		if g.cmd >= 6 {
			// Cells, not byte offsets: the scope glyph is three bytes wide and
			// one cell, so a byte index is already off by two before the first
			// column begins.
			endCell := func(line, s string) int {
				stripped := ansi.Strip(line)
				return lipgloss.Width(stripped[:strings.Index(stripped, s)+len(s)])
			}
			if l, v := endCell(labels, "cmd"), endCell(row, "claude"); l != v {
				t.Errorf("w=%d: cmd label ends at cell %d, its value at %d", w, l, v)
			}
		}
	}
}
