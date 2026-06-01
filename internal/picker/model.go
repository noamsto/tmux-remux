package picker

import (
	"encoding/json"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/scrollback"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
)

// Mode is "snapshot" (tree pane visible) or "close" (list-only).
type Mode int

// Picker modes select which event kind drives the UI.
const (
	ModeSnapshot Mode = iota // restore-from-snapshot picker (prefix+R)
	ModeClose                // restore-from-close-event picker (prefix+U)
)

type focusZone int

const (
	focusList focusZone = iota
	focusTree
)

// FocusZone aliases focusZone for tests.
type FocusZone = focusZone

// Focus-zone constants exported for tests.
const (
	FocusList = focusList
	FocusTree = focusTree
)

// PickerModel is the Bubble Tea model for the restore picker.
//
//revive:disable-next-line:exported other callers reference picker.PickerModel
type PickerModel struct {
	mode             Mode
	events           []store.Event
	cursor           int
	treeCursor       int                         // index into the flattened visible-node list
	manifests        map[int64]snapshot.Manifest // lazy parse cache
	trees            map[int64]*TreeNode         // lazy build cache
	manifestErrors   map[int64]error             // remember parse failures
	filter           filter.Filter
	dimOlderThan     time.Duration // list-pane only; 0 = no dimming
	runningSet       map[string]bool
	keys             keyMap
	help             help.Model
	width, height    int
	focus            focusZone
	showHelp         bool
	footerNote       string // transient warning text
	selectedID       int64  // 0 = no selection (cancelled)
	scrollbackStore  *scrollback.Store
	scrollbacks      map[string][]byte // sha → bytes
	scrollbackErrors map[string]error  // sha → load error
	loadingSHAs      map[string]bool   // sha → in-flight load
	previewScroll    int               // lines scrolled up from the tail; 0 = bottom
	previewScrollX   int               // visible cells shifted right; 0 = left edge
}

// NewPickerModel builds the initial state. The caller is responsible for
// fetching events and the running session set before constructing it.
func NewPickerModel(mode Mode, events []store.Event, running map[string]bool, sb *scrollback.Store) PickerModel {
	applyTheme(NewTheme())
	return PickerModel{
		mode:             mode,
		events:           events,
		manifests:        make(map[int64]snapshot.Manifest, len(events)),
		trees:            make(map[int64]*TreeNode, len(events)),
		manifestErrors:   make(map[int64]error),
		filter:           filter.Filter{DedupRunningServer: true},
		dimOlderThan:     24 * time.Hour,
		runningSet:       running,
		keys:             defaultKeys(),
		help:             help.New(),
		focus:            focusList,
		scrollbackStore:  sb,
		scrollbacks:      make(map[string][]byte),
		scrollbackErrors: make(map[string]error),
		loadingSHAs:      make(map[string]bool),
	}
}

// ScrollbackStore returns the scrollback store passed to the constructor.
// Exported for tests; production code does not call this.
func (m PickerModel) ScrollbackStore() *scrollback.Store { return m.scrollbackStore }

// ScrollbackFor returns the cached scrollback bytes for sha and whether the
// entry was present.
func (m PickerModel) ScrollbackFor(sha string) ([]byte, bool) {
	b, ok := m.scrollbacks[sha]
	return b, ok
}

// ScrollbackError returns the cached load error for sha, or nil.
func (m PickerModel) ScrollbackError(sha string) error { return m.scrollbackErrors[sha] }

// Init satisfies tea.Model.
func (m PickerModel) Init() tea.Cmd { return nil }

// Update handles key events. Implementation grows across the next few tasks.
func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case scrollbackLoadedMsg:
		delete(m.loadingSHAs, msg.sha)
		if msg.err != nil {
			m.scrollbackErrors[msg.sha] = msg.err
		} else {
			m.scrollbacks[msg.sha] = msg.content
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseWheelMsg:
		if m.mode != ModeSnapshot {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			inner := m.height - 3
			maxScroll := m.previewMaxScroll(inner)
			m.previewScroll += 3
			if m.previewScroll > maxScroll {
				m.previewScroll = maxScroll
			}
			return m, nil
		case tea.MouseWheelDown:
			m.previewScroll -= 3
			if m.previewScroll < 0 {
				m.previewScroll = 0
			}
			return m, nil
		}
	}
	return m, nil
}

// visibleNodes flattens the current tree honoring Expanded.
func (m PickerModel) visibleNodes() []*TreeNode {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return nil
	}
	tree := m.trees[m.events[m.cursor].ID]
	if tree == nil {
		return nil
	}
	var out []*TreeNode
	var walk func(n *TreeNode)
	walk = func(n *TreeNode) {
		out = append(out, n)
		if n.Expanded {
			for _, c := range n.Children {
				walk(c)
			}
		}
	}
	for _, sess := range tree.Children {
		walk(sess)
	}
	return out
}

// VisibleNodes exports visibleNodes for tests.
func (m PickerModel) VisibleNodes() []*TreeNode { return m.visibleNodes() }

