package snapshot_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/tmux"
)

type fakeClient struct {
	sessions []tmux.SessionRow
	windows  []tmux.WindowRow
	panes    []tmux.PaneRow
	// winOptions maps a "<session>:<index>" target to its raw window options.
	winOptions map[string]map[string]string
}

func (f *fakeClient) ListSessions(context.Context) ([]tmux.SessionRow, error) {
	return f.sessions, nil
}
func (f *fakeClient) ListWindows(context.Context) ([]tmux.WindowRow, error) { return f.windows, nil }
func (f *fakeClient) ListPanes(context.Context) ([]tmux.PaneRow, error)     { return f.panes, nil }
func (f *fakeClient) ShowWindowOptions(_ context.Context, target string) (map[string]string, error) {
	return f.winOptions[target], nil
}

func TestBuildAssemblesTree(t *testing.T) {
	fc := &fakeClient{
		sessions: []tmux.SessionRow{
			{Name: "s1", LastAttached: 100},
		},
		windows: []tmux.WindowRow{
			{Session: "s1", Index: 1, Name: "main", Layout: "L"},
		},
		panes: []tmux.PaneRow{
			{Session: "s1", WindowIndex: 1, PaneIndex: 1, Cwd: "/home", Command: "nvim", PID: 1234, LastUsed: 99},
			{Session: "s1", WindowIndex: 1, PaneIndex: 2, Cwd: "/tmp", Command: "bash", PID: 1235, LastUsed: 50},
		},
	}
	m, err := snapshot.Build(context.Background(), fc, "host1", 200, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Host != "host1" || m.SavedAt != 200 {
		t.Errorf("envelope wrong: %+v", m)
	}
	if len(m.Sessions) != 1 || m.Sessions[0].Name != "s1" {
		t.Fatalf("sessions: %+v", m.Sessions)
	}
	if len(m.Sessions[0].Windows) != 1 {
		t.Fatalf("windows: %+v", m.Sessions[0].Windows)
	}
	if len(m.Sessions[0].Windows[0].Panes) != 2 {
		t.Fatalf("panes: %+v", m.Sessions[0].Windows[0].Panes)
	}
}

func TestBuildCarriesWindowAndPaneIDs(t *testing.T) {
	fc := &fakeClient{
		sessions: []tmux.SessionRow{{Name: "s1"}},
		windows:  []tmux.WindowRow{{Session: "s1", Index: 1, ID: "@4"}},
		panes:    []tmux.PaneRow{{Session: "s1", WindowIndex: 1, PaneIndex: 1, ID: "%7"}},
	}
	m, err := snapshot.Build(context.Background(), fc, "h", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := m.Sessions[0].Windows[0]
	if w.ID != "@4" {
		t.Errorf("window ID = %q, want @4", w.ID)
	}
	if w.Panes[0].ID != "%7" {
		t.Errorf("pane ID = %q, want %%7", w.Panes[0].ID)
	}
}

func TestBuildCapturesAllowlistedWindowOptions(t *testing.T) {
	fc := &fakeClient{
		sessions: []tmux.SessionRow{{Name: "s1"}},
		windows:  []tmux.WindowRow{{Session: "s1", Index: 1}},
		panes:    []tmux.PaneRow{{Session: "s1", WindowIndex: 1, PaneIndex: 1}},
		winOptions: map[string]map[string]string{
			"s1:1": {
				"@branch":          "feat/x",
				"@issue_id":        "ENG-1",
				"@thm_fg":          "#fff", // not allow-listed
				"automatic-rename": "on",   // not allow-listed
			},
		},
	}
	m, err := snapshot.Build(context.Background(), fc, "h", 0, []string{"@branch", "@issue_"})
	if err != nil {
		t.Fatal(err)
	}
	got := m.Sessions[0].Windows[0].Options
	want := map[string]string{"@branch": "feat/x", "@issue_id": "ENG-1"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("options mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildSkipsWindowOptionsWhenNoPrefixes(t *testing.T) {
	fc := &fakeClient{
		sessions:   []tmux.SessionRow{{Name: "s1"}},
		windows:    []tmux.WindowRow{{Session: "s1", Index: 1}},
		panes:      []tmux.PaneRow{{Session: "s1", WindowIndex: 1, PaneIndex: 1}},
		winOptions: map[string]map[string]string{"s1:1": {"@branch": "feat/x"}},
	}
	m, err := snapshot.Build(context.Background(), fc, "h", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Sessions[0].Windows[0].Options != nil {
		t.Errorf("expected nil Options when no prefixes, got %v", m.Sessions[0].Windows[0].Options)
	}
}

func TestBuildPopulatesChildCountFromPID(t *testing.T) {
	// Use the current process PID as a sentinel — it has at least 0 children
	// and we can verify the field is set (not whatever the zero value is from
	// an uninitialized PID).
	selfPID := os.Getpid()
	fc := &fakeClient{
		sessions: []tmux.SessionRow{{Name: "s"}},
		windows:  []tmux.WindowRow{{Session: "s", Index: 1}},
		panes:    []tmux.PaneRow{{Session: "s", WindowIndex: 1, PaneIndex: 1, PID: selfPID}},
	}
	m, err := snapshot.Build(context.Background(), fc, "h", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// ChildCount should equal the actual count for this PID (>=0, deterministic).
	expected, _ := snapshot.ChildCount(selfPID)
	if m.Sessions[0].Windows[0].Panes[0].ChildCount != expected {
		t.Errorf("ChildCount = %d, want %d", m.Sessions[0].Windows[0].Panes[0].ChildCount, expected)
	}
}
