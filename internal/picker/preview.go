package picker

import (
	"context"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/tmux-state/internal/scrollback"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

// scrollbackLoadedMsg is emitted by loadScrollbackCmd when the scrollback read
// completes (successfully or not). The model handles it by populating the
// scrollback cache and refreshing the viewport if the cursor still points at
// the same SHA.
type scrollbackLoadedMsg struct {
	sha     string
	content []byte
	err     error
}

// renderPreview renders the right-most preview pane. width is the cell budget
// (including the rounded border). Height comes from m.height.
func (m PickerModel) renderPreview(width int) string {
	// Inside-frame height: total height minus footer (1) minus border top/bottom (2).
	innerHeight := m.height - 3
	if innerHeight < 3 {
		innerHeight = 3
	}
	frame := previewFrame.Width(width).Height(innerHeight)

	if m.focus != focusTree {
		return frame.Render(rowDim.Render("(focus a pane to preview)"))
	}
	nodes := m.visibleNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return frame.Render(rowDim.Render("(focus a pane to preview)"))
	}
	n := nodes[m.treeCursor]
	if n.Kind != NodePane {
		return frame.Render(rowDim.Render("(focus a pane to preview)"))
	}
	p, _ := n.Ref.(*snapshot.Pane)
	if p == nil || p.ScrollbackSHA == "" {
		return frame.Render(rowDim.Render("(no scrollback recorded)"))
	}
	sha := p.ScrollbackSHA
	if err := m.scrollbackErrors[sha]; err != nil {
		return frame.Render(footerWarn.Render("(scrollback unavailable: " + err.Error() + ")"))
	}
	content, ok := m.scrollbacks[sha]
	if !ok {
		if m.loadingSHAs[sha] {
			return frame.Render(rowDim.Render("(loading...)"))
		}
		// Not loading yet — PreviewCmd will schedule on next key event.
		return frame.Render(rowDim.Render("(preview pending)"))
	}
	return frame.Render(tailLines(string(content), innerHeight))
}

// tailLines returns the last n lines of s, joined by "\n". If s has fewer
// lines, all are returned. ANSI escapes are preserved verbatim — the saved
// scrollback already encodes terminal-state for replay.
func tailLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// loadScrollbackCmd returns a tea.Cmd that reads the scrollback for sha off
// the UI goroutine. Returns nil if sb is nil or sha is empty (caller short-
// circuits and never schedules a load).
func loadScrollbackCmd(sb *scrollback.Store, sha string) tea.Cmd {
	if sb == nil || sha == "" {
		return nil
	}
	return func() tea.Msg {
		rc, err := sb.Stream(context.Background(), sha)
		if err != nil {
			return scrollbackLoadedMsg{sha: sha, err: err}
		}
		defer func() { _ = rc.Close() }()
		buf, err := io.ReadAll(rc)
		return scrollbackLoadedMsg{sha: sha, content: buf, err: err}
	}
}
