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
		return tea.NewView(m.help.View(m.keys))
	}

	listWidth, treeWidth := m.paneWidths()
	list := renderList(m, listWidth)

	if m.mode == ModeClose || m.width < 80 {
		// Close mode and narrow mode: list-only at this scale.
		return tea.NewView(lipgloss.JoinVertical(lipgloss.Top, list, m.renderFooter(m.width)))
	}

	_ = treeWidth // tree pane lands in Task 14
	body := list  // for now; Task 14 adds the joined tree
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Top, body, m.renderFooter(m.width)))
}

// renderFooter is implemented as a stub here; full logic lands in Task 15.
func (m PickerModel) renderFooter(width int) string {
	if m.footerNote != "" {
		return footerWarn.Render(m.footerNote)
	}
	return ""
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
