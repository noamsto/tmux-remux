package panemap

import (
	"fmt"
	"math"
	"slices"
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
// Borders are accumulated into edge masks and only then turned into runes, so a
// divider shared by two panes becomes one line with junctions rather than two
// adjacent borders — which is also what keeps rounding from doubling an edge.
//
// label and marked may be nil; Task 3 wires them.
func Render(g Grid, w, h int, label func(int) string, marked func(int) bool) string {
	if w < minMapWidth || h < minMapHeight {
		return summary(g)
	}
	return render(g, w, h, label, marked)
}

// render skips the panel-size guard so tests can assert exact output below it.
// It still degrades to a summary when the layout itself is undrawable, which is
// a property of the pane geometry rather than of the panel size.
func render(g Grid, w, h int, _ func(int) string, _ func(int) bool) string {
	boxes, ok := scale(g, w, h)
	if !ok {
		return summary(g)
	}

	// hEdge[y][x]: a horizontal segment occupies this cell. vEdge likewise.
	hEdge := grid2D(w, h)
	vEdge := grid2D(w, h)
	for _, b := range boxes {
		for x := b.x0; x <= b.x1; x++ {
			hEdge[b.y0][x] = true
			hEdge[b.y1][x] = true
		}
		for y := b.y0; y <= b.y1; y++ {
			vEdge[y][b.x0] = true
			vEdge[y][b.x1] = true
		}
	}

	out := make([][]rune, h)
	for y := range out {
		out[y] = make([]rune, w)
		for x := range out[y] {
			out[y][x] = runeFor(hEdge, vEdge, x, y, w, h)
		}
	}
	return joinRunes(out)
}

type box struct {
	x0, y0, x1, y1, idx int
}

// scale maps window cells onto panel cells, reporting false when the panel is
// too small to give every pane a box with interior — the caller then degrades to
// a summary rather than drawing a pane as a bare line. w-1 and h-1 because a
// box's right and bottom borders sit *on* the last column and row.
func scale(g Grid, w, h int) ([]box, bool) {
	alongX := func(p Rect) (int, int) { return p.X, p.W }
	alongY := func(p Rect) (int, int) { return p.Y, p.H }

	xBounds := make([]int, 0, 2*len(g.Panes))
	yBounds := make([]int, 0, 2*len(g.Panes))
	for _, p := range g.Panes {
		xBounds = append(xBounds, p.X, p.X+p.W)
		yBounds = append(yBounds, p.Y, p.Y+p.H)
	}
	xMap := scaleBoundaries(xBounds, sharedDividers(g.Panes, alongX, alongY), float64(w-1)/float64(g.W), w-1)
	yMap := scaleBoundaries(yBounds, sharedDividers(g.Panes, alongY, alongX), float64(h-1)/float64(g.H), h-1)

	boxes := make([]box, 0, len(g.Panes))
	for _, p := range g.Panes {
		b := box{
			x0:  clamp(xMap[p.X], w-1),
			y0:  clamp(yMap[p.Y], h-1),
			x1:  clamp(xMap[p.X+p.W], w-1),
			y1:  clamp(yMap[p.Y+p.H], h-1),
			idx: p.Index,
		}
		if b.x1 <= b.x0 || b.y1 <= b.y0 {
			return nil, false
		}
		boxes = append(boxes, b)
	}
	return boxes, true
}

// sharedDividers returns the boundary pairs that a divider shared by two panes
// leaves 1 apart: the near pane's far edge, the divider itself, then the far
// pane's near edge. along picks the axis being scaled, across the other one —
// panes that satisfy the coordinate relation without overlapping across it sit
// in unrelated parts of the window and share no divider.
func sharedDividers(panes []Rect, along, across func(Rect) (int, int)) [][2]int {
	var pairs [][2]int
	for _, a := range panes {
		aStart, aLen := along(a)
		aAcross, aAcrossLen := across(a)
		for _, b := range panes {
			bStart, _ := along(b)
			if bStart != aStart+aLen+1 {
				continue
			}
			bAcross, bAcrossLen := across(b)
			if aAcross >= bAcross+bAcrossLen || bAcross >= aAcross+aAcrossLen {
				continue
			}
			pairs = append(pairs, [2]int{aStart + aLen, bStart})
		}
	}
	return pairs
}

// scaleBoundaries maps each raw coordinate onto a panel coordinate, giving the
// two sides of every shared divider the same one so runeFor can merge them into
// a single line. pairs are only the boundaries that actually flank a divider, so
// a 1-cell pane keeps its own two edges apart; the relation is closed
// transitively, so a divider chained across columns split at different heights
// still lands on one line. The classes holding the outermost boundaries are
// pinned to 0 and limit, keeping the frame flush with the panel edges.
func scaleBoundaries(raw []int, pairs [][2]int, s float64, limit int) map[int]int {
	uniq := make(map[int]struct{}, len(raw))
	for _, v := range raw {
		uniq[v] = struct{}{}
	}
	sorted := make([]int, 0, len(uniq))
	for v := range uniq {
		sorted = append(sorted, v)
	}
	slices.Sort(sorted)

	parent := make(map[int]int, len(sorted))
	for _, v := range sorted {
		parent[v] = v
	}
	find := func(v int) int {
		for parent[v] != v {
			parent[v] = parent[parent[v]]
			v = parent[v]
		}
		return v
	}
	for _, p := range pairs {
		if a, b := find(p[0]), find(p[1]); a != b {
			parent[a] = b
		}
	}

	// sorted ascends, so the last write per class is its largest member.
	classMax := make(map[int]int, len(sorted))
	for _, v := range sorted {
		classMax[find(v)] = v
	}

	first, last := find(sorted[0]), find(sorted[len(sorted)-1])
	m := make(map[int]int, len(sorted))
	for _, v := range sorted {
		switch find(v) {
		case first:
			m[v] = 0
		case last:
			m[v] = limit
		default:
			m[v] = int(math.Round(float64(classMax[find(v)]) * s))
		}
	}
	return m
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

func clamp(v, hi int) int {
	return min(max(v, 0), hi)
}
