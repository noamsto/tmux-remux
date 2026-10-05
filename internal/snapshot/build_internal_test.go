package snapshot

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/noamsto/tmux-remux/internal/tmux"
)

type errChildCounterLister struct{}

func (errChildCounterLister) ListSessions(context.Context) ([]tmux.SessionRow, error) {
	return []tmux.SessionRow{{Name: "s"}}, nil
}

func (errChildCounterLister) ListWindows(context.Context) ([]tmux.WindowRow, error) {
	return []tmux.WindowRow{{Session: "s", Index: 1}}, nil
}

func (errChildCounterLister) ListPanes(context.Context) ([]tmux.PaneRow, error) {
	return []tmux.PaneRow{{Session: "s", WindowIndex: 1, PaneIndex: 1, PID: 1}}, nil
}

func TestBuildStoresChildCountErrorAsNegativeOne(t *testing.T) {
	orig := newChildCounter
	t.Cleanup(func() { newChildCounter = orig })
	newChildCounter = func() func(int) (int, error) {
		return func(int) (int, error) { return 0, errors.New("boom") }
	}

	m, err := Build(context.Background(), errChildCounterLister{}, "h", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Sessions[0].Windows[0].Panes[0].ChildCount; got != -1 {
		t.Errorf("ChildCount = %d, want -1", got)
	}
}

type onePaneLister struct{ pane tmux.PaneRow }

func (onePaneLister) ListSessions(context.Context) ([]tmux.SessionRow, error) {
	return []tmux.SessionRow{{Name: "s"}}, nil
}

func (onePaneLister) ListWindows(context.Context) ([]tmux.WindowRow, error) {
	return []tmux.WindowRow{{Session: "s", Index: 1}}, nil
}

func (l onePaneLister) ListPanes(context.Context) ([]tmux.PaneRow, error) {
	return []tmux.PaneRow{l.pane}, nil
}

func TestBuildDropsStampOfDeadOwner(t *testing.T) {
	const (
		panePID = 100
		stamp   = "codex resume abc"
	)
	table := map[int]Proc{
		200: {PPID: 100, Start: 5000},
		100: {PPID: 1, Start: 4000},
		300: {PPID: 1, Start: 6000},
		310: {PPID: 200, Start: 7000},
		320: {PPID: 300, Start: 8000},
		330: {PPID: 250, Start: 9000},
		340: {PPID: 260, Start: 9500},
	}
	tests := []struct {
		name         string
		owner        string
		command      string
		lookupErr    error
		errPID       int
		wantRelaunch string
	}{
		{name: "owner gone", owner: FormatRelaunchOwner(999, 1, stamp), command: "nvim", wantRelaunch: ""},
		{name: "live child owner", owner: FormatRelaunchOwner(200, 5000, stamp), command: "nvim", wantRelaunch: stamp},
		{name: "owner is pane process", owner: FormatRelaunchOwner(100, 4000, stamp), command: "nvim", wantRelaunch: stamp},
		{name: "no owner record", owner: "", command: "zsh", wantRelaunch: stamp},
		{name: "start mismatch", owner: FormatRelaunchOwner(200, 4999, stamp), command: "nvim", wantRelaunch: ""},
		{name: "record bound to other stamp", owner: FormatRelaunchOwner(999, 1, "claude --resume old"), command: "nvim", wantRelaunch: stamp},
		{name: "owner reparented", owner: FormatRelaunchOwner(300, 6000, stamp), command: "nvim", wantRelaunch: ""},
		{name: "live grandchild owner", owner: FormatRelaunchOwner(310, 7000, stamp), command: "zsh", wantRelaunch: stamp},
		{name: "owner ancestry reaches init before the pane", owner: FormatRelaunchOwner(320, 8000, stamp), command: "nvim", wantRelaunch: ""},
		{name: "owner ancestor gone", owner: FormatRelaunchOwner(330, 9000, stamp), command: "nvim", wantRelaunch: ""},
		{name: "lookup error is unknown", owner: FormatRelaunchOwner(200, 5000, stamp), command: "nvim", lookupErr: errors.New("boom"), errPID: 200, wantRelaunch: stamp},
		{name: "ancestor lookup error is unknown", owner: FormatRelaunchOwner(340, 9500, stamp), command: "nvim", lookupErr: errors.New("boom"), errPID: 260, wantRelaunch: stamp},
		{name: "unparsable record", owner: "garbage", command: "nvim", wantRelaunch: stamp},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origLookup, origCounter := newProcLookup, newChildCounter
			t.Cleanup(func() { newProcLookup, newChildCounter = origLookup, origCounter })
			newProcLookup = func() func(int) (Proc, error) {
				return func(pid int) (Proc, error) {
					if pid == tc.errPID {
						return Proc{}, tc.lookupErr
					}
					p, ok := table[pid]
					if !ok {
						return Proc{}, fmt.Errorf("pid %d: %w", pid, ErrNoProcess)
					}
					return p, nil
				}
			}
			newChildCounter = func() func(int) (int, error) {
				return func(int) (int, error) { return 0, nil }
			}

			l := onePaneLister{pane: tmux.PaneRow{
				Session: "s", WindowIndex: 1, PaneIndex: 1, PID: panePID,
				Command: tc.command, Relaunch: stamp, RelaunchOwner: tc.owner,
			}}
			m, err := Build(context.Background(), l, "h", 1)
			if err != nil {
				t.Fatal(err)
			}
			got := m.Sessions[0].Windows[0].Panes[0]
			if got.Relaunch != tc.wantRelaunch {
				t.Errorf("Relaunch = %q, want %q", got.Relaunch, tc.wantRelaunch)
			}
			if got.Command != tc.command || got.ChildCount != 0 {
				t.Errorf("Command/ChildCount = %q/%d, want %q/0", got.Command, got.ChildCount, tc.command)
			}
		})
	}
}

func TestRelaunchOwnerRoundTrip(t *testing.T) {
	const stamp = "codex resume abc --flag"
	pid, start, gotStamp, ok := parseRelaunchOwner(FormatRelaunchOwner(42, 1234567, stamp))
	if !ok || pid != 42 || start != 1234567 || gotStamp != stamp {
		t.Errorf("parse = (%d, %d, %q, %v), want (42, 1234567, %q, true)", pid, start, gotStamp, ok, stamp)
	}
	for _, bad := range []string{"", "1 2", "x 2 s", "0 2 s", "1 y s"} {
		if _, _, _, ok := parseRelaunchOwner(bad); ok {
			t.Errorf("parseRelaunchOwner(%q) ok = true, want false", bad)
		}
	}
}
