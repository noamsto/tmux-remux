package panemap

import (
	"fmt"
	"math"
	"strings"
)

// Below this the boxes carry no information, so Render returns a summary line
// instead of art. Starting values; adjust once there is something to judge.
const (
	minMapWidth  = 24
	minMapHeight = 6
)

// Render draws g's panes as box art exactly w columns by h rows.
//
// It walks g's split tree: each container divides its own rectangle among its
// children and consecutive children share the dividing line, so a divider is
// always one line with junctions rather than two abutting borders, and a split
// in one column can never collide with a split in another.
//
// label and marked may be nil; Task 3 wires them.
func Render(g Grid, w, h int, label func(int) string, marked func(int) bool) string {
	if w < minMapWidth || h < minMapHeight {
		return summary(g)
	}
	return render(g, w, h, label, marked)
}

// render skips the panel-size guard so tests can assert exact output below it.
// It still degrades to a summary when the layout itself is undrawable at this
// size — a pane too small to hold an interior — which is a property of the
// geometry rather than of the panel size. label and marked may be nil.
func render(g Grid, w, h int, label func(int) string, marked func(int) bool) string {
	if g.root == nil {
		return summary(g)
	}
	if label == nil {
		label = func(int) string { return "" }
	}
	if marked == nil {
		marked = func(int) bool { return false }
	}
	var boxes []box
	if !layoutNode(g.root, 0, 0, w-1, h-1, &boxes) {
		return summary(g)
	}

	// hEdge[y][x]: a horizontal segment occupies this cell. vEdge likewise.
	// Sibling boxes set the same shared cell, so runeFor merges the divider.
	// dashed marks the borders of a marked pane so its lines render broken.
	hEdge := grid2D(w, h)
	vEdge := grid2D(w, h)
	dashed := grid2D(w, h)
	for _, b := range boxes {
		for x := b.x0; x <= b.x1; x++ {
			hEdge[b.y0][x] = true
			hEdge[b.y1][x] = true
		}
		for y := b.y0; y <= b.y1; y++ {
			vEdge[y][b.x0] = true
			vEdge[y][b.x1] = true
		}
		if !marked(b.idx) {
			continue
		}
		for x := b.x0; x <= b.x1; x++ {
			dashed[b.y0][x] = true
			dashed[b.y1][x] = true
		}
		for y := b.y0; y <= b.y1; y++ {
			dashed[y][b.x0] = true
			dashed[y][b.x1] = true
		}
	}

	out := make([][]rune, h)
	for y := range out {
		out[y] = make([]rune, w)
		for x := range out[y] {
			r := runeFor(hEdge, vEdge, x, y, w, h)
			if dashed[y][x] {
				r = dash(r)
			}
			out[y][x] = r
		}
	}
	drawLabels(out, boxes, label)
	return joinRunes(out)
}

type box struct {
	x0, y0, x1, y1, idx int
}

// layoutNode places n and its descendants into the panel rectangle
// [x0,y0]-[x1,y1], inclusive of borders, appending one box per leaf. A
// container divides its OWN rectangle: consecutive children share the dividing
// line (each child's far edge is the next child's near edge), so the divider is
// one line and a split confined to this rectangle cannot reach another. It
// returns false the moment a leaf is too small to draw with an interior — two
// borders with at least one cell between them — so the caller degrades to a
// summary rather than emit bare abutting borders.
func layoutNode(n *node, x0, y0, x1, y1 int, boxes *[]box) bool {
	switch n.kind {
	case leafKind:
		if x1-x0 < 2 || y1-y0 < 2 {
			return false
		}
		*boxes = append(*boxes, box{x0: x0, y0: y0, x1: x1, y1: y1, idx: n.index})
		return true

	case rowsKind: // stacked top-to-bottom: divide the Y axis
		top := y0
		for i, c := range n.children {
			bottom := y1
			if i < len(n.children)-1 {
				// The tmux separator sits just below child c; place the divider
				// there, scaled into this rectangle's height.
				sep := c.y + c.h
				bottom = y0 + iround(float64(sep-n.y)*float64(y1-y0)/float64(n.h))
			}
			if !layoutNode(c, x0, top, x1, bottom, boxes) {
				return false
			}
			top = bottom
		}
		return true

	case colsKind: // side by side: divide the X axis
		left := x0
		for i, c := range n.children {
			right := x1
			if i < len(n.children)-1 {
				sep := c.x + c.w
				right = x0 + iround(float64(sep-n.x)*float64(x1-x0)/float64(n.w))
			}
			if !layoutNode(c, left, y0, right, y1, boxes) {
				return false
			}
			left = right
		}
		return true
	}
	return false
}

func iround(f float64) int { return int(math.Round(f)) }

// dash swaps a plain line for its dashed counterpart. Junctions stay solid:
// they belong to two boxes at once, and dashing them would claim a border the
// marked pane only half owns.
func dash(r rune) rune {
	switch r {
	case '─':
		return '┄'
	case '│':
		return '┆'
	}
	return r
}

// drawLabels writes each pane's label on the first interior row of its box,
// truncated to fit. A box with no interior room keeps its shape and loses the
// text.
func drawLabels(out [][]rune, boxes []box, label func(int) string) {
	for _, b := range boxes {
		room := b.x1 - b.x0 - 3 // borders plus one space of padding each side
		row := b.y0 + 1
		if room < 1 || row >= b.y1 {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%d %s", b.idx, label(b.idx)))
		for i, r := range []rune(text) {
			if i >= room {
				break
			}
			out[row][b.x0+2+i] = r
		}
	}
}

// runeFor picks the box-drawing rune for one cell from which of its four
// neighbours continue a line.
func runeFor(hEdge, vEdge [][]bool, x, y, w, h int) rune {
	if !hEdge[y][x] && !vEdge[y][x] {
		return ' '
	}
	up := y > 0 && vEdge[y-1][x]
	down := y < h-1 && vEdge[y+1][x]
	left := x > 0 && hEdge[y][x-1]
	right := x < w-1 && hEdge[y][x+1]

	switch {
	case up && down && left && right:
		return '┼'
	case up && down && right:
		return '├'
	case up && down && left:
		return '┤'
	case left && right && down:
		return '┬'
	case left && right && up:
		return '┴'
	case down && right:
		return '┌'
	case down && left:
		return '┐'
	case up && right:
		return '└'
	case up && left:
		return '┘'
	case up || down:
		return '│'
	default:
		return '─'
	}
}

func summary(g Grid) string {
	return fmt.Sprintf("%d panes · %d×%d", len(g.Panes), g.W, g.H)
}

func grid2D(w, h int) [][]bool {
	g := make([][]bool, h)
	for i := range g {
		g[i] = make([]bool, w)
	}
	return g
}

func joinRunes(rows [][]rune) string {
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(r))
	}
	return b.String()
}
