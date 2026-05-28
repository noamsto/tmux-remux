package picker

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/tmux-state/internal/scrollback"
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

// renderPreview is the stub completed in Task 6.
func (m PickerModel) renderPreview(width int) string {
	return previewFrame.Width(width).Render("(preview)")
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
