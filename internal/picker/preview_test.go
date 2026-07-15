package picker

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/noamsto/tmux-remux/internal/scrollback"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

func TestLoadScrollbackCmd_ReturnsContent(t *testing.T) {
	tmp := t.TempDir()
	sb := scrollback.New(tmp)
	sha, _, err := sb.Put(context.Background(), []byte("hello scrollback"))
	if err != nil {
		t.Fatalf("seed scrollback: %v", err)
	}

	cmd := loadScrollbackCmd(sb, sha)
	if cmd == nil {
		t.Fatal("loadScrollbackCmd returned nil")
	}
	msg := cmd()
	loaded, ok := msg.(scrollbackLoadedMsg)
	if !ok {
		t.Fatalf("expected scrollbackLoadedMsg, got %T", msg)
	}
	if loaded.sha != sha {
		t.Errorf("sha mismatch: got %q want %q", loaded.sha, sha)
	}
	if loaded.err != nil {
		t.Errorf("unexpected err: %v", loaded.err)
	}
	if !strings.Contains(string(loaded.content), "hello scrollback") {
		t.Errorf("content mismatch: got %q", loaded.content)
	}
}

func TestLoadScrollbackCmd_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	sb := scrollback.New(tmp)
	// all-zeros is not a valid sha256 output for any input, so this file is guaranteed absent
	const missing = "0000000000000000000000000000000000000000000000000000000000000000"

	cmd := loadScrollbackCmd(sb, missing)
	msg := cmd()
	loaded, ok := msg.(scrollbackLoadedMsg)
	if !ok {
		t.Fatalf("expected scrollbackLoadedMsg, got %T", msg)
	}
	if loaded.err == nil {
		t.Fatal("expected err for missing scrollback, got nil")
	}
	if !errors.Is(loaded.err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist-chain error, got %v", loaded.err)
	}
}

func TestPickerModel_HandlesScrollbackLoadedMsg(t *testing.T) {
	m := NewPickerModel(ModeSnapshot, nil, nil, nil)
	msg := scrollbackLoadedMsg{sha: "deadbeef", content: []byte("hi"), err: nil}
	updated, _ := m.Update(msg)
	final := updated.(PickerModel)
	got, ok := final.ScrollbackFor("deadbeef")
	if !ok {
		t.Fatalf("cache miss for sha after loaded msg")
	}
	if string(got) != "hi" {
		t.Errorf("content mismatch: got %q want %q", got, "hi")
	}
}

func TestPickerModel_RemembersScrollbackError(t *testing.T) {
	m := NewPickerModel(ModeSnapshot, nil, nil, nil)
	wantErr := errors.New("boom")
	msg := scrollbackLoadedMsg{sha: "deadbeef", err: wantErr}
	updated, _ := m.Update(msg)
	final := updated.(PickerModel)
	if got := final.ScrollbackError("deadbeef"); !errors.Is(got, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, got)
	}
}

func TestPickerModel_FocusedPaneTriggersLoad(t *testing.T) {
	// Build a minimal manifest with one pane carrying a scrollback SHA.
	man := snapshot.Manifest{
		V: 1,
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 0, Name: "w1",
				Panes: []snapshot.Pane{{Index: 0, Cwd: "/tmp", Command: "bash", ScrollbackSHA: "abc123"}},
			}},
		}},
	}
	raw, _ := json.Marshal(man)
	ev := store.Event{ID: 7, Kind: "snapshot", ManifestJSON: string(raw)}

	tmp := t.TempDir()
	sb := scrollback.New(tmp)

	m := NewPickerModel(ModeSnapshot, []store.Event{ev}, nil, sb)
	m.Bootstrap()
	// Focus tree, then walk cursor down session → window → pane.
	m.focus = focusTree
	m.treeCursor = paneNodeIndex(t, m)

	cmd := m.PreviewCmd()
	if cmd == nil {
		t.Fatal("PreviewCmd returned nil for a pane with scrollback")
	}
}

func TestPickerModel_NoLoadWhenAlreadyCached(t *testing.T) {
	man := snapshot.Manifest{
		V: 1,
		Sessions: []snapshot.Session{{
			Windows: []snapshot.Window{{
				Panes: []snapshot.Pane{{ScrollbackSHA: "abc123"}},
			}},
		}},
	}
	raw, _ := json.Marshal(man)
	ev := store.Event{ID: 7, Kind: "snapshot", ManifestJSON: string(raw)}
	tmp := t.TempDir()
	sb := scrollback.New(tmp)

	m := NewPickerModel(ModeSnapshot, []store.Event{ev}, nil, sb)
	m.Bootstrap()
	m.focus = focusTree
	m.treeCursor = paneNodeIndex(t, m)
	m.scrollbacks["abc123"] = []byte("cached")

	if cmd := m.PreviewCmd(); cmd != nil {
		t.Fatal("PreviewCmd should be nil when SHA already cached")
	}
}

