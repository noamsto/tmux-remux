package picker_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/picker"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// ctx builds a CloseContext whose sub-manifest is non-empty (the recoverable
// gate) with the given placement.
func closeCtx(session string, idx int, winName, scope string, panes int) picker.CloseContext {
	return picker.CloseContext{
		Label: scope + " label",
		Placement: picker.ClosePlacement{
			Session: session, WindowIndex: idx, WindowName: winName,
			Scope: scope, PaneCount: panes,
		},
		SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{Name: session}}},
	}
}

// childLabels returns the labels of n's children, in order.
func childLabels(n *picker.CloseNode) []string {
	out := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.Label)
	}
	return out
}

// findChild returns the child of n whose label matches, or nil.
func findChild(n *picker.CloseNode, label string) *picker.CloseNode {
	for _, c := range n.Children {
		if c.Label == label {
			return c
		}
	}
	return nil
}

func TestBuildCloseTree_PaneNestsUnderItsWindow(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 300, Kind: "window-unlinked"},
		{ID: 2, Ts: 200, Kind: "pane-died"},
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("mono", 2, "main", "pane", 0),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupThis {
		t.Fatalf("root children = %v, want a single this-session group", childLabels(root))
	}
	group := root.Children[0]
	if len(group.Children) != 1 {
		t.Fatalf("group children = %v, want one window node", childLabels(group))
	}
	win := group.Children[0]
	if win.Kind != picker.CWindow || win.EventID != 1 {
		t.Errorf("window node = %+v, want CWindow carrying event 1", win)
	}
	if len(win.Children) != 1 || win.Children[0].EventID != 2 || win.Children[0].Kind != picker.CPane {
		t.Errorf("window children = %+v, want the pane event nested", win.Children)
	}
}

func TestBuildCloseTree_LiveWindowIsAHeaderNotSelectable(t *testing.T) {
	evs := []store.Event{{ID: 2, Ts: 200, Kind: "pane-died"}}
	ctxs := map[int64]picker.CloseContext{2: closeCtx("mono", 5, "docs", "pane", 0)}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	win := root.Children[0].Children[0]
	if win.EventID != 0 {
		t.Errorf("header window EventID = %d, want 0 (not selectable)", win.EventID)
	}
	if win.State != "live" {
		t.Errorf("header window State = %q, want \"live\"", win.State)
	}
}

func TestBuildCloseTree_GoneSessionHeader(t *testing.T) {
	evs := []store.Event{{ID: 3, Ts: 100, Kind: "pane-died"}}
	ctxs := map[int64]picker.CloseContext{3: closeCtx("lazytmux", 0, "shell", "pane", 0)}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupOther {
		t.Fatalf("root children = %v, want only the other-sessions group", childLabels(root))
	}
	sess := root.Children[0].Children[0]
	if sess.Kind != picker.CSession || sess.Label != "lazytmux" {
		t.Fatalf("session node = %+v, want CSession lazytmux", sess)
	}
	if sess.State != "gone" {
		t.Errorf("State = %q, want \"gone\" (session is not live)", sess.State)
	}
	if sess.EventID != 0 {
		t.Errorf("EventID = %d, want 0 — no session-close event was recorded", sess.EventID)
	}
}

func TestBuildCloseTree_SessionCloseIsSelectableAndParentsOlderWindows(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 400, Kind: "session-closed"},
		{ID: 2, Ts: 100, Kind: "window-unlinked"},
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("lazytmux", 0, "", "session", 0),
		2: closeCtx("lazytmux", 1, "shell", "window", 2),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	sess := root.Children[0].Children[0]
	if sess.EventID != 1 {
		t.Errorf("session node EventID = %d, want 1 (the session close)", sess.EventID)
	}
	if len(sess.Children) != 1 || sess.Children[0].EventID != 2 {
		t.Errorf("session children = %+v, want the older window close nested", sess.Children)
	}
}

func TestBuildCloseTree_ThisSessionFirstAndExpanded(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 100, Kind: "window-unlinked"}, // older, but this session
		{ID: 2, Ts: 900, Kind: "window-unlinked"}, // newer, other session
	}
	ctxs := map[int64]picker.CloseContext{
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("lazytmux", 3, "docs", "window", 1),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true, "lazytmux": true})

	if len(root.Children) != 2 {
		t.Fatalf("root children = %v, want both groups", childLabels(root))
	}
	if root.Children[0].Kind != picker.GroupThis {
		t.Error("this-session group must come first even when another session has a newer close")
	}
	if !root.Children[0].Expanded {
		t.Error("this-session group must start expanded")
	}
	if root.Children[1].Expanded {
		t.Error("other-sessions group must start collapsed")
	}
}

func TestBuildCloseTree_SortsByNewestDescendant(t *testing.T) {
	evs := []store.Event{
		{ID: 1, Ts: 100, Kind: "window-unlinked"},
		{ID: 2, Ts: 500, Kind: "pane-died"},
		{ID: 3, Ts: 300, Kind: "window-unlinked"},
	}
	ctxs := map[int64]picker.CloseContext{
		// Window 2 has an OLD close but a NEW pane inside it, so its subtree is
		// the newest and it must sort ahead of window 4.
		1: closeCtx("mono", 2, "main", "window", 1),
		2: closeCtx("mono", 2, "main", "pane", 0),
		3: closeCtx("mono", 4, "docs", "window", 1),
	}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	group := root.Children[0]
	if len(group.Children) != 2 {
		t.Fatalf("group children = %v, want two windows", childLabels(group))
	}
	if group.Children[0].EventID != 1 {
		t.Errorf("first window EventID = %d, want 1 (newest descendant wins)", group.Children[0].EventID)
	}
}

func TestBuildCloseTree_UnattributedEventGroupsUnderUnknown(t *testing.T) {
	evs := []store.Event{{ID: 1, Ts: 100, Kind: "window-unlinked"}}
	ctxs := map[int64]picker.CloseContext{1: closeCtx("", 2, "main", "window", 1)}

	root := picker.BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupOther {
		t.Fatalf("root children = %v, want the other-sessions group", childLabels(root))
	}
	if got := root.Children[0].Children[0].Label; got != closeevent.UnknownSession {
		t.Errorf("session label = %q, want %q", got, closeevent.UnknownSession)
	}
}

func TestBuildCloseTree_NoSessionContextPutsEverythingInOther(t *testing.T) {
	evs := []store.Event{{ID: 1, Ts: 100, Kind: "window-unlinked"}}
	ctxs := map[int64]picker.CloseContext{1: closeCtx("mono", 2, "main", "window", 1)}

	root := picker.BuildCloseTree(evs, ctxs, "", map[string]bool{"mono": true})

	if len(root.Children) != 1 || root.Children[0].Kind != picker.GroupOther {
		t.Fatalf("root children = %v, want only the other-sessions group", childLabels(root))
	}
	if findChild(root.Children[0], "mono") == nil {
		t.Error("expected a session node for mono")
	}
}

func TestBuildCloseTree_SkipsUnrecoverableEvents(t *testing.T) {
	evs := []store.Event{{ID: 1, Ts: 100, Kind: "window-unlinked"}}
	// No entry in ctxs at all: the event never resolved to an entity.
	root := picker.BuildCloseTree(evs, map[int64]picker.CloseContext{}, "mono", map[string]bool{"mono": true})

	if len(root.Children) != 0 {
		t.Errorf("root children = %v, want none", childLabels(root))
	}
}
