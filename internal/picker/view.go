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

	listWidth, treeWidth, previewWidth := m.paneWidthsThree()
	// Reserve one row for the footer; everything else (border + content) goes
	// into the body frames. Without an explicit height, list/tree overflow
	// past the popup and push the footer off-screen.
	bodyHeight := m.height - 1
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	list := renderList(m, listWidth, bodyHeight)

	var content string
	switch {
	case m.width < 80:
		content = lipgloss.JoinVertical(lipgloss.Top, list, m.renderFooter(m.width))
	case m.mode == ModeClose:
		// Close mode: list + tree (showing the diff-derived sub-manifest of
		// what was lost). No scrollback preview — close events don't carry
		// pane scrollback.
		tree := renderTree(m, m.width-listWidth, bodyHeight)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree)
		content = lipgloss.JoinVertical(lipgloss.Top, body, m.renderFooter(m.width))
	case previewWidth == 0:
		tree := renderTree(m, treeWidth, bodyHeight)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree)
		content = lipgloss.JoinVertical(lipgloss.Top, body, m.renderFooter(m.width))
	default:
		tree := renderTree(m, treeWidth, bodyHeight)
		preview := m.renderPreview(previewWidth)
		body := lipgloss.JoinHorizontal(lipgloss.Top, list, tree, preview)
		content = lipgloss.JoinVertical(lipgloss.Top, body, m.renderFooter(m.width))
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderFooter renders the footer bar with toggle indicators, pane counter, and
// an optional transient warning note. Format: `key:value` pairs in lavender +
// state color, separated by a dim "·" so the eye can lock onto each pair.
func (m PickerModel) renderFooter(width int) string {
	toggle := func(b bool, key, label string) string {
		state := footerOff.Render("off")
		if b {
			state = footerOn.Render("on")
		}
		return footerKey.Render(key) + footerSep.Render(":") + state + footerSep.Render(" "+label)
	}
	hint := func(key, label string) string {
		return footerKey.Render(key) + footerSep.Render(":"+label)
	}
	sep := footerSep.Render(" · ")

	c := m.CurrentCounts()
	counter := fmt.Sprintf("%d panes / %d skipped", c.KeptPanes, c.SkippedPanes)

	parts := []string{
		toggle(m.filter.SkipIdleShells, "s", "skip idle"),
		toggle(m.filter.SkipRunningSessions, "d", "skip running"),
		toggle(m.dimOlderThan > 0, "a", "age≤24h"),
		counter,
		hint("↵", "restore"),
	}
	if m.width >= 120 && m.mode == ModeSnapshot {
		parts = append(parts, hint("tab", "tree"))
		parts = append(parts, hint("M-hjkl", "scroll preview"))
	}
	line := strings.Join(parts, sep)
	if m.footerNote != "" {
		line = footerWarn.Render(m.footerNote) + sep + line
	}
	return footerBar.Width(width).Render(line)
}

// paneWidthsThree splits the available width between list, tree, and preview.
// Returns (list, tree, preview) where preview==0 means the preview pane is
// hidden at this width (or in close mode).
func (m PickerModel) paneWidthsThree() (int, int, int) {
	if m.width < 80 {
		return m.width, 0, 0
	}
	if m.mode == ModeClose {
		// Close mode: list + tree (no scrollback preview). Give the list
		// ~40% so labels like "session: reviewtest2401692 (1w)" fit.
		listW := m.width * 2 / 5
		if listW < 32 {
			listW = 32
		}
		return listW, m.width - listW, 0
	}
	if m.width < 120 {
		// Two-pane fallback (current behavior).
		listW := m.width / 3
		if listW < 28 {
			listW = 28
		}
		return listW, m.width - listW, 0
	}
	// Three-pane: 1/4 list, 1/3 tree, remainder preview. At width ≥ 120 the
	// proportions guarantee previewW ≥ 50 and both min-clamps (28/32) are
	// already satisfied by the proportional values, so no squeeze is needed.
	listW := m.width / 4
	treeW := m.width / 3
	return listW, treeW, m.width - listW - treeW
}

