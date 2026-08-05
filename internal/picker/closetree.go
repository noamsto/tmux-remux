package picker

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// CloseNodeKind identifies the level of a CloseNode.
type CloseNodeKind int

// Close-node kinds. The two Group kinds are the top-level section headers.
const (
	GroupThis CloseNodeKind = iota
	GroupOther
	CSession
	CWindow
	CPane
)

// ClosePlacement locates a close event in the tmux hierarchy so the tree can
// nest it. Filled by the caller from the resolved closeevent.ClosedItem plus
// the event's own stored session name.
type ClosePlacement struct {
	Session     string
	WindowIndex int
	WindowName  string
	Scope       string // "session" | "window" | "pane"
	PaneCount   int    // panes in the closed window; 0 for other scopes
}

// CloseNode is one row of the close picker's tree.
//
// A node is either an event or a header. An event node carries Ts and EventID
// and can be restored; a header node exists only to parent something else and
// carries State ("live" | "gone") instead. A window or session node can be
// both: a session close that also parents older window closes, for instance.
type CloseNode struct {
	Kind     CloseNodeKind
	Label    string
	Ts       int64  // 0 on a pure header
	EventID  int64  // 0 = not selectable
	State    string // "live" | "gone" | ""
	Parent   *CloseNode
	Children []*CloseNode
	Expanded bool
}

// IsCloseGroup reports whether n is one of the two top-level section headers.
// Guides are not drawn for these, and the walk that builds a child's guide
// prefix stops here.
func IsCloseGroup(n *CloseNode) bool {
	return n.Kind == GroupThis || n.Kind == GroupOther
}

// BuildCloseTree groups recoverable close events under "this session" and
// "other sessions".
//
// The current session gets no session node: its group header already names it,
// so its windows hang directly off the group. Only other sessions need the
// extra level.
//
// evs is expected newest-first (store.ListEvents orders by ts DESC), but the
// result does not depend on that — every level is sorted by newest descendant
// at the end.
func BuildCloseTree(evs []store.Event, ctxs map[int64]CloseContext, current string, live map[string]bool) *CloseNode {
	root := &CloseNode{Expanded: true}
	thisGroup := &CloseNode{Kind: GroupThis, Label: "this session · " + current, Parent: root, Expanded: true}
	otherGroup := &CloseNode{Kind: GroupOther, Label: "other sessions", Parent: root}

	sessions := map[string]*CloseNode{}
	windows := map[string]*CloseNode{}

	for _, ev := range evs {
		cc, ok := ctxs[ev.ID]
		// No context, or nothing to restore: the caller counts these as hidden
		// rather than listing a dead row.
		if !ok || len(cc.SubManifest.Sessions) == 0 {
			continue
		}
		p := cc.Placement
		name := p.Session
		if name == "" {
			name = closeevent.UnknownSession
		}
		// A session close means that session is not the one we are sitting in,
		// so it always belongs to the other group — which also keeps the
		// this-group header from ever having to carry an event id.
		mine := current != "" && name == current && p.Scope != "session"

		if p.Scope == "session" {
			n := sessionNode(sessions, otherGroup, name, live)
			n.Ts, n.EventID, n.State = ev.Ts, ev.ID, ""
			continue
		}

		parent := thisGroup
		if !mine {
			parent = sessionNode(sessions, otherGroup, name, live)
		}
		wkey := name + "\x1f" + strconv.Itoa(p.WindowIndex) + "\x1f" + p.WindowName
		w, ok := windows[wkey]
		if !ok {
			// A pure header window: it exists because a pane died inside it and
			// no close event for the window itself was recorded, so the window
			// is still there whenever its session is.
			state := "gone"
			if live[name] {
				state = "live"
			}
			w = &CloseNode{
				Kind:   CWindow,
				Label:  closeWindowLabel(p.WindowIndex, p.WindowName, 0),
				State:  state,
				Parent: parent,
			}
			parent.Children = append(parent.Children, w)
			windows[wkey] = w
		}
		if p.Scope == "window" {
			w.Ts, w.EventID, w.State = ev.Ts, ev.ID, ""
			w.Label = closeWindowLabel(p.WindowIndex, p.WindowName, p.PaneCount)
			continue
		}
		w.Children = append(w.Children, &CloseNode{
			Kind: CPane, Label: cc.Label, Ts: ev.Ts, EventID: ev.ID, Parent: w,
		})
	}

	for _, g := range []*CloseNode{thisGroup, otherGroup} {
		if len(g.Children) == 0 {
			continue
		}
		sortCloseTree(g)
		// A close event and the detail nested under it (say, the pane that died
		// inside a window that later got closed) are meant to be visible together
		// from the start, so everything below the group header starts expanded.
		// The group header itself keeps the expansion state set above.
		for _, child := range g.Children {
			expandCloseSubtree(child)
		}
		root.Children = append(root.Children, g)
	}
	return root
}

// expandCloseSubtree opens n and everything beneath it.
func expandCloseSubtree(n *CloseNode) {
	n.Expanded = true
	for _, c := range n.Children {
		expandCloseSubtree(c)
	}
}

// sessionNode returns the cached session node for name, creating it under
// group on first use.
func sessionNode(cache map[string]*CloseNode, group *CloseNode, name string, live map[string]bool) *CloseNode {
	if n, ok := cache[name]; ok {
		return n
	}
	state := "gone"
	if live[name] {
		state = "live"
	}
	n := &CloseNode{Kind: CSession, Label: name, State: state, Parent: group}
	group.Children = append(group.Children, n)
	cache[name] = n
	return n
}

// closeWindowLabel renders "<index>: <name>", with " (Np)" appended when the
// window itself was closed and its pane count is known.
func closeWindowLabel(index int, name string, panes int) string {
	label := fmt.Sprintf("%d: %s", index, snapshot.StripFormat(name))
	if panes > 0 {
		label += fmt.Sprintf(" (%dp)", panes)
	}
	return label
}

// newestTs returns the newest timestamp in the subtree rooted at n, so a header
// sorts by the freshest thing inside it.
func newestTs(n *CloseNode) int64 {
	t := n.Ts
	for _, c := range n.Children {
		if ct := newestTs(c); ct > t {
			t = ct
		}
	}
	return t
}

// sortCloseTree orders every level newest-first. The root's children are the
// two groups and keep their fixed order, so callers sort each group instead.
func sortCloseTree(n *CloseNode) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		return newestTs(n.Children[i]) > newestTs(n.Children[j])
	})
	for _, c := range n.Children {
		sortCloseTree(c)
	}
}

// FlattenClose returns the visible rows of the tree, honoring Expanded. The
// synthetic root is not included.
func FlattenClose(root *CloseNode) []*CloseNode {
	if root == nil {
		return nil
	}
	var out []*CloseNode
	var walk func(n *CloseNode)
	walk = func(n *CloseNode) {
		out = append(out, n)
		if !n.Expanded {
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, g := range root.Children {
		walk(g)
	}
	return out
}