func TestPickerModel_NoLoadWhenAlreadyInFlight(t *testing.T) {
	man := snapshot.Manifest{
		V: 1,
		Sessions: []snapshot.Session{{
			Windows: []snapshot.Window{{
				Panes: []snapshot.Pane{{ScrollbackSHA: "abc123"}},
			}},
		}},
	}
	raw, _ := json.Marshal(man)
	ev := store.Event{ID: 7, Kind: "snapshot", ManifestJSON: string(raw)}
	tmp := t.TempDir()
	sb := scrollback.New(tmp)

	m := NewPickerModel(ModeSnapshot, []store.Event{ev}, nil, sb)
	m.Bootstrap()
	m.focus = focusTree
	m.treeCursor = paneNodeIndex(t, m)

	if cmd := m.PreviewCmd(); cmd == nil {
		t.Fatal("first PreviewCmd should return a Cmd")
	}
	if cmd := m.PreviewCmd(); cmd != nil {
		t.Fatal("second PreviewCmd should be nil when SHA is in-flight")
	}
}

// paneNodeIndex returns the index of the first NodePane in m's visible tree,
// failing the test if none exists. Use this instead of hard-coded cursor indices.
func paneNodeIndex(t *testing.T, m PickerModel) int {
	t.Helper()
	for i, n := range m.VisibleNodes() {
		if n.Kind == NodePane {
			return i
		}
	}
	t.Fatal("no pane node in visible tree")
	return -1
}

func TestPaneWidths_ThreePane(t *testing.T) {
	m := PickerModel{mode: ModeSnapshot, width: 160}
	l, tr, pv := m.paneWidthsThree()
	if l+tr+pv != 160 {
		t.Errorf("widths must sum to total: got %d+%d+%d != 160", l, tr, pv)
	}
	if l < 28 || tr < 32 || pv < 40 {
		t.Errorf("min widths violated: l=%d tr=%d pv=%d", l, tr, pv)
	}
}

func TestPaneWidths_NarrowFallsBackToTwoPane(t *testing.T) {
	m := PickerModel{mode: ModeSnapshot, width: 100}
	l, tr, pv := m.paneWidthsThree()
	if pv != 0 {
		t.Errorf("preview should be 0 at width=100, got %d", pv)
	}
	if l+tr != 100 {
		t.Errorf("widths must sum to total: got %d+%d != 100", l, tr)
	}
}

func TestRenderPreview_NoPaneFocused(t *testing.T) {
	m := PickerModel{mode: ModeSnapshot, width: 160, height: 30, focus: focusList}
	got := m.renderPreview(60)
	if !strings.Contains(stripANSI(got), "press Tab") {
		t.Errorf("expected hint, got: %q", got)
	}
}

func TestRenderPreview_PaneWithoutSHA(t *testing.T) {
	man := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Windows: []snapshot.Window{{Panes: []snapshot.Pane{{}}}},
	}}}
	raw, _ := json.Marshal(man)
	ev := store.Event{ID: 1, Kind: "snapshot", ManifestJSON: string(raw)}
	m := NewPickerModel(ModeSnapshot, []store.Event{ev}, nil, nil)
	m.Bootstrap()
	m.focus = focusTree
	m.treeCursor = paneNodeIndex(t, m)

	got := m.renderPreview(60)
	if !strings.Contains(stripANSI(got), "no scrollback captured") {
		t.Errorf("expected hint, got: %q", got)
	}
}

func TestRenderPreview_Loaded(t *testing.T) {
	man := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Windows: []snapshot.Window{{Panes: []snapshot.Pane{{ScrollbackSHA: "abc"}}}},
	}}}
	raw, _ := json.Marshal(man)
	ev := store.Event{ID: 1, Kind: "snapshot", ManifestJSON: string(raw)}
	m := NewPickerModel(ModeSnapshot, []store.Event{ev}, nil, nil)
	m.Bootstrap()
	m.focus = focusTree
	m.treeCursor = paneNodeIndex(t, m)
	m.scrollbacks["abc"] = []byte("$ echo hi\nhi\n$ ")

	got := stripANSI(m.renderPreview(60))
	if !strings.Contains(got, "echo hi") {
		t.Errorf("expected content, got: %q", got)
	}
}

func TestRenderPreview_Error(t *testing.T) {
	man := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Windows: []snapshot.Window{{Panes: []snapshot.Pane{{ScrollbackSHA: "abc"}}}},
	}}}
	raw, _ := json.Marshal(man)
	ev := store.Event{ID: 1, Kind: "snapshot", ManifestJSON: string(raw)}
	m := NewPickerModel(ModeSnapshot, []store.Event{ev}, nil, nil)
	m.Bootstrap()
	m.focus = focusTree
	m.treeCursor = paneNodeIndex(t, m)
	m.scrollbackErrors["abc"] = errors.New("file gone")

	got := stripANSI(m.renderPreview(60))
	if !strings.Contains(got, "missing") {
		t.Errorf("expected error label, got: %q", got)
	}
}

// stripANSI removes ANSI escapes for assertion ergonomics.
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
