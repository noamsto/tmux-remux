package tmux_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/tmux"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    tmux.Version
		wantErr bool
	}{
		{in: "tmux 3.4\n", want: tmux.Version{Major: 3, Minor: 4}},
		{in: "tmux 3.5a\n", want: tmux.Version{Major: 3, Minor: 5}},
		{in: "tmux next-3.8\n", want: tmux.Version{Major: 3, Minor: 8}},
		{in: "3.8", want: tmux.Version{Major: 3, Minor: 8}},
		{in: "tmux master\n", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, c := range cases {
		got, err := tmux.ParseVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		v     tmux.Version
		major int
		minor int
		want  bool
	}{
		{tmux.Version{Major: 3, Minor: 8}, 3, 8, true},
		{tmux.Version{Major: 3, Minor: 7}, 3, 8, false},
		{tmux.Version{Major: 3, Minor: 10}, 3, 8, true},
		{tmux.Version{Major: 4, Minor: 0}, 3, 8, true},
		{tmux.Version{Major: 2, Minor: 9}, 3, 8, false},
	}
	for _, c := range cases {
		if got := c.v.AtLeast(c.major, c.minor); got != c.want {
			t.Errorf("%v.AtLeast(%d, %d) = %v, want %v", c.v, c.major, c.minor, got, c.want)
		}
	}
}
