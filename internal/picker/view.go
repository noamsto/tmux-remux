package picker

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// View renders the full picker UI. Called by Bubble Tea after every Update.
func (m PickerModel) View() tea.View {
	if m.showHelp {
		v := tea.NewView(m.help.View(m.keys))
		v.AltScreen = true
		return v
	}

	listWidth, treeWidth := m.paneWidths()
	list := renderList(m, listWidth)

	var content string
	if m.mode == ModeClose || m.width < 80 {
		// Close mode and narrow mode: list-only at this scale.
		content = lipgloss.JoinVertical(lipgloss.Top, list, m.renderFooter(m.width))
	} else {
		tree := renderTree(m, treeWidth)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree)
		content = lipgloss.JoinVertical(lipgloss.Top, body, m.renderFooter(m.width))
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// renderFooter renders the footer bar with toggle indicators, pane counter, and
// an optional transient warning note.
func (m PickerModel) renderFooter(width int) string {
	on := func(b bool, label string) string {
		if b {
			return footerOn.Render("[" + label + ":●]")
		}
		return footerOff.Render("[" + label + ":◯]")
	}
	c := m.CurrentCounts()
	counter := fmt.Sprintf("%d panes / %d skipped", c.KeptPanes, c.SkippedPanes)
	parts := []string{
		on(m.filter.SkipIdleShells, "skip idle"),
		on(m.filter.DedupRunningServer, "dedup running"),
		on(m.dimOlderThan > 0, "age≤24h"),
		"  " + counter,
		"  ↵ restore",
	}
	line := strings.Join(parts, "  ")
	if m.footerNote != "" {
		line = footerWarn.Render(m.footerNote) + "  " + line
	}
	return footerBar.Width(width).Render(line)
}

// paneWidths splits the available width between list and tree. Returns
// (listWidth, 0) when narrow or close mode.
func (m PickerModel) paneWidths() (int, int) {
	if m.width < 80 || m.mode == ModeClose {
		return m.width, 0
	}
	listW := m.width / 3
	if listW < 28 {
		listW = 28
	}
	treeW := m.width - listW
	return listW, treeW
}

func renderList(m PickerModel, width int) string {
	var b strings.Builder
	if len(m.events) == 0 {
		b.WriteString(rowDim.Render("No snapshots yet — run `tmux-state save`."))
		return listFrame.Width(width).Render(b.String())
	}
	now := time.Now()
	for i, ev := range m.events {
		ts := time.UnixMilli(ev.Ts).Format("01-02 15:04")
		reason := shortReason(ev.Reason)
		line := fmt.Sprintf("#%d %s %s", ev.ID, ts, reason)
		dim := m.dimOlderThan > 0 && now.Sub(time.UnixMilli(ev.Ts)) > m.dimOlderThan
		style := rowDefault
		switch {
		case i == m.cursor:
			style = rowActive
		case dim:
			style = rowDim
		}
		b.WriteString(style.Width(width - 2).Render(line))
		b.WriteString("\n")
	}
	return listFrame.Width(width).Render(strings.TrimRight(b.String(), "\n"))
}

func renderTree(m PickerModel, width int) string {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return treeFrame.Width(width).Render("")
	}
	ev := m.events[m.cursor]
	if err, bad := m.manifestErrors[ev.ID]; bad {
		return treeFrame.Width(width).Render(footerWarn.Render("(invalid manifest)") + "\n" + skipReason.Render(err.Error()))
	}
	tree := m.trees[ev.ID]
	if tree == nil {
		return treeFrame.Width(width).Render(rowDim.Render("(loading...)"))
	}
	if len(tree.Children) == 0 {
		return treeFrame.Width(width).Render(rowDim.Render("(empty snapshot)"))
	}

	var b strings.Builder
	header := fmt.Sprintf("Contents (#%d)", ev.ID)
	b.WriteString(lipgloss.NewStyle().Foreground(colBlue).Bold(true).Render(header))
	b.WriteString("\n")

	highlightIdx := -1
	if m.focus == focusTree {
		highlightIdx = m.treeCursor
	}
	idx := 0
	for _, sess := range tree.Children {
		writeNode(&b, sess, 0, &idx, highlightIdx)
	}
	return treeFrame.Width(width).Render(strings.TrimRight(b.String(), "\n"))
}

// writeNode appends a rendered row for n and its visible descendants.
// idx tracks the position in the flat visible-node list; *idx is incremented
// for each row written. highlightIdx is the target row to highlight (-1 = none).
func writeNode(b *strings.Builder, n *TreeNode, depth int, idx *int, highlightIdx int) {
	indent := strings.Repeat("  ", depth)
	bullet := "•"
	if len(n.Children) > 0 {
		if n.Expanded {
			bullet = "▾"
		} else {
			bullet = "▸"
		}
	}
	label := n.Label
	style := nodeKept
	if n.Skipped {
		style = nodeSkipped
		if n.SkipReason != "" {
			label = label + "  " + skipReason.Render("("+n.SkipReason+")")
		}
	}
	rendered := fmt.Sprintf("%s%s %s", indent, bullet, style.Render(label))
	if *idx == highlightIdx {
		rendered = rowActive.Render(rendered)
	}
	b.WriteString(rendered)
	b.WriteString("\n")
	*idx++
	if n.Expanded {
		for _, c := range n.Children {
			writeNode(b, c, depth+1, idx, highlightIdx)
		}
	}
}

// shortReason truncates "hook:window-linked" to "wlink", "timer" to "timer",
// "keybinding" to "key". Best-effort; falls back to the first 8 chars.
func shortReason(r string) string {
	switch r {
	case "timer":
		return "timer"
	case "keybinding":
		return "key"
	case "hook:window-linked":
		return "wlink"
	case "hook:session-created":
		return "screat"
	case "hook:client-detached":
		return "cdet"
	}
	if len(r) > 8 {
		return r[:8]
	}
	return r
}
