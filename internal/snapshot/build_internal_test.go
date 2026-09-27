package snapshot

import (
	"context"
	"errors"
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
