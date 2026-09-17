package picker

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// paneZone names the pane a terminal cell falls in, so a wheel or click can be
// routed to what the user is pointing at.
type paneZone int

const (
	zoneNone paneZone = iota
	zoneList
	zoneTree
	zonePreview
)

// zoneAt maps a terminal cell to the pane that owns it and the cell's row
// inside that pane's frame (0 is the first content row under the top border,
// −1 the border itself). It mirrors View's layout decisions, so input lands on
// the pane the user sees rather than one recomputed differently.
func (m PickerModel) zoneAt(x, y int) (paneZone, int) {
	if m.width <= 0 || m.height <= 0 || x < 0 || y < 0 {
		return zoneNone, 0
	}
	bodyH := m.height - lipgloss.Height(m.renderFooter(m.width))
	if y >= bodyH {
		return zoneNone, 0 // the footer
	}
	listW, treeW, previewW := m.paneWidthsThree()

	// Stacked layouts put the preview under the list; snapshot mode splits the
	// top region between the list and the tree.
	if previewW == 0 && m.stacksPanel() {
		topH := bodyH - m.panelFrameHeight()
		if y < topH {
			if m.mode == ModeSnapshot && x >= listW {
				return zoneTree, y - 1
			}
			return zoneList, y - 1
		}
		return zonePreview, y - topH - 1
	}
	if m.width < 80 {
		return zoneList, y - 1
	}
	switch {
	case x < listW:
		return zoneList, y - 1
	case m.mode == ModeClose:
		// Close mode has no tree column; everything past the list is preview.
		return zonePreview, y - 1
	case x < listW+treeW:
		return zoneTree, y - 1
	default:
		return zonePreview, y - 1
	}
}

// handleWheel scrolls whatever pane the pointer is over: the list's cursor,
// the tree's cursor, or the preview's scrollback. Vertical only — the wheel
// has no natural horizontal axis on most mice, and M-h/M-l own that.
func (m *PickerModel) handleWheel(button tea.MouseButton, x, y int) tea.Cmd {
	zone, _ := m.zoneAt(x, y)
	switch zone {
	case zoneList:
		switch button {
		case tea.MouseWheelUp:
			return m.moveListCursor(-wheelRows)
		case tea.MouseWheelDown:
			return m.moveListCursor(+wheelRows)
		}
	case zoneTree:
		switch button {
		case tea.MouseWheelUp:
			return m.moveTreeCursor(-wheelRows)
		case tea.MouseWheelDown:
			return m.moveTreeCursor(+wheelRows)
		}
	case zonePreview:
		switch button {
		case tea.MouseWheelUp:
			maxScroll := m.previewMaxScroll(m.paneScrollbackHeight())
			m.previewScroll += wheelRows
			if m.previewScroll > maxScroll {
				m.previewScroll = maxScroll
			}
		case tea.MouseWheelDown:
			m.previewScroll -= wheelRows
			if m.previewScroll < 0 {
				m.previewScroll = 0
			}
		case tea.MouseWheelLeft:
			m.previewScrollX -= wheelRows
			if m.previewScrollX < 0 {
				m.previewScrollX = 0
			}
		case tea.MouseWheelRight:
			m.previewScrollX += wheelRows
		}
	}
	return nil
}

// wheelRows is how far one wheel notch scrolls — the same step tmux uses for
// copy-mode, and large enough that a scrollback is traversable.
const wheelRows = 3

// moveListCursor walks the focused list by delta rows, skipping section headers
// in close mode and clamping at the ends in snapshot mode. Any move resets the
// preview to the tail of the newly selected row's scrollback.
func (m *PickerModel) moveListCursor(delta int) tea.Cmd {
	if m.mode == ModeClose {
		for i := 0; i < absInt(delta); i++ {
			idx := m.nextCloseRowIdx(m.cursor, sign(delta))
			if idx < 0 {
				break
			}
			m.cursor = idx
		}
		m.previewScroll, m.previewScrollX = 0, 0
		return m.PreviewCmd()
	}
	if len(m.events) == 0 {
		return nil
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.events)-1 {
		m.cursor = len(m.events) - 1
	}
	m.ensureManifest()
	m.treeCursor = m.firstPaneIdx()
	m.previewScroll, m.previewScrollX = 0, 0
	return m.PreviewCmd()
}

