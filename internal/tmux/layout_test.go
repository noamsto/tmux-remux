package tmux_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/tmux"
)

func TestNormalizeLayout(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "one floating pane",
			in:   "e2cc,80x24,0,0[80x24,0,0,1,30x7,9,3,2]<30x7,9,3,2>",
			want: "b25e,80x24,0,0,1",
		},
		{
			name: "multiple floating panes",
			in:   "a012,80x24,0,0{40x24,0,0,0,14x2,49,15,3,30x7,9,3,2,39x24,41,0,1}<14x2,49,15,3,30x7,9,3,2>",
			want: "8205,80x24,0,0{40x24,0,0,0,39x24,41,0,1}",
		},
		{
			name: "no floating panes",
			in:   "8205,80x24,0,0{40x24,0,0,0,39x24,41,0,1}",
			want: "8205,80x24,0,0{40x24,0,0,0,39x24,41,0,1}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tmux.NormalizeLayout(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("NormalizeLayout() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeLayoutRejectsInvalidChecksum(t *testing.T) {
	_, err := tmux.NormalizeLayout("0000,80x24,0,0,1")
	if err == nil {
		t.Fatal("NormalizeLayout accepted invalid checksum")
	}
}
