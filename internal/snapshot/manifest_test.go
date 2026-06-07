package snapshot_test

import (
	"encoding/json"
	"testing"

	"github.com/noamsto/tmux-state/internal/snapshot"
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

func withOptions(opts map[string]string) snapshot.Manifest {
	return snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Name:    "s",
		Windows: []snapshot.Window{{Index: 1, Options: opts}},
	}}}
}

func TestFingerprintChangesWhenWindowOptionChanges(t *testing.T) {
	a := withOptions(map[string]string{"@branch": "feat/x"})
	b := withOptions(map[string]string{"@branch": "feat/y"})
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("fingerprint should change when an allow-listed option value changes")
	}
}

func TestFingerprintStableAcrossMapInsertionOrder(t *testing.T) {
	a := withOptions(map[string]string{"@branch": "x", "@issue_id": "1"})
	b := withOptions(map[string]string{"@issue_id": "1", "@branch": "x"})
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("fingerprint must be insertion-order independent (json sorts map keys)")
	}
}
