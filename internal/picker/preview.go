package picker

import (
	"context"
	"io"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/tmux-remux/internal/scrollback"
	"github.com/noamsto/tmux-remux/internal/snapshot"
)

// structuralANSI matches CSI sequences whose final byte is NOT 'm' (i.e.,
// everything except SGR color/style). These cursor-movement, erase, scroll,
// bracketed-paste-mode toggles confuse lipgloss when embedded in framed
// content — strip them but keep the SGR codes so previews stay colored.
var structuralANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-ln-~]`)

// oscANSI matches OSC sequences (\x1b]…\x07 or \x1b]…\x1b\). These set
// terminal title / hyperlinks; they also break frame rendering when raw.
var oscANSI = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// otherESC matches stray ESC-then-single-letter sequences (e.g., ESC=, ESC>,
// ESC(B) that escape lipgloss's parser.
var otherESC = regexp.MustCompile(`\x1b[()=>NMc78]`)

// sanitizeScrollback removes non-SGR escape sequences and control characters
// (NUL, BS, CR, VT, FF) that would otherwise break preview-frame rendering.
// SGR colors (\x1b[...m) and tab are left intact.
func sanitizeScrollback(s string) string {
	s = oscANSI.ReplaceAllString(s, "")
	s = structuralANSI.ReplaceAllString(s, "")
	s = otherESC.ReplaceAllString(s, "")
	s = strings.NewReplacer(
		"\x00", "", "\x08", "", "\x0b", "", "\x0c", "", "\r", "",
	).Replace(s)
	return s
}

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
	// Match the body height the rest of the layout uses (m.height - 1 for the
	// footer). previewFrame has Border (2 cells) + Padding(0,1) (2 cells)
	// → 4 cells total horizontal overhead and 2 vertical (border only).
	frameHeight := m.height - 1
	if frameHeight < 5 {
		frameHeight = 5
	}
	innerHeight := m.previewInnerHeight()
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	frame := previewFrame.Width(width).Height(frameHeight).MaxHeight(frameHeight)

	if m.focus != focusTree {
		return frame.Render(rowDim.Render("(press Tab to preview panes)"))
	}
	nodes := m.visibleNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return frame.Render(rowDim.Render("(no pane selected)"))
	}
	n := nodes[m.treeCursor]
	if n.Kind != NodePane {
		// Reachable after Left collapses to a window/session node.
		return frame.Render(rowDim.Render("(press → to expand, ↑↓ to find a pane)"))
	}
	p, _ := n.Ref.(*snapshot.Pane)
	if p == nil || p.ScrollbackSHA == "" {
		return frame.Render(rowDim.Render("(no scrollback captured for this pane)"))
	}
	sha := p.ScrollbackSHA
	if err := m.scrollbackErrors[sha]; err != nil {
		return frame.Render(footerWarn.Render("(scrollback file missing: " + err.Error() + ")"))
	}
	content, ok := m.scrollbacks[sha]
	if !ok {
		if m.loadingSHAs[sha] {
			return frame.Render(rowDim.Render("(loading scrollback…)"))
		}
		// Not loading yet — PreviewCmd will schedule on next key event.
		return frame.Render(rowDim.Render("(scrollback pending)"))
	}
	return frame.Render(previewWindow(string(content), innerWidth, innerHeight, m.previewScroll, m.previewScrollX))
}

// previewWindow returns the slice of scrollback to display: structural ANSI
// removed (so cursor moves and erase codes don't break the lipgloss frame),
// each logical line horizontally windowed to [scrollX, scrollX+width) and the
// vertical window offset `scroll` lines from the tail. SGR color escapes are
// preserved through ansi.Cut so the preview stays colored where possible.
func previewWindow(s string, width, height, scroll, scrollX int) string {
	if height <= 0 {
		return ""
	}
	cleaned := sanitizeScrollback(s)
	raw := strings.Split(strings.TrimRight(cleaned, "\n"), "\n")
	lines := make([]string, len(raw))
	for i, l := range raw {
		if scrollX > 0 {
			l = ansi.Cut(l, scrollX, scrollX+width)
		} else {
			l = ansi.Truncate(l, width, "")
		}
		lines[i] = l
	}
	end := len(lines) - scroll
	if end > len(lines) {
		end = len(lines)
	}
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:end], "\n")
}

// previewInnerHeight is the number of scrollback rows the preview pane shows:
// the body height (m.height − footer) minus the frame's border. Single source
// of truth for renderPreview and the scroll-clamp math in Update/handleKey,
// which otherwise drifted apart at very small terminal heights.
func (m PickerModel) previewInnerHeight() int {
	frameHeight := m.height - 1
	if frameHeight < 5 {
		frameHeight = 5
	}
	if inner := frameHeight - 2; inner > 1 {
		return inner
	}
	return 1
}

// previewMaxScroll returns the largest valid m.previewScroll for the current
// pane's scrollback. Used to clamp Alt+K scroll-up at the top of the buffer.
func (m PickerModel) previewMaxScroll(innerHeight int) int {
	nodes := m.visibleNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return 0
	}
	n := nodes[m.treeCursor]
	if n.Kind != NodePane {
		return 0
	}
	p, _ := n.Ref.(*snapshot.Pane)
	if p == nil {
		return 0
	}
	content, ok := m.scrollbacks[p.ScrollbackSHA]
	if !ok {
		return 0
	}
	total := strings.Count(strings.TrimRight(string(content), "\n"), "\n") + 1
	if total <= innerHeight {
		return 0
	}
	return total - innerHeight
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
