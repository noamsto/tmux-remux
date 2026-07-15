package picker_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/tmux-remux/internal/picker"
	"github.com/noamsto/tmux-remux/internal/scrollback"
	"github.com/noamsto/tmux-remux/internal/store"
)

func TestModel_TabSwitchesFocus_SnapshotMode(t *testing.T) {
	events := []store.Event{{ID: 1, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[]}`}}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)

	// Initial focus is list. After tab, should be tree.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	pm := updated.(picker.PickerModel)
	if pm.Focus() != picker.FocusTree {
		t.Errorf("after tab: focus=%v, want focusTree", pm.Focus())
	}

	// Tab again returns to list.
	updated, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	pm = updated.(picker.PickerModel)
	if pm.Focus() != picker.FocusList {
		t.Errorf("after second tab: focus=%v, want focusList", pm.Focus())
	}
}

func TestModel_ToggleIdleUpdatesCounter(t *testing.T) {
	events := []store.Event{
		{
			ID: 1, Kind: "snapshot",
			ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[
				{"index":0,"command":"fish","child_count":0},
				{"index":1,"command":"nvim","child_count":0}
			]}]}]}`,
		},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()

	// Before toggle: 2 panes kept.
	if c := m.CurrentCounts(); c.KeptPanes != 2 || c.SkippedPanes != 0 {
		t.Fatalf("before toggle: counts=%+v", c)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	pm := updated.(picker.PickerModel)

	// After "skip idle shells": fish (idle) skipped, nvim kept.
	if c := pm.CurrentCounts(); c.KeptPanes != 1 || c.SkippedPanes != 1 {
		t.Errorf("after toggle: counts=%+v", c)
	}
}

func TestModel_CursorMoveTriggersManifestParse(t *testing.T) {
	events := []store.Event{
		{ID: 1, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[{"name":"a","windows":[]}]}`},
		{ID: 2, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[{"name":"b","windows":[]}]}`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	pm := updated.(picker.PickerModel)
	if pm.Cursor() != 1 {
		t.Errorf("after Down: cursor=%d, want 1", pm.Cursor())
	}
	tree := pm.TreeFor(2)
	if tree == nil || len(tree.Children) != 1 || tree.Children[0].Label != "b (0w)" {
		t.Errorf("tree for event 2 not built correctly: %+v", tree)
	}
}

func TestModel_EnterRecordsSelectedID(t *testing.T) {
	events := []store.Event{
		{ID: 7, Kind: "snapshot", ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[]}]}`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pm := updated.(picker.PickerModel)
	if pm.SelectedID() != 7 {
		t.Errorf("selectedID=%d, want 7", pm.SelectedID())
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd, got nil")
	}
}

func TestModel_EnterBlockedOnParseError(t *testing.T) {
	events := []store.Event{
		{ID: 9, Kind: "snapshot", ManifestJSON: `{not json`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pm := updated.(picker.PickerModel)
	if pm.SelectedID() != 0 {
		t.Errorf("selectedID=%d, want 0 (blocked)", pm.SelectedID())
	}
	if cmd != nil {
		t.Error("expected no quit cmd on parse error")
	}
	if pm.FooterNote() == "" {
		t.Error("expected footer warning to be set")
	}
}

func TestModel_TreeLeftCollapsesAncestorRightReExpands(t *testing.T) {
	events := []store.Event{{
		ID: 1, Kind: "snapshot",
		ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[
			{"index":0,"command":"fish"}
		]}]}]}`,
	}}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()
	// Tab into tree: cursor lands on the pane.
	upd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	pm := upd.(picker.PickerModel)
	// Left from the pane collapses the parent window; cursor moves to the window.
	upd, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	pm = upd.(picker.PickerModel)
	nodes := pm.VisibleNodes()
	if len(nodes) != 2 {
		t.Fatalf("after Left from pane: visible=%d, want 2 (session + collapsed window)", len(nodes))
	}
	if nodes[pm.TreeCursor()].Kind != picker.NodeWindow {
		t.Errorf("after Left from pane: cursor on %v, want NodeWindow", nodes[pm.TreeCursor()].Kind)
	}
	// Right re-expands and snaps back to the pane within.
	upd, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	pm = upd.(picker.PickerModel)
	nodes = pm.VisibleNodes()
	if nodes[pm.TreeCursor()].Kind != picker.NodePane {
		t.Errorf("after Right re-expand: cursor on %v, want NodePane", nodes[pm.TreeCursor()].Kind)
	}
}

func TestModel_TreeFocusLandsOnFirstPane(t *testing.T) {
	events := []store.Event{{
		ID: 1, Kind: "snapshot",
		ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[
			{"index":0,"command":"fish"},
			{"index":1,"command":"vim"}
		]}]}]}`,
	}}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()
	// Tab into tree focus. Cursor should snap to the first pane, skipping
	// session/window nodes that have no preview.
	upd, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	pm := upd.(picker.PickerModel)
	nodes := pm.VisibleNodes()
	cur := pm.TreeCursor()
	if cur < 0 || cur >= len(nodes) || nodes[cur].Kind != picker.NodePane {
		t.Fatalf("after Tab: cursor=%d, want a NodePane", cur)
	}
	// Down should move to the next pane, skipping over any non-pane between.
	upd, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	pm = upd.(picker.PickerModel)
	nodes = pm.VisibleNodes()
	cur = pm.TreeCursor()
	if cur < 0 || cur >= len(nodes) || nodes[cur].Kind != picker.NodePane {
		t.Fatalf("after Down: cursor=%d, want a NodePane", cur)
	}
}

func TestModel_ViewRendersWithoutPanic(t *testing.T) {
	events := []store.Event{
		{ID: 1, Ts: time.Now().UnixMilli(), Kind: "snapshot",
			ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[{"index":0,"command":"fish"}]}]}]}`},
	}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()
	// Simulate a sane terminal size.
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pm := upd.(picker.PickerModel)
	view := pm.View()
	out := view.Content // tea.View exposes rendered content via the Content field
	if out == "" {
		t.Fatal("View() returned empty string")
	}
	if !strings.Contains(out, "s (1w)") {
		t.Errorf("expected session label in view, got:\n%s", out)
	}
}

func TestNewPickerModel_AcceptsScrollbackStore(t *testing.T) {
	tmp := t.TempDir()
	sb := scrollback.New(tmp)
	m := picker.NewPickerModel(picker.ModeSnapshot, nil, nil, sb)
	if m.ScrollbackStore() != sb {
		t.Fatalf("scrollback store not threaded through constructor")
	}
}

func TestModel_ViewHighlightsTreeCursor(t *testing.T) {
	events := []store.Event{{
		ID: 1, Ts: time.Now().UnixMilli(), Kind: "snapshot",
		ManifestJSON: `{"v":1,"sessions":[{"name":"s","windows":[{"name":"w","panes":[{"index":0,"command":"fish"}]}]}]}`,
	}}
	m := picker.NewPickerModel(picker.ModeSnapshot, events, nil, nil)
	m.Bootstrap()
	// Resize so two-pane mode kicks in.
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	pm := upd.(picker.PickerModel)
	// Move focus to tree.
	upd, _ = pm.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	pm = upd.(picker.PickerModel)
	out := pm.View().Content
	// Active row style sets a mauve background (#cba6f7 = 203;166;247 in 24-bit SGR).
	// When focus is on the tree pane, the first visible node must be highlighted.
	if !strings.Contains(out, "203;166;247") {
		t.Errorf("expected mauve-background highlight in tree pane, got:\n%s", out)
	}
}
