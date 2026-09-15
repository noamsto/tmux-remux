package main

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/store"
)

func TestLanesToReap(t *testing.T) {
	const self = "/sock/self"
	const cutoff = 1000
	present := map[string]bool{"/sock/alive": true}
	exists := func(path string) bool { return present[path] }

	lanes := []store.ServerLane{
		{Key: self, NewestTs: 1},
		{Key: "/sock/alive", NewestTs: 1},
		{Key: "/sock/recent", NewestTs: 2000},
		{Key: "/sock/ghost", NewestTs: 1},
	}

	got := lanesToReap(lanes, self, cutoff, exists)
	want := []string{"/sock/ghost"}
	if len(got) != len(want) || (len(got) == 1 && got[0] != want[0]) {
		t.Errorf("lanesToReap() = %v, want %v", got, want)
	}
}
