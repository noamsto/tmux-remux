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

func TestParseWindows(t *testing.T) {
	input := "lazytmux\x1f1\x1fmain\x1fabcd,200x50,0,0,1\nwork\x1f2\x1fbuild\x1fefgh,80x24,0,0,2\n"
	got, err := tmux.ParseWindows(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []tmux.WindowRow{
		{Session: "lazytmux", Index: 1, Name: "main", Layout: "abcd,200x50,0,0,1"},
		{Session: "work", Index: 2, Name: "build", Layout: "efgh,80x24,0,0,2"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseWindows mismatch (-want +got):\n%s", diff)
	}
}

func TestParsePanes(t *testing.T) {
	input := "lazytmux\x1f1\x1f1\x1f/home/me\x1fnvim\x1f12345\x1f1745700000\nlazytmux\x1f1\x1f2\x1f/tmp\x1fbash\x1f12346\x1f1745699000\n"
	got, err := tmux.ParsePanes(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []tmux.PaneRow{
		{Session: "lazytmux", WindowIndex: 1, PaneIndex: 1, Cwd: "/home/me", Command: "nvim", PID: 12345, LastUsed: 1745700000},
		{Session: "lazytmux", WindowIndex: 1, PaneIndex: 2, Cwd: "/tmp", Command: "bash", PID: 12346, LastUsed: 1745699000},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParsePanes mismatch (-want +got):\n%s", diff)
	}
}
