package closeevent_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/snapshot"
)

func TestOwnerSession(t *testing.T) {
	tests := []struct {
		name string
		post closeevent.CloseManifest
		item *closeevent.ClosedItem
		want string
	}{
		{
			name: "stored name wins",
			post: closeevent.CloseManifest{SessionName: "lazytmux"},
			item: &closeevent.ClosedItem{SessionName: "stale"},
			want: "lazytmux",
		},
		{
			name: "falls back to the snapshot diff",
			post: closeevent.CloseManifest{SessionID: "$3"},
			item: &closeevent.ClosedItem{SessionName: "mono"},
			want: "mono",
		},
		{
			name: "unrecoverable event with a stored name is still attributed",
			post: closeevent.CloseManifest{SessionName: "mono"},
			item: nil,
			want: "mono",
		},
		{
			name: "no name anywhere is unknown, never guessed from the id",
			post: closeevent.CloseManifest{SessionID: "$3"},
			item: nil,
			want: closeevent.UnknownSession,
		},
		{
			name: "an item with an empty name does not mask the unknown case",
			post: closeevent.CloseManifest{},
			item: &closeevent.ClosedItem{Session: &snapshot.Session{}},
			want: closeevent.UnknownSession,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := closeevent.OwnerSession(tc.post, tc.item); got != tc.want {
				t.Errorf("OwnerSession = %q, want %q", got, tc.want)
			}
		})
	}
}
