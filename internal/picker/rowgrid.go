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

// cwdFloor is the width below which a fitted path fragment stops saying
// anything a reader can act on — "…ker" is a syllable, not a directory — so
// the column is given up whole rather than shown at less than this.
const cwdFloor = 8

// cwdCap bounds the cwd column so one deep path cannot price the column out
// of the list: without it the column is sized to the widest tail present, and
// a single 34-cell path pushes the title under its floor and sheds the cwd for
// every row. Paired with the quarter-row budget in newCloseGrid.
const cwdCap = 24

// closeGrid holds the column widths resolved once for a whole list, so every
// row renders against the same columns. A zero width means the column draws
// nothing at all — value and separator both — whether because no row filled
// it or because it was shed to make the row fit. The glyph is one cell and
// always drawn.
type closeGrid struct {
	innerWidth int
	cwd, id    int
	title      int
	badge, cmd int
	target     int
	age        int
}

// newCloseGrid resolves column widths for rows at innerWidth. Each optional
// column is as wide as the widest value present in the list — the cwd capped,
// since a path is the one value that can be arbitrarily deep — the title takes
// what is left, and columns are shed in the declared order as the row runs out
// of room.
func newCloseGrid(rows []closeCells, innerWidth int) closeGrid {
	g := closeGrid{innerWidth: innerWidth}
	want := 0 // the widest title in the list, before any of it is given up
	for _, r := range rows {
		g.cwd = maxWidth(g.cwd, r.cwd)
		g.id = maxWidth(g.id, r.id)
		want = maxWidth(want, r.title)
		g.badge = maxWidth(g.badge, r.badge)
		g.cmd = maxWidth(g.cmd, r.cmd)
		g.target = maxWidth(g.target, r.target)
		g.age = maxWidth(g.age, r.age)
	}
	// The age never sheds, but it cannot be wider than the row either: past
	// that it would push the line over innerWidth, and the frame pads without
	// clipping. Clamping here keeps budget+age == innerWidth exact in render.
	g.age = min(g.age, max(innerWidth, 0))

	// The cwd is the one column whose widest value says nothing about how much
	// width it deserves: a path nests arbitrarily deep, so sizing the column to
	// it would let one close in a far-down directory decide the row for every
	// other. A quarter of the row, and never more than 24 cells; under eight a
	// path fragment says nothing a reader can act on, so the column goes. A
	// list where no row has a cwd at all stays at zero throughout — that is
	// what retires the blank gutter, and the cap must not resurrect it.
	if g.cwd > 0 {
		if g.cwd = min(g.cwd, innerWidth/4, cwdCap); g.cwd < cwdFloor {
			g.cwd = 0
		}
	}

	// The cwd yields before the title gives up a cell: a window name clipped
	// from a shared column still identifies its window, where a path already
	// fitted into one is the value that has least left to lose. It gives
	// ground down to its own floor first and goes whole only when even that
	// is not enough — a fitted tail still discriminates at eight cells, so
	// shedding the column while it could still be shown gives up more than
	// the narrower column costs. Past that the title absorbs down to its own
	// floor, and only then do the badge and the command go.
	g.title = innerWidth - g.fixed() - 1
	if g.title < want && g.cwd > 0 {
		if give := want - g.title; g.cwd-give >= cwdFloor {
			g.cwd -= give
		} else {
			g.cwd = 0
		}
		g.title = innerWidth - g.fixed() - 1
	}
	for _, col := range []*int{&g.badge, &g.cmd} {
		if g.title >= titleFloor {
			break
		}
		*col = 0
		g.title = innerWidth - g.fixed() - 1
	}
	// Past the floor the title keeps giving ground until it is down to its
	// last cell; from there the target pays instead, which it can afford
	// because it is clipped from the left and its tail is what discriminates.
	if g.title < 1 {
		if deficit := 1 - g.title; g.target-deficit >= 1 {
			g.target -= deficit
		} else {
			g.target = 0
		}
		g.title = innerWidth - g.fixed() - 1
		if g.title < 1 {
			g.title = 1
		}
	}
	return g
}

