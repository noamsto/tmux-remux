package picker

import (
	"encoding/json"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
)

// Mode is "snapshot" (tree pane visible) or "close" (list-only).
type Mode int

const (
	ModeSnapshot Mode = iota
	ModeClose
)

type focusZone int

const (
	focusList focusZone = iota
	focusTree
)

// FocusZone aliases focusZone for tests.
type FocusZone = focusZone

const (
	FocusList = focusList
	FocusTree = focusTree
)

// PickerModel is the Bubble Tea model for the restore picker.
type PickerModel struct {
	mode           Mode
	events         []store.Event
	cursor         int
	manifests      map[int64]snapshot.Manifest // lazy parse cache
	trees          map[int64]*TreeNode         // lazy build cache
	manifestErrors map[int64]error             // remember parse failures
	filter         filter.Filter
	dimOlderThan   time.Duration // list-pane only; 0 = no dimming
	runningSet     map[string]bool
	keys           keyMap
	help           help.Model
	width, height  int
	focus          focusZone
	showHelp       bool
	footerNote     string // transient warning text
	selectedID     int64  // 0 = no selection (cancelled)
}

// NewPickerModel builds the initial state. The caller is responsible for
// fetching events and the running session set before constructing it.
func NewPickerModel(mode Mode, events []store.Event, running map[string]bool) PickerModel {
	return PickerModel{
		mode:           mode,
		events:         events,
		manifests:      make(map[int64]snapshot.Manifest, len(events)),
		trees:          make(map[int64]*TreeNode, len(events)),
		manifestErrors: make(map[int64]error),
		filter:         filter.Filter{DedupRunningServer: true},
		runningSet:     running,
		keys:           defaultKeys(),
		help:           help.New(),
		focus:          focusList,
	}
}

// Init satisfies tea.Model.
func (m PickerModel) Init() tea.Cmd { return nil }

// Update handles key events. Implementation grows across the next few tasks.
func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m PickerModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.events)-1 {
			m.cursor++
			(&m).ensureManifest()
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
			(&m).ensureManifest()
		}
		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Tab):
		if m.mode == ModeSnapshot {
			if m.focus == focusList {
				m.focus = focusTree
			} else {
				m.focus = focusList
			}
		}
		return m, nil
	}
	return m, nil
}

// View is implemented in view.go.
func (m PickerModel) View() tea.View { return tea.NewView("") }

// Filter returns the current filter for caller-side BuildPlan.
func (m PickerModel) Filter() filter.Filter { return m.filter }

// SelectedID returns the event ID of the row the user confirmed, or 0 on cancel.
func (m PickerModel) SelectedID() int64 { return m.selectedID }

// SelectedManifest returns the parsed manifest of the selected event.
func (m PickerModel) SelectedManifest() snapshot.Manifest {
	if m.selectedID == 0 {
		return snapshot.Manifest{}
	}
	return m.manifests[m.selectedID]
}

// Focus returns the current focus zone (exported for tests).
func (m PickerModel) Focus() FocusZone { return m.focus }

// Cursor returns the current cursor position (exported for tests).
func (m PickerModel) Cursor() int { return m.cursor }

// TreeFor returns the cached tree for the event with the given ID, or nil.
func (m PickerModel) TreeFor(id int64) *TreeNode { return m.trees[id] }

// ensureManifest parses + builds + decorates the tree for the cursor's event,
// caching the result. No-op on cache hit. Records parse errors in
// m.manifestErrors so View can render "(invalid manifest)".
func (m *PickerModel) ensureManifest() {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return
	}
	ev := m.events[m.cursor]
	if _, ok := m.manifests[ev.ID]; ok {
		return
	}
	if _, bad := m.manifestErrors[ev.ID]; bad {
		return
	}
	man, err := parseEventManifest(ev)
	if err != nil {
		m.manifestErrors[ev.ID] = err
		return
	}
	m.manifests[ev.ID] = man
	tree := BuildTree(man)
	FilterDecorate(tree, m.filter, m.runningSet)
	m.trees[ev.ID] = tree
}

func parseEventManifest(ev store.Event) (snapshot.Manifest, error) {
	var m snapshot.Manifest
	if ev.Kind == "snapshot" {
		if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
			return snapshot.Manifest{}, err
		}
		return m, nil
	}
	// Close events wrap the index inside an "index" key.
	var wrapped struct {
		Index json.RawMessage `json:"index"`
	}
	if err := json.Unmarshal([]byte(ev.ManifestJSON), &wrapped); err != nil {
		return snapshot.Manifest{}, err
	}
	if len(wrapped.Index) == 0 {
		return snapshot.Manifest{}, fmt.Errorf("close event has no index")
	}
	if err := json.Unmarshal(wrapped.Index, &m); err != nil {
		return snapshot.Manifest{}, err
	}
	return m, nil
}
