package picker_test

import (
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/store"
)

func TestFormatRow(t *testing.T) {
	ev := store.Event{ID: 7, Ts: 1745700000000, Kind: "snapshot", Reason: "timer"}
	row := picker.FormatRow(ev)
	if !strings.Contains(row, "snapshot") || !strings.Contains(row, "timer") {
		t.Errorf("row missing kind/reason: %q", row)
	}
	if strings.HasPrefix(row, "7") {
		t.Errorf("ID must not be in display row (carried separately as Item.Key); got %q", row)
	}
}
