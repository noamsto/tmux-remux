package snapshot_test

import (
	"encoding/json"
	"testing"

	"github.com/noamsto/tmux-remux/internal/snapshot"
)

func TestManifestRoundTrip(t *testing.T) {
	m := snapshot.Manifest{
		V:       1,
		Host:    "h",
		SavedAt: 100,
		Sessions: []snapshot.Session{
			{
				Name:         "s1",
				LastAttached: 99,
				Windows: []snapshot.Window{
					{
						Index: 1, Name: "main", Layout: "abcd,80x24,0,0,1",
						Panes: []snapshot.Pane{
							{Index: 1, Cwd: "/x", Command: "nvim", LastUsed: 99, ChildCount: 2},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got snapshot.Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.V != 1 || len(got.Sessions) != 1 || got.Sessions[0].Windows[0].Panes[0].Command != "nvim" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestFingerprintIgnoresTimestamps(t *testing.T) {
	a := snapshot.Manifest{V: 1, Host: "h", SavedAt: 1, Sessions: []snapshot.Session{{Name: "s"}}}
	b := snapshot.Manifest{V: 1, Host: "h", SavedAt: 2, Sessions: []snapshot.Session{{Name: "s"}}}
	if a.Fingerprint() != b.Fingerprint() {
		t.Errorf("fingerprint should ignore SavedAt: %s vs %s", a.Fingerprint(), b.Fingerprint())
	}
}

func TestFingerprintDifferentForDifferentStructure(t *testing.T) {
	a := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{Name: "s"}}}
	b := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{Name: "different"}}}
	if a.Fingerprint() == b.Fingerprint() {
		t.Errorf("expected different fingerprints")
	}
}

func TestFingerprintIgnoresDecoration(t *testing.T) {
	base := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{Name: "s", Windows: []snapshot.Window{{Index: 1}}}}}
	withDecor := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{Name: "s", Windows: []snapshot.Window{{
		Index: 1, Decoration: map[string]string{"@crew_color": "colour141"},
	}}}}}
	if base.Fingerprint() != withDecor.Fingerprint() {
		t.Error("Fingerprint changed when only Decoration differs")
	}
}