func renderList(m PickerModel, width, height int) string {
	frame := listFrame.Width(width).Height(height)
	if len(m.events) == 0 {
		return frame.Render(rowDim.Render("No snapshots yet — run `tmux-state save`."))
	}
	// Inner content height = frame height − 2 (top+bottom border).
	rows := height - 2
	if rows < 1 {
		rows = 1
	}
	start, end := scrollWindow(m.cursor, len(m.events), rows)

	var b strings.Builder
	now := time.Now()
	for i := start; i < end; i++ {
		ev := m.events[i]
		ts := time.UnixMilli(ev.Ts).Format("01-02 15:04")
		var line string
		if m.mode == ModeClose {
			// Show the diff-derived label (e.g., "lazytmux/main 🧠 (1p)")
			// instead of the generic "hook"; falls back to the Kind when the
			// context lookup failed (no prior snapshot).
			label := m.closeContexts[ev.ID].Label
			if label == "" {
				label = ev.Kind
			}
			line = fmt.Sprintf("%s  %s", ts, label)
		} else {
			line = fmt.Sprintf("#%d %s %s", ev.ID, ts, shortReason(ev.Reason))
		}
		dim := m.dimOlderThan > 0 && now.Sub(time.UnixMilli(ev.Ts)) > m.dimOlderThan
		style := rowDefault
		switch {
		case i == m.cursor:
			style = rowActive
		case dim:
			style = rowDim
		}
		b.WriteString(style.Width(width - 2).Render(line))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return frame.Render(b.String())
}

// scrollWindow returns [start,end) such that `cursor` falls inside and the
// window length ≤ rows. Tries to keep the cursor centered; clamps at the ends
// of the list so a small list with many rows stays anchored at index 0.
func scrollWindow(cursor, total, rows int) (int, int) {
	if total <= rows {
		return 0, total
	}
	half := rows / 2
	start := cursor - half
	if start < 0 {
		start = 0
	}
	end := start + rows
	if end > total {
		end = total
		start = end - rows
	}
	return start, end
}

func renderTree(m PickerModel, width, height int) string {
	frame := treeFrame.Width(width).Height(height)
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return frame.Render("")
	}
	ev := m.events[m.cursor]
	if err, bad := m.manifestErrors[ev.ID]; bad {
		return frame.Render(footerWarn.Render("(invalid manifest)") + "\n" + skipReason.Render(err.Error()))
	}
	tree := m.trees[ev.ID]
	if tree == nil {
		return frame.Render(rowDim.Render("(loading...)"))
	}
	if len(tree.Children) == 0 {
		return frame.Render(rowDim.Render("(empty snapshot)"))
	}

	var b strings.Builder
	header := fmt.Sprintf("Contents (#%d)", ev.ID)
	b.WriteString(previewHeader.Render(header))
	b.WriteString("\n")

	highlightIdx := -1
	if m.focus == focusTree {
		highlightIdx = m.treeCursor
	}
	idx := 0
	var rows []string
	for _, sess := range tree.Children {
		appendNodeRows(&rows, sess, 0, &idx, highlightIdx)
	}

	// Header (1) + border (2) consume 3 rows inside the frame's height.
	visible := height - 3
	if visible < 1 {
		visible = 1
	}
	start, end := scrollWindow(highlightIdx, len(rows), visible)
	for i := start; i < end; i++ {
		b.WriteString(rows[i])
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return frame.Render(b.String())
}

// appendNodeRows appends one rendered string per visible node of the subtree
// rooted at n. idx tracks the position in the flat visible-node list and is
// incremented for each row appended. highlightIdx is the row to mark active
// (−1 = none). Caller windows the returned slice for scrolling.
func appendNodeRows(rows *[]string, n *TreeNode, depth int, idx *int, highlightIdx int) {
	indent := strings.Repeat("  ", depth)
	bullet := "•"
	if len(n.Children) > 0 {
		if n.Expanded {
			bullet = "▾"
		} else {
			bullet = "▸"
		}
	}
	active := *idx == highlightIdx
	var rendered string
	if active {
		// Active row gets a single flat style: lipgloss v2 strips ESC bytes
		// from pre-styled input, so nesting role-color inside rowActive's
		// mauve background can collapse to mauve-on-mauve = invisible. Render
		// once, plain.
		line := fmt.Sprintf("%s%s %s", indent, bullet, n.Label)
		if n.Skipped && n.SkipReason != "" {
			line = line + "  (" + n.SkipReason + ")"
		}
		rendered = rowActive.Render(line)
	} else {
		var style lipgloss.Style
		switch n.Kind {
		case NodeSession:
			style = nodeSession
		case NodeWindow:
			style = nodeWindow
		default:
			style = nodePane
		}
		if n.Skipped {
			// Keep the role color so the tree shape stays legible when
			// skip-running marks everything skipped; just dim it.
			style = style.Faint(true).Italic(true)
		}
		styled := style.Render(n.Label)
		if n.Skipped && n.SkipReason != "" {
			styled = styled + "  " + skipReason.Render("("+n.SkipReason+")")
		}
		rendered = fmt.Sprintf("%s%s %s", indent, bullet, styled)
	}
	*rows = append(*rows, rendered)
	*idx++
	if n.Expanded {
		for _, c := range n.Children {
			appendNodeRows(rows, c, depth+1, idx, highlightIdx)
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
