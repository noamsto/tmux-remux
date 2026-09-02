// Package panemap turns a tmux window layout string into box art. It knows
// nothing about snapshots or the picker: callers hand it a layout string and
// callbacks for labelling, and get back lines of text.
package panemap

import (
	"errors"
	"regexp"
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
type Grid struct {
	W, H  int
	Panes []Rect
}

// ErrNoLayout reports a layout string this package cannot draw: empty, from a
// snapshot written before layouts were stored, or malformed.
var ErrNoLayout = errors.New("panemap: no usable layout")

// tmux writes "<checksum>,<WxH>,<X>,<Y>" followed by nested [] (rows) and {}
// (columns) groups. A tuple with a fifth field is a pane; a tuple followed by a
// bracket is a container, and its children carry absolute coordinates already,
// so only the pane tuples matter.
var (
	winRe  = regexp.MustCompile(`^(\d+)x(\d+),(\d+),(\d+)`)
	paneRe = regexp.MustCompile(`(\d+)x(\d+),(\d+),(\d+),(\d+)`)
)

// Parse extracts the window size and every pane rectangle from a tmux layout
// string.
func Parse(layout string) (Grid, error) {
	_, body, found := strings.Cut(layout, ",")
	if !found {
		return Grid{}, ErrNoLayout
	}
	wm := winRe.FindStringSubmatch(body)
	if wm == nil {
		return Grid{}, ErrNoLayout
	}
	g := Grid{W: atoi(wm[1]), H: atoi(wm[2])}
	for _, m := range paneRe.FindAllStringSubmatch(body, -1) {
		g.Panes = append(g.Panes, Rect{
			W:     atoi(m[1]),
			H:     atoi(m[2]),
			X:     atoi(m[3]),
			Y:     atoi(m[4]),
			Index: atoi(m[5]),
		})
	}
	if g.W <= 0 || g.H <= 0 || len(g.Panes) == 0 {
		return Grid{}, ErrNoLayout
	}
	return g, nil
}

// atoi is safe without an error check: every call site passes a regexp capture
// group that matched \d+.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