// moveTreeCursor walks the snapshot tree's focus cursor by delta navigable
// rows and gives the tree focus, the same as a Tab into it.
func (m *PickerModel) moveTreeCursor(delta int) tea.Cmd {
	if m.mode != ModeSnapshot {
		return nil
	}
	if m.treeCursor < 0 {
		m.treeCursor = m.firstPaneIdx()
	}
	for i := 0; i < absInt(delta); i++ {
		idx := m.nextPaneIdx(m.treeCursor, sign(delta))
		if idx < 0 {
			break
		}
		m.treeCursor = idx
	}
	m.focus = focusTree
	m.previewScroll, m.previewScrollX = 0, 0
	return m.PreviewCmd()
}

// handleClick selects the list row or tree node under the pointer. Clicks on
// the preview or the frame do nothing: there is no cursor there to move.
func (m *PickerModel) handleClick(x, y int) tea.Cmd {
	zone, row := m.zoneAt(x, y)
	switch zone {
	case zoneList:
		return m.clickListRow(row)
	case zoneTree:
		return m.clickTreeRow(row)
	}
	return nil
}

// clickListRow maps a clicked content row to a close or event index using the
// same window the list was drawn from.
func (m *PickerModel) clickListRow(row int) tea.Cmd {
	if row < 0 {
		return nil
	}
	if m.mode == ModeClose {
		start, end, pin, _, _ := m.closeListWindow(m.listPaneHeight())
		base := 0
		if pin >= 0 {
			base = 1
		}
		idx := start + row - base
		if row < base || idx < 0 || idx >= end {
			return nil
		}
		if !m.closeRows[idx].Selectable() {
			return nil
		}
		m.cursor = idx
		m.previewScroll, m.previewScrollX = 0, 0
		return m.PreviewCmd()
	}
	start, end, eventRows, _ := m.listWindow(m.listPaneHeight())
	if row >= eventRows {
		return nil
	}
	idx := start + row
	if idx < 0 || idx >= end || idx >= len(m.events) {
		return nil
	}
	m.cursor = idx
	m.ensureManifest()
	m.treeCursor = m.firstPaneIdx()
	m.previewScroll, m.previewScrollX = 0, 0
	return m.PreviewCmd()
}

// clickTreeRow maps a clicked row to a visible tree node, below the one-row
// "Contents" header the tree draws first.
func (m *PickerModel) clickTreeRow(row int) tea.Cmd {
	if m.mode != ModeSnapshot || row < 1 {
		return nil
	}
	nodes := m.visibleNodes()
	if len(nodes) == 0 {
		return nil
	}
	highlight := -1
	if m.focus == focusTree {
		highlight = m.treeCursor
	}
	visible := m.listPaneHeight() - 3
	if visible < 1 {
		visible = 1
	}
	start, end := scrollWindow(highlight, len(nodes), visible)
	idx := start + row - 1
	if idx < 0 || idx >= end || idx >= len(nodes) || !isNavTarget(nodes[idx]) {
		return nil
	}
	m.focus = focusTree
	m.treeCursor = idx
	m.previewScroll, m.previewScrollX = 0, 0
	return m.PreviewCmd()
}

// listPaneHeight is the list's rendered frame height — the full body, or the
// top half when the preview is stacked beneath it. renderCloseList and
// renderTree are called with it, and click mapping must window by the same.
func (m PickerModel) listPaneHeight() int {
	if m.stacksPanel() {
		return m.bodyHeight() - m.panelFrameHeight()
	}
	return m.bodyHeight()
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
