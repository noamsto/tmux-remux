// Package panemap turns a tmux window layout string into box art. It knows
// nothing about snapshots or the picker: callers hand it a layout string and
// callbacks for labelling, and get back lines of text.
package panemap

import (
	"errors"
	"strconv"
	"strings"
)

// Rect is one pane's geometry in window cells, as tmux recorded it.
type Rect struct {
	Index      int
	W, H, X, Y int
}

// Grid is a window's own size plus every pane rectangle inside it. Pane
// coordinates are absolute within the window, not relative to a parent split.
// The split tree that produced them is kept privately in root; rendering needs
// it to know which two panes a divider separates, which a flat rectangle list
// cannot say.
type Grid struct {
	W, H  int
	Panes []Rect
	root  *node
}

// ErrNoLayout reports a layout string this package cannot draw: empty, from a
// snapshot written before layouts were stored, or malformed.
var ErrNoLayout = errors.New("panemap: no usable layout")

// kind distinguishes a leaf pane from the two ways tmux nests containers.
type kind int

const (
	leafKind kind = iota
	rowsKind      // "[...]": children stacked top-to-bottom, dividing the Y axis
	colsKind      // "{...}": children side by side, dividing the X axis
)

// node is one cell of the layout tree: a leaf pane, or a container whose
// children partition its rectangle along one axis. Coordinates are absolute
// window cells, as the layout string records them.
type node struct {
	x, y, w, h int
	index      int // leaf only
	kind       kind
	children   []*node
}

// Parse reads a tmux layout string into the split tree and, from its leaves in
// order, the flat pane list. tmux writes "<checksum>,<cell>" where a cell is
// "<WxH>,<X>,<Y>" followed by ",<index>" for a pane or a "[]"/"{}" group for a
// container.
func Parse(layout string) (Grid, error) {
	_, body, found := strings.Cut(layout, ",")
	if !found {
		return Grid{}, ErrNoLayout
	}
	p := &parser{s: body}
	root := p.cell()
	if p.err || p.pos != len(body) || root.w <= 0 || root.h <= 0 {
		return Grid{}, ErrNoLayout
	}
	g := Grid{W: root.w, H: root.h, root: root}
	appendLeaves(root, &g.Panes)
	if len(g.Panes) == 0 {
		return Grid{}, ErrNoLayout
	}
	return g, nil
}

// appendLeaves collects the tree's leaf panes left-to-right, depth first, which
// is the order tmux writes them and the order the picker expects.
func appendLeaves(n *node, out *[]Rect) {
	if n.kind == leafKind {
		*out = append(*out, Rect{Index: n.index, W: n.w, H: n.h, X: n.x, Y: n.y})
		return
	}
	for _, c := range n.children {
		appendLeaves(c, out)
	}
}

// parser is a cursor over the layout body (everything after the checksum). On
// any malformed input it sets err and stops advancing meaningfully; the caller
// checks err rather than each step.
type parser struct {
	s   string
	pos int
	err bool
}

// cell parses one "<WxH>,<X>,<Y>" tuple and whatever follows it: a "[" or "{"
// group makes it a container, a "," makes it a leaf carrying a pane index.
func (p *parser) cell() *node {
	w := p.readInt()
	p.expect('x')
	h := p.readInt()
	p.expect(',')
	x := p.readInt()
	p.expect(',')
	y := p.readInt()
	n := &node{x: x, y: y, w: w, h: h}
	switch p.peek() {
	case '[':
		p.pos++
		n.kind = rowsKind
		n.children = p.cellList()
		p.expect(']')
	case '{':
		p.pos++
		n.kind = colsKind
		n.children = p.cellList()
		p.expect('}')
	case ',':
		p.pos++
		n.kind = leafKind
		n.index = p.readInt()
	default:
		p.err = true
	}
	return n
}

// cellList parses one or more comma-separated cells, the children of a
// container, stopping at the closing bracket.
func (p *parser) cellList() []*node {
	children := []*node{p.cell()}
	for p.peek() == ',' {
		p.pos++
		children = append(children, p.cell())
	}
	return children
}

func (p *parser) readInt() int {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		p.err = true
		return 0
	}
	n, _ := strconv.Atoi(p.s[start:p.pos])
	return n
}

func (p *parser) expect(c byte) {
	if p.peek() == c {
		p.pos++
		return
	}
	p.err = true
}

func (p *parser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}
