// Package picker renders a Bubble Tea TUI over tmux-state events.
package picker

import (
	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

// NodeKind identifies the level of a TreeNode.
type NodeKind int

const (
	NodeSession NodeKind = iota
	NodeWindow
	NodePane
)

// TreeNode is one entry in the rendered tree. BuildTree returns the root with
// Kind=NodeSession at depth 0; the spec mockup wraps these in a synthetic
// "Snapshot #N" header which lives in the View, not here.
type TreeNode struct {
	Kind       NodeKind
	Label      string // display label without skip styling
	Ref        any    // *snapshot.Session | *snapshot.Window | *snapshot.Pane
	Children   []*TreeNode
	Expanded   bool   // initial state set by BuildTree (sessions+windows true, panes false)
	Skipped    bool   // set by FilterDecorate
	SkipReason string // "idle shell" | "running" | "" (empty when kept)
}

// Counts is what FilterDecorate returns so the View can render
// "<KeptPanes> panes / <SkippedPanes> skipped" in the footer.
type Counts struct {
	KeptSessions, KeptWindows, KeptPanes          int
	SkippedSessions, SkippedWindows, SkippedPanes int
}

// BuildTree converts a manifest into a virtual root whose Children are session
// nodes. The root itself has Kind=NodeSession and Label="" — callers iterate
// root.Children rather than rendering the root.
func BuildTree(m snapshot.Manifest) *TreeNode {
	return nil // intentionally unimplemented; Task 3 fills this in
}

// FilterDecorate walks the tree and marks each node Skipped/SkipReason based on
// the filter predicates. It mutates the tree in place. Returns counts for the
// footer counter. The runningSessions argument is consulted only when
// f.DedupRunningServer is true.
func FilterDecorate(root *TreeNode, f filter.Filter, runningSessions map[string]bool) Counts {
	return Counts{} // intentionally unimplemented; Task 5 fills this in
}
