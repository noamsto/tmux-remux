package picker_test

import (
	"testing"

	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/store"
)

func TestFormatRow(t *testing.T) {
	ev := store.Event{ID: 7, Ts: 1745700000000, Kind: "snapshot", Reason: "timer"}
	row := picker.FormatRow(ev)
	if row[0] != '7' {
		t.Errorf("row should start with id 7, got %q", row)
	}
}
