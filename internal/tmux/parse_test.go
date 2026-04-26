package tmux_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-state/internal/tmux"
)

func TestParseSessions(t *testing.T) {
	input := "lazytmux\x1f1745700000\nwork\x1f1745699000\n"
	got, err := tmux.ParseSessions(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []tmux.SessionRow{
		{Name: "lazytmux", LastAttached: 1745700000},
		{Name: "work", LastAttached: 1745699000},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseSessions mismatch (-want +got):\n%s", diff)
	}
}

func TestParseSessionsEmpty(t *testing.T) {
	got, err := tmux.ParseSessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}
