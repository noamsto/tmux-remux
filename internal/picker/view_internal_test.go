package picker

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// TestRenderList_NeverOverflowsFrame guards the list-pane box math: a lipgloss
// frame pads short content but does NOT clip overflow, so a wrapped row or
// footer pushes the border past the requested height and desyncs the sibling
// panes. Rendered output must be exactly width×height for every size — even a
// narrow pane with a long label and a hidden-count footer.
func TestRenderList_NeverOverflowsFrame(t *testing.T) {
	applyTheme(NewTheme())
	long := "reviewtest-session-with-a-fairly-long-path/main 🧠 (2p)"
	m := PickerModel{
		mode:         ModeClose,
		dimOlderThan: 24 * time.Hour,
		events: []store.Event{
			{ID: 1, Ts: time.Now().UnixMilli(), Kind: "window-unlinked"},
			{ID: 2, Ts: time.Now().UnixMilli(), Kind: "pane-died"},
		},
		closeContexts: map[int64]CloseContext{
			1: {Label: long, SubManifest: oneSession()},
			2: {Label: "pane: fish in mono/2", SubManifest: oneSession()},
		},
		hiddenCount: 14,
	}
	for _, w := range []int{32, 40, 80, 120} {
		for _, h := range []int{3, 4, 6, 10} {
			out := renderList(m, w, h)
			if got := lipgloss.Height(out); got != h {
				t.Errorf("renderList(w=%d,h=%d): height=%d, want %d\n%s", w, h, got, h, out)
			}
			if got := lipgloss.Width(out); got != w {
				t.Errorf("renderList(w=%d,h=%d): width=%d, want %d", w, h, got, w)
			}
		}
	}
}

func oneSession() snapshot.Manifest {
	return snapshot.Manifest{Sessions: []snapshot.Session{{Name: "s"}}}
}