// firstPaneIdx returns the index of the first visible NodePane, or 0 if none.
func (m PickerModel) firstPaneIdx() int {
	for i, n := range m.visibleNodes() {
		if n.Kind == NodePane {
			return i
		}
	}
	return 0
}

// isNavTarget reports whether `n` is a valid Up/Down landing spot in tree
// focus: panes (where the preview lives) and collapsed non-leaf nodes (so the
// user can step onto a collapsed window/session and press Right to re-expand).
func isNavTarget(n *TreeNode) bool {
	if n.Kind == NodePane {
		return true
	}
	return !n.Expanded && len(n.Children) > 0
}

// nextPaneIdx walks from `start` in `dir` (+1 or -1) and returns the next
// navigable index — see isNavTarget. Returns -1 if none in that direction.
func (m PickerModel) nextPaneIdx(start, dir int) int {
	nodes := m.visibleNodes()
	for i := start + dir; i >= 0 && i < len(nodes); i += dir {
		if isNavTarget(nodes[i]) {
			return i
		}
	}
	return -1
}

// indexOf returns the visible-tree index of `target`, or -1 if `target` isn't
// currently visible (e.g., an ancestor was collapsed).
func (m PickerModel) indexOf(target *TreeNode) int {
	for i, n := range m.visibleNodes() {
		if n == target {
			return i
		}
	}
	return -1
}

// firstPaneIdxIn returns the visible-tree index of the first NodePane that
// descends from `subtree`. Falls back to `indexOf(subtree)` if no pane is
// visible underneath (everything below it is collapsed).
func (m PickerModel) firstPaneIdxIn(subtree *TreeNode) int {
	nodes := m.visibleNodes()
	rootIdx := -1
	seenRoot := false
	for i, n := range nodes {
		if n == subtree {
			rootIdx = i
			seenRoot = true
			continue
		}
		if !seenRoot {
			continue
		}
		// Once we step outside the subtree, stop.
		if !descendsFrom(n, subtree) {
			break
		}
		if n.Kind == NodePane {
			return i
		}
	}
	return rootIdx
}

func descendsFrom(n, ancestor *TreeNode) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p == ancestor {
			return true
		}
	}
	return false
}

func (m PickerModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Preview scroll: Alt+J/K / PgUp/PgDn. Available whenever the picker is in
	// snapshot mode — scroll up to read past output without leaving the cursor
	// pane. We approximate the inner height with m.height-3 (footer+borders).
	if m.mode == ModeSnapshot {
		switch {
		case key.Matches(msg, m.keys.PreviewUp):
			inner := m.height - 3
			maxScroll := m.previewMaxScroll(inner)
			if m.previewScroll < maxScroll {
				m.previewScroll++
				if m.previewScroll > maxScroll {
					m.previewScroll = maxScroll
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.PreviewDown):
			if m.previewScroll > 0 {
				m.previewScroll--
			}
			return m, nil
		case key.Matches(msg, m.keys.PreviewLeft):
			if m.previewScrollX > 0 {
				m.previewScrollX -= 8
				if m.previewScrollX < 0 {
					m.previewScrollX = 0
				}
			}
			return m, nil
		case key.Matches(msg, m.keys.PreviewRight):
			m.previewScrollX += 8
			return m, nil
		}
	}
	// Focus-tree key handling for ModeSnapshot: intercept Up/Down/Left/Right.
	if m.mode == ModeSnapshot && m.focus == focusTree {
		switch {
		case key.Matches(msg, m.keys.Up):
			if idx := m.nextPaneIdx(m.treeCursor, -1); idx >= 0 {
				m.treeCursor = idx
				m.previewScroll = 0
				m.previewScrollX = 0
			}
			return m, (&m).PreviewCmd()
		case key.Matches(msg, m.keys.Down):
			if idx := m.nextPaneIdx(m.treeCursor, +1); idx >= 0 {
				m.treeCursor = idx
				m.previewScroll = 0
				m.previewScrollX = 0
			}
			return m, (&m).PreviewCmd()
		case key.Matches(msg, m.keys.Right):
			nodes := m.visibleNodes()
			if m.treeCursor >= 0 && m.treeCursor < len(nodes) {
				n := nodes[m.treeCursor]
				switch {
				case !n.Expanded && len(n.Children) > 0:
					// Cursor on a collapsed parent: expand and re-snap to first
					// pane within (preserves pane-first invariant).
					n.Expanded = true
					m.treeCursor = m.firstPaneIdxIn(n)
				case n.Kind == NodePane:
					// On a leaf pane: nothing to expand. No-op.
				}
			}
			return m, (&m).PreviewCmd()
		case key.Matches(msg, m.keys.Left):
			nodes := m.visibleNodes()
			if m.treeCursor >= 0 && m.treeCursor < len(nodes) {
				// Walk up from cursor to the nearest expanded ancestor with
				// children, collapse it, and move the cursor to it. From a pane
				// this collapses the parent window; pressing Left again on the
				// window collapses the parent session.
				for n := nodes[m.treeCursor]; n != nil; n = n.Parent {
					if n.Parent == nil {
						break // don't collapse the synthetic root
					}
					if n.Expanded && len(n.Children) > 0 {
						n.Expanded = false
						m.treeCursor = m.indexOf(n)
						break
					}
				}
			}
			return m, (&m).PreviewCmd()
		}
	}

	switch {
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.events)-1 {
			m.cursor++
			(&m).ensureManifest()
			m.treeCursor = m.firstPaneIdx()
			m.previewScroll = 0
		}
		return m, (&m).PreviewCmd()
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
			(&m).ensureManifest()
			m.treeCursor = m.firstPaneIdx()
			m.previewScroll = 0
		}
		return m, (&m).PreviewCmd()
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Tab):
		if m.mode == ModeSnapshot {
			if m.focus == focusList {
				m.focus = focusTree
				nodes := m.visibleNodes()
				if m.treeCursor < 0 || m.treeCursor >= len(nodes) || nodes[m.treeCursor].Kind != NodePane {
					m.treeCursor = m.firstPaneIdx()
				}
			} else {
				m.focus = focusList
			}
		}
		return m, (&m).PreviewCmd()
	case key.Matches(msg, m.keys.ToggleIdle):
		m.filter.SkipIdleShells = !m.filter.SkipIdleShells
		(&m).redecorate()
		return m, nil
	case key.Matches(msg, m.keys.ToggleDedup):
		m.filter.DedupRunningServer = !m.filter.DedupRunningServer
		(&m).redecorate()
		return m, nil
	case key.Matches(msg, m.keys.ToggleAge):
		if m.dimOlderThan == 0 {
			m.dimOlderThan = 24 * time.Hour
		} else {
			m.dimOlderThan = 0
		}
		return m, nil
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if m.cursor < 0 || m.cursor >= len(m.events) {
			return m, nil
		}
		ev := m.events[m.cursor]
		if _, bad := m.manifestErrors[ev.ID]; bad {
			m.footerNote = "(invalid manifest — cannot restore)"
			return m, nil
		}
		if _, ok := m.manifests[ev.ID]; !ok {
			m.footerNote = "(manifest not loaded yet)"
			return m, nil
		}
		m.selectedID = ev.ID
		return m, tea.Quit
	}
	return m, nil
}

