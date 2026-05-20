// Package picker renders a Bubble Tea TUI over tmux-state events.
package picker

import (
	"fmt"
	"os"
	"strings"

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
	root := &TreeNode{Kind: NodeSession, Expanded: true}
	for i := range m.Sessions {
		s := &m.Sessions[i]
		sessionNode := &TreeNode{
			Kind:     NodeSession,
			Label:    sessionLabel(s),
			Ref:      s,
			Expanded: true,
		}
		for j := range s.Windows {
			w := &s.Windows[j]
			windowNode := &TreeNode{
				Kind:     NodeWindow,
				Label:    windowLabel(w),
				Ref:      w,
				Expanded: true,
			}
			for k := range w.Panes {
				p := &w.Panes[k]
				windowNode.Children = append(windowNode.Children, &TreeNode{
					Kind:     NodePane,
					Label:    paneLabel(p),
					Ref:      p,
					Expanded: false,
				})
			}
			sessionNode.Children = append(sessionNode.Children, windowNode)
		}
		root.Children = append(root.Children, sessionNode)
	}
	return root
}

func sessionLabel(s *snapshot.Session) string {
	return fmt.Sprintf("%s (%dw)", s.Name, len(s.Windows))
}

func windowLabel(w *snapshot.Window) string {
	return fmt.Sprintf("%d: %s (%dp)", w.Index, w.Name, len(w.Panes))
}

func paneLabel(p *snapshot.Pane) string {
	cwd := p.Cwd
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + strings.TrimPrefix(cwd, home)
	}
	cmd := p.Command
	if cmd == "" {
		cmd = "(none)"
	}
	return fmt.Sprintf("%-7s %s", cmd, cwd)
}

// FilterDecorate walks the tree and marks each node Skipped/SkipReason based on
// the filter predicates. It mutates the tree in place. Returns counts for the
// footer counter. The runningSessions argument is consulted only when
// f.DedupRunningServer is true.
func FilterDecorate(root *TreeNode, f filter.Filter, runningSessions map[string]bool) Counts {
	return Counts{} // intentionally unimplemented; Task 5 fills this in
}