// fixed returns the cells every column but the title consumes, each with its
// trailing separator. The glyph is one cell plus a separator; the age closes
// the row, so it has none.
func (g closeGrid) fixed() int {
	w := 1 + 1 + g.age
	for _, c := range []int{g.cwd, g.id, g.badge, g.cmd, g.target} {
		if c > 0 {
			w += c + 1
		}
	}
	return w
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
// indexes by width, and glyph-dense names make bytes, runes and cells
// disagree.
func (g closeGrid) render(c closeCells) (string, int, int) {
	var b strings.Builder
	cmdStart, cmdEnd := -1, -1

	col := func(s string) {
		b.WriteString(s)
		b.WriteByte(' ')
	}

	col(pad(c.glyph, 1))
	if g.cwd > 0 {
		col(fitCwd(c.cwd, g.cwd))
	}
	if g.id > 0 {
		col(pad(c.id, g.id))
	}
	col(pad(clipName(c.title, g.title), g.title))
	if g.badge > 0 {
		col(pad(c.badge, g.badge))
	}
	if g.cmd > 0 {
		if c.cmd != "" {
			// Right-aligned so the command sits against the arrow, keeping the
			// "claude → mono:2" pairing the old flowing layout produced.
			cmdStart = lipgloss.Width(b.String()) + g.cmd - lipgloss.Width(c.cmd)
			cmdEnd = cmdStart + lipgloss.Width(c.cmd)
		}
		col(padLeft(c.cmd, g.cmd))
	}
	if g.target > 0 {
		col(pad(clipLeft(c.target, g.target), g.target))
	}

	// The age closes the row against the right edge and never sheds, so the
	// rest of the row is fitted to what it leaves rather than the line being
	// cut as a whole: a width too narrow for the columns then eats the title,
	// not the sort key.
	age := padLeft(c.age, g.age)
	budget := max(g.innerWidth-g.age, 0)
	if cmdEnd > budget {
		// That fitting cut the command away; a range over those cells would
		// recolour whatever now sits in them.
		cmdStart, cmdEnd = -1, -1
	}
	return pad(b.String(), budget) + age, cmdStart, cmdEnd
}

// pad fits s to exactly width cells, padding on the right and cutting from
// the right. Values that need an ellipsis or a left cut are fitted by their
// own helper first; this is the squaring-up that keeps each column exact.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width, "")
	if w := lipgloss.Width(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// padLeft fits s to exactly width cells with the padding on the left, so the
// value sits flush right.
func padLeft(s string, width int) string {
	s = ansi.Truncate(s, width, "")
	if w := lipgloss.Width(s); w < width {
		s = strings.Repeat(" ", width-w) + s
	}
	return s
}

// fitCwd pads or left-truncates a tail to exactly width cells. Truncation is
// from the left, since the tail is what discriminates. A cut that lands
// mid-segment ("…sto/tmux-remux") reads as a mangled word rather than a path,
// so the cut is nudged forward to the next "/" when that costs only a few
// more cells — past that the segment is long enough that losing it whole
// gives up more than the ragged edge does.
func fitCwd(tail string, width int) string {
	w := lipgloss.Width(tail)
	if w <= width {
		return tail + strings.Repeat(" ", width-w)
	}
	cut := ansi.TruncateLeft(tail, w-width+1, "…")
	if i := strings.IndexByte(cut, '/'); i > 0 && lipgloss.Width(cut[:i]) <= 6 {
		cut = "…" + cut[i:]
	}
	// A double-width rune straddling the TruncateLeft cut can leave it one
	// cell over width; clamp before padding so the Repeat count never goes
	// negative.
	cut = ansi.Truncate(cut, width, "")
	if pad := width - lipgloss.Width(cut); pad > 0 {
		cut += strings.Repeat(" ", pad)
	}
	return cut
}

// clipLeft cuts from the left, for a value whose tail discriminates — a
// restore target's ":12" says more than its session name's first letters.
func clipLeft(s string, width int) string {
	w := lipgloss.Width(s)
	if w <= width {
		return s
	}
	// A double-width rune straddling the cut can leave TruncateLeft one cell
	// over budget; the re-truncate clamps it back, as fitCwd does.
	cut := ansi.Truncate(ansi.TruncateLeft(s, w-width+1, "…"), width, "")
	if cut == "" {
		// At width 1 the cut consumes the whole string, and TruncateLeft drops
		// its own ellipsis along with it. A column asked for a cell should
		// still say something is there.
		return ansi.Truncate("…", width, "")
	}
	return cut
}
