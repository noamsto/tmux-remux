package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/tmux-remux/internal/store"
)

// mouseSnapModel is a snapshot-mode model with five events, sized for a
// three-column layout (list 40 / tree 53 / preview 67 at width 160).
func mouseSnapModel(t *testing.T) PickerModel {
	t.Helper()
	applyTheme(NewTheme())
	var evs []store.Event
	for i := 1; i <= 5; i++ {
		evs = append(evs, store.Event{ID: int64(i), Kind: "snapshot",
			ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[{"index":0,"command":"fish"}]}]}]}`})
	}
	m := NewPickerModel(ModeSnapshot, evs, nil, nil)
	m.width, m.height = 160, 40
	m.Bootstrap()
	return m
}

// zoneAt must agree with View's composition: a wheel or click has to land on
// the pane the user sees, not a layout recomputed differently.
func TestZoneAt_RoutesToPaneUnderPointer(t *testing.T) {
	m := mouseSnapModel(t)
	for _, tc := range []struct {
		name    string
		x, y    int
		want    paneZone
		wantRow int
	}{
		{"list", 10, 5, zoneList, 4},
		{"tree", 50, 5, zoneTree, 4},
		{"preview", 140, 5, zonePreview, 4},
		{"footer", 10, 39, zoneNone, 0},
	} {
		gotZone, gotRow := m.zoneAt(tc.x, tc.y)
		if gotZone != tc.want {
			t.Errorf("%s: zoneAt(%d,%d) zone = %v, want %v", tc.name, tc.x, tc.y, gotZone, tc.want)
		}
		if tc.want != zoneNone && gotRow != tc.wantRow {
			t.Errorf("%s: zoneAt(%d,%d) row = %d, want %d", tc.name, tc.x, tc.y, gotRow, tc.wantRow)
		}
	}
}

// Below closeSideBySideMin the preview has no column beside the list, so the
// zone under it must be the stacked panel instead.
func TestZoneAt_StackedClosePreviewIsUnderTheList(t *testing.T) {
	m := closeListModel(t, 12)
	m.width, m.height = 100, 40
	if z, _ := m.zoneAt(50, 5); z != zoneList {
		t.Errorf("upper half = %v, want list", z)
	}
	if z, _ := m.zoneAt(50, 25); z != zonePreview {
		t.Errorf("lower half = %v, want preview", z)
	}
}

func TestPreviewHintKey_NamesAllFourDirections(t *testing.T) {
	if got := previewHintKey(defaultKeys()); got != "M-hjkl" {
		t.Errorf("previewHintKey() = %q, want %q", got, "M-hjkl")
	}
}

// The wheel over the list walks the cursor; the same wheel over the preview
// scrolls the preview and leaves the cursor alone.
func TestUpdate_WheelByPane(t *testing.T) {
	m := mouseSnapModel(t)

	u, _ := m.Update(tea.MouseWheelMsg{X: 10, Y: 5, Button: tea.MouseWheelDown})
	got := u.(PickerModel)
	if got.cursor != 3 {
		t.Errorf("wheel down over list: cursor = %d, want 3", got.cursor)
	}

	u, _ = got.Update(tea.MouseWheelMsg{X: 140, Y: 5, Button: tea.MouseWheelUp})
	got = u.(PickerModel)
	if got.cursor != 3 {
		t.Errorf("wheel over preview moved the list cursor to %d", got.cursor)
	}
}

func TestUpdate_WheelOverTreeMovesTreeCursor(t *testing.T) {
	m := mouseSnapModel(t)
	before := m.treeCursor

	u, _ := m.Update(tea.MouseWheelMsg{X: 50, Y: 5, Button: tea.MouseWheelDown})
	got := u.(PickerModel)
	if got.focus != focusTree {
		t.Errorf("wheel over tree left focus = %v, want focusTree", got.focus)
	}
	if got.treeCursor == before {
		t.Errorf("wheel down over tree did not move treeCursor (%d)", before)
	}
}

// A left-click selects the list row under the pointer; clicks elsewhere leave
// the cursor where it was.
func TestUpdate_ClickSelectsListRow(t *testing.T) {
	m := mouseSnapModel(t)

	u, _ := m.Update(tea.MouseClickMsg{X: 10, Y: 2, Button: tea.MouseLeft})
	got := u.(PickerModel)
	if got.cursor != 1 {
		t.Errorf("click on row 1: cursor = %d, want 1", got.cursor)
	}

	u, _ = got.Update(tea.MouseClickMsg{X: 140, Y: 2, Button: tea.MouseLeft})
	if c := u.(PickerModel).cursor; c != 1 {
		t.Errorf("click on the preview moved the cursor to %d", c)
	}

	u, _ = got.Update(tea.MouseClickMsg{X: 10, Y: 2, Button: tea.MouseRight})
	if c := u.(PickerModel).cursor; c != 1 {
		t.Errorf("right-click moved the cursor to %d", c)
	}
}

// A click on a close list's section header is structural, not a selection.
func TestUpdate_ClickOnCloseSectionHeaderDoesNothing(t *testing.T) {
	m := closeListModel(t, 12)
	m.width, m.height = 100, 40

	hdr := -1
	for i, r := range m.closeRows {
		if r.Kind == RowSectionHeader {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		t.Fatal("fixture has no section header")
	}
	start, _, pin, _, _ := m.closeListWindow(m.listPaneHeight())
	base := 0
	if pin >= 0 {
		base = 1
	}
	before := m.cursor
	u, _ := m.Update(tea.MouseClickMsg{X: 50, Y: hdr - start + base + 1, Button: tea.MouseLeft})
	if c := u.(PickerModel).cursor; c != before {
		t.Errorf("click on a section header moved the cursor %d→%d", before, c)
	}
}
