package tmux_test

import (
	"context"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/tmux"
)

func TestRunReturnsStdoutTrimmed(t *testing.T) {
	c := tmux.NewClient("echo")
	out, err := c.Run(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(out, "\n"); got != "hello" {
		t.Errorf("Run = %q, want \"hello\"", got)
	}
}

func TestRunReturnsErrorOnNonZero(t *testing.T) {
	c := tmux.NewClient("false")
	_, err := c.Run(context.Background(), nil)
	if err == nil {
		t.Error("expected error for non-zero exit")
	}
}
