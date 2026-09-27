package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type setCall struct{ pane, name, value string }

type fakeSetter struct {
	calls []setCall
	err   error

	panePID    int
	panePIDErr error
}

func (f *fakeSetter) SetPaneOption(_ context.Context, pane, name, value string) error {
	f.calls = append(f.calls, setCall{pane, name, value})
	return f.err
}

func (f *fakeSetter) PanePID(_ context.Context, _ string) (int, error) {
	return f.panePID, f.panePIDErr
}

func run(t *testing.T, stdin string, opts relaunchStampOpts) *fakeSetter {
	t.Helper()
	f := &fakeSetter{}
	if err := runRelaunchStamp(context.Background(), f, strings.NewReader(stdin), opts); err != nil {
		t.Fatalf("runRelaunchStamp: %v", err)
	}
	return f
}

func TestRelaunchStampClaudePreset(t *testing.T) {
	f := run(t, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	want := setCall{"%3", "@remux_relaunch", "claude --resume abc-123"}
	if len(f.calls) != 2 || f.calls[1] != want {
		t.Errorf("calls = %+v, want [..., %+v]", f.calls, want)
	}
}

func TestRelaunchStampCodexPresetPositional(t *testing.T) {
	f := run(t, `{"session_id":"t-9"}`, relaunchStampOpts{agent: "codex", pane: "%1"})
	if len(f.calls) != 2 || f.calls[1].value != "codex resume t-9" {
		t.Errorf("calls = %+v, want value \"codex resume t-9\"", f.calls)
	}
}

func TestRelaunchStampCmdEscapeHatch(t *testing.T) {
	f := run(t, `{"session_id":"z"}`, relaunchStampOpts{cmdTemplate: "cursor-agent --resume {id}", pane: "%1"})
	if len(f.calls) != 2 || f.calls[1].value != "cursor-agent --resume z" {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestRelaunchStampClearUnsetsWithoutStdin(t *testing.T) {
	f := run(t, "", relaunchStampOpts{pane: "%2", clear: true})
	want := []setCall{
		{"%2", "@remux_relaunch_pid", ""},
		{"%2", "@remux_relaunch", ""},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %+v, want %+v", f.calls, want)
	}
}

// TestRelaunchStampSetsOwnerPIDBeforeRelaunch proves the full stamp path binds
// the stamp to its owning process: panePID set to a real ancestor of this test
// process (its parent) means self (this process) is a direct child of
// panePID, so relaunchOwner resolves the owner to self's own pid.
func TestRelaunchStampSetsOwnerPIDBeforeRelaunch(t *testing.T) {
	f := &fakeSetter{panePID: os.Getppid()}
	if err := runRelaunchStamp(context.Background(), f, strings.NewReader(`{"session_id":"abc-123"}`),
		relaunchStampOpts{agent: "claude", pane: "%3"}); err != nil {
		t.Fatalf("runRelaunchStamp: %v", err)
	}
	want := []setCall{
		{"%3", "@remux_relaunch_pid", strconv.Itoa(os.Getpid())},
		{"%3", "@remux_relaunch", "claude --resume abc-123"},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %+v, want %+v", f.calls, want)
	}
}

// TestRelaunchStampPanePIDErrorYieldsUnknownOwner covers the pane vanishing
// between the hook firing and the PanePID lookup: the stamp must still be
// written (never fail the hook), just with an unknown ("") owner.
func TestRelaunchStampPanePIDErrorYieldsUnknownOwner(t *testing.T) {
	f := &fakeSetter{panePIDErr: errors.New("pane gone")}
	if err := runRelaunchStamp(context.Background(), f, strings.NewReader(`{"session_id":"abc-123"}`),
		relaunchStampOpts{agent: "claude", pane: "%3"}); err != nil {
		t.Fatalf("runRelaunchStamp: %v", err)
	}
	want := []setCall{
		{"%3", "@remux_relaunch_pid", ""},
		{"%3", "@remux_relaunch", "claude --resume abc-123"},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %+v, want %+v", f.calls, want)
	}
}

// TestRelaunchOwner table-tests the ancestor walk in isolation, with an
// injected parent lookup so it never touches the real process table.
func TestRelaunchOwner(t *testing.T) {
	tests := []struct {
		name    string
		self    int
		panePID int
		parent  func(int) (int, error)
		want    int
	}{
		{
			name:    "direct child",
			self:    100,
			panePID: 50,
			parent: func(pid int) (int, error) {
				if pid == 100 {
					return 50, nil
				}
				return 0, fmt.Errorf("unexpected lookup for pid %d", pid)
			},
			want: 100,
		},
		{
			name:    "deeper chain",
			self:    102,
			panePID: 50,
			parent: func(pid int) (int, error) {
				switch pid {
				case 102:
					return 101, nil
				case 101:
					return 50, nil
				}
				return 0, fmt.Errorf("unexpected lookup for pid %d", pid)
			},
			want: 101,
		},
		{
			name:    "self is pane pid",
			self:    50,
			panePID: 50,
			parent: func(pid int) (int, error) {
				return 0, fmt.Errorf("parent should not be called, got pid %d", pid)
			},
			want: 50,
		},
		{
			name:    "broken chain",
			self:    100,
			panePID: 50,
			parent: func(pid int) (int, error) {
				return 0, fmt.Errorf("process %d gone", pid)
			},
			want: 0,
		},
		{
			name:    "loop guard",
			self:    100,
			panePID: 50,
			parent: func(pid int) (int, error) {
				return pid + 1000, nil // never reaches panePID or <= 1
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relaunchOwner(tt.self, tt.panePID, tt.parent); got != tt.want {
				t.Errorf("relaunchOwner(%d, %d, ...) = %d, want %d", tt.self, tt.panePID, got, tt.want)
			}
		})
	}
}

func TestRelaunchStampNoIDDoesNotClobber(t *testing.T) {
	f := run(t, `{}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	if len(f.calls) != 0 {
		t.Errorf("expected no set call, got %+v", f.calls)
	}
}

func TestRelaunchStampRejectsShellMetacharacters(t *testing.T) {
	// The id is exec'd verbatim by /bin/sh -c on restore (via @remux_relaunch),
	// so a shell-unsafe id must be dropped, not stamped.
	for _, id := range []string{
		"abc; rm -rf ~",
		"$(touch pwned)",
		"a`id`",
		"a b",
		"a|b",
	} {
		payload, err := json.Marshal(map[string]string{"session_id": id})
		if err != nil {
			t.Fatal(err)
		}
		f := run(t, string(payload), relaunchStampOpts{agent: "claude", pane: "%3"})
		if len(f.calls) != 0 {
			t.Errorf("id %q: expected no stamp, got %+v", id, f.calls)
		}
	}
}

func TestRelaunchStampNoPaneNoOp(t *testing.T) {
	f := run(t, `{"session_id":"x"}`, relaunchStampOpts{agent: "claude", pane: ""})
	if len(f.calls) != 0 {
		t.Errorf("expected no set call, got %+v", f.calls)
	}
}

func TestRelaunchStampUnknownAgentErrors(t *testing.T) {
	f := &fakeSetter{}
	err := runRelaunchStamp(context.Background(), f, strings.NewReader(`{"session_id":"x"}`),
		relaunchStampOpts{agent: "bogus", pane: "%1"})
	if err == nil {
		t.Fatal("expected error for unknown agent with no --cmd")
	}
}