// redecorate runs FilterDecorate over every cached tree with the current
// filter state. Cheap — O(nodes) and only over what's been viewed.
func (m *PickerModel) redecorate() {
	for _, tree := range m.trees {
		FilterDecorate(tree, m.filter, m.runningSet)
	}
}

// Bootstrap parses the manifest for the initial cursor position. Call once
// after construction; the cobra wiring does this so View has data on first
// render. Idempotent.
func (m *PickerModel) Bootstrap() {
	m.ensureManifest()
}

// CurrentCounts returns FilterDecorate's most recent output for the cursor's
// event. Used by the footer and by tests.
func (m PickerModel) CurrentCounts() Counts {
	if m.cursor < 0 || m.cursor >= len(m.events) {
		return Counts{}
	}
	tree := m.trees[m.events[m.cursor].ID]
	if tree == nil {
		return Counts{}
	}
	// FilterDecorate mutates in place; rerun to read counts cheaply.
	return FilterDecorate(tree, m.filter, m.runningSet)
}

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

// TreeCursor returns the index of the focused node in the visible-tree list.
// Exported for tests.
func (m PickerModel) TreeCursor() int { return m.treeCursor }

// FooterNote returns the transient warning text (exported for tests + view).
func (m PickerModel) FooterNote() string { return m.footerNote }

// TreeFor returns the cached tree for the event with the given ID, or nil.
func (m PickerModel) TreeFor(id int64) *TreeNode { return m.trees[id] }

// PreviewCmd returns a tea.Cmd that loads the scrollback for the currently
// focused tree-pane node, or nil if no load is needed (wrong focus, no SHA,
// cached, already loading, or no scrollback store).
//
// Side effect: marks the SHA as in-flight in m.loadingSHAs before returning.
// Pointer receiver is required to write through to that map.
func (m *PickerModel) PreviewCmd() tea.Cmd {
	sha := m.focusedPaneSHA()
	if sha == "" || m.scrollbackStore == nil {
		return nil
	}
	if _, cached := m.scrollbacks[sha]; cached {
		return nil
	}
	if _, errored := m.scrollbackErrors[sha]; errored {
		return nil
	}
	if m.loadingSHAs[sha] {
		return nil
	}
	m.loadingSHAs[sha] = true
	return loadScrollbackCmd(m.scrollbackStore, sha)
}

// focusedPaneSHA returns the ScrollbackSHA of the currently focused tree-pane
// node, or "" if focus is not on the tree, the node is not a pane, or the
// pane has no scrollback.
func (m PickerModel) focusedPaneSHA() string {
	if m.mode != ModeSnapshot || m.focus != focusTree {
		return ""
	}
	nodes := m.visibleNodes()
	if m.treeCursor < 0 || m.treeCursor >= len(nodes) {
		return ""
	}
	n := nodes[m.treeCursor]
	if n.Kind != NodePane {
		return ""
	}
	p, ok := n.Ref.(*snapshot.Pane)
	if !ok || p == nil {
		return ""
	}
	return p.ScrollbackSHA
}

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
