package panemap

import (
	"fmt"
	"math"
	"sort"
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

// render draws the art unconditionally. Separate from Render so tests can assert
// exact output at sizes smaller than the guard allows.
func render(g Grid, w, h int, _ func(int) string, _ func(int) bool) string {

	boxes := scale(g, w, h)

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

// scale maps window cells onto panel cells. w-1 and h-1 because a box's right
// and bottom borders sit *on* the last column and row.
func scale(g Grid, w, h int) []box {
	sx := float64(w-1) / float64(g.W)
	sy := float64(h-1) / float64(g.H)

	xBounds := make([]int, 0, 2*len(g.Panes))
	yBounds := make([]int, 0, 2*len(g.Panes))
	for _, p := range g.Panes {
		xBounds = append(xBounds, p.X, p.X+p.W)
		yBounds = append(yBounds, p.Y, p.Y+p.H)
	}
	xMap := scaleBoundaries(xBounds, sx)
	yMap := scaleBoundaries(yBounds, sy)

	boxes := make([]box, 0, len(g.Panes))
	for _, p := range g.Panes {
		b := box{
			x0:  clamp(xMap[p.X], w-1),
			y0:  clamp(yMap[p.Y], h-1),
			x1:  clamp(xMap[p.X+p.W], w-1),
			y1:  clamp(yMap[p.Y+p.H], h-1),
			idx: p.Index,
		}
		// A pane that rounds to nothing still deserves a visible box.
		if b.x1 <= b.x0 {
			b.x1 = min(b.x0+1, w-1)
		}
		if b.y1 <= b.y0 {
			b.y1 = min(b.y0+1, h-1)
		}
		boxes = append(boxes, b)
	}
	return boxes
}

// scaleBoundaries rounds each raw coordinate independently, except where two
// coordinates are 1 apart — a tmux separator gap — in which case both take the
// scaled position of the larger one. Without this, adjacent panes can round
// their shared edge to different rows/columns, breaking the divider merge in
// runeFor.
func scaleBoundaries(raw []int, s float64) map[int]int {
	uniq := make(map[int]struct{}, len(raw))
	for _, v := range raw {
		uniq[v] = struct{}{}
	}
	sorted := make([]int, 0, len(uniq))
	for v := range uniq {
		sorted = append(sorted, v)
	}
	sort.Ints(sorted)

	m := make(map[int]int, len(sorted))
	for i, v := range sorted {
		if i > 0 && v-sorted[i-1] == 1 {
			scaled := int(math.Round(float64(v) * s))
			m[sorted[i-1]] = scaled
			m[v] = scaled
			continue
		}
		m[v] = int(math.Round(float64(v) * s))
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
