package manifest_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/noamsto/tmux-remux/pkg/manifest"
)

func TestCurrentVersion(t *testing.T) {
	if manifest.CurrentVersion != 1 {
		t.Fatalf("CurrentVersion = %d, want 1", manifest.CurrentVersion)
	}
}

func TestManifestJSONRoundTripAndUnknownFields(t *testing.T) {
	orig := manifest.Manifest{
		V:                 manifest.CurrentVersion,
		Host:              "host-a",
		TmuxPID:           4242,
		SavedAt:           1700000000000,
		ScrollbackSkipped: true,
		Bridged:           []string{"remote-1"},
		Sessions: []manifest.Session{
			{
				Name:         "work",
				LastAttached: 1699999999,
				PlacedBridges: []manifest.PlacedBridge{
					{Host: "box-b", RemoteSession: "dev"},
				},
				Windows: []manifest.Window{
					{
						Index:           0,
						Name:            "editor",
						Layout:          "abcd,80x24,0,0,1",
						ID:              "@1",
						AutomaticRename: true,
						Decoration:      map[string]string{"color": "blue"},
						Panes: []manifest.Pane{
							{
								Index:         0,
								Cwd:           "/tmp",
								Command:       "nvim",
								CommandArgs:   []string{"nvim", "main.go"},
								LastUsed:      1699999990,
								ChildCount:    1,
								ScrollbackSHA: "deadbeef",
								ID:            "%1",
								Relaunch:      "nvim main.go",
								Decoration:    map[string]string{"role": "lead"},
							},
						},
					},
				},
			},
		},
	}

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded manifest.Manifest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Fatalf("round-trip mismatch:\ngot:  %+v\nwant: %+v", decoded, orig)
	}

	// Verify unknown fields are ignored and missing optional fields stay zeroed.
	withExtra := `{"v":1,"host":"host-a","saved_at":1,"future_field":"ignored","sessions":[{"name":"s","last_attached":0,"windows":[]}]}`
	var partial manifest.Manifest
	if err := json.Unmarshal([]byte(withExtra), &partial); err != nil {
		t.Fatalf("Unmarshal with unknown field: %v", err)
	}
	if partial.V != 1 || partial.Host != "host-a" || partial.TmuxPID != 0 || partial.ScrollbackSkipped {
		t.Fatalf("unexpected decoded partial manifest: %+v", partial)
	}
}

func TestFingerprint(t *testing.T) {
	base := manifest.Manifest{
		V:       manifest.CurrentVersion,
		Host:    "host-a",
		SavedAt: 100,
		Sessions: []manifest.Session{
			{
				Name:         "work",
				LastAttached: 50,
				Windows: []manifest.Window{
					{
						Index:      0,
						Name:       "sh",
						Layout:     "layout1",
						Decoration: map[string]string{"k": "v1"},
						Panes: []manifest.Pane{
							{
								Index:      0,
								Cwd:        "/a",
								Command:    "bash",
								LastUsed:   40,
								Decoration: map[string]string{"p": "1"},
							},
						},
					},
				},
			},
		},
	}

	onlyTimeAndDecorationChanged := base
	onlyTimeAndDecorationChanged.SavedAt = 999
	onlyTimeAndDecorationChanged.Sessions = []manifest.Session{
		{
			Name:         "work",
			LastAttached: 888,
			Windows: []manifest.Window{
				{
					Index:      0,
					Name:       "sh",
					Layout:     "layout1",
					Decoration: map[string]string{"k": "v2"},
					Panes: []manifest.Pane{
						{
							Index:      0,
							Cwd:        "/a",
							Command:    "bash",
							LastUsed:   777,
							Decoration: map[string]string{"p": "2"},
						},
					},
				},
			},
		},
	}

	if base.Fingerprint() != onlyTimeAndDecorationChanged.Fingerprint() {
		t.Fatalf("Fingerprint changed when only timestamps/decorations changed")
	}

	cwdChanged := base
	cwdChanged.Sessions = []manifest.Session{
		{
			Name: "work",
			Windows: []manifest.Window{
				{
					Index:  0,
					Name:   "sh",
					Layout: "layout1",
					Panes: []manifest.Pane{
						{Index: 0, Cwd: "/b", Command: "bash"},
					},
				},
			},
		},
	}
	if base.Fingerprint() == cwdChanged.Fingerprint() {
		t.Fatalf("Fingerprint did not change when pane Cwd changed")
	}
}

func TestStructureFingerprint(t *testing.T) {
	base := manifest.Manifest{
		V:    manifest.CurrentVersion,
		Host: "host-a",
		Sessions: []manifest.Session{
			{
				Name:          "work",
				PlacedBridges: []manifest.PlacedBridge{{Host: "b", RemoteSession: "r"}},
				Windows: []manifest.Window{
					{
						Index:  0,
						ID:     "@1",
						Layout: "l1",
						Panes: []manifest.Pane{
							{Index: 0, ID: "%1", Cwd: "/a", Command: "bash", Relaunch: ""},
						},
					},
				},
			},
		},
	}

	cwdAndCmdChanged := manifest.Manifest{
		V:    manifest.CurrentVersion,
		Host: "host-a",
		Sessions: []manifest.Session{
			{
				Name:          "work",
				PlacedBridges: []manifest.PlacedBridge{{Host: "b", RemoteSession: "r"}},
				Windows: []manifest.Window{
					{
						Index:  0,
						ID:     "@1",
						Layout: "l2",
						Panes: []manifest.Pane{
							{Index: 0, ID: "%1", Cwd: "/other", Command: "zsh", ScrollbackSHA: "abc", Relaunch: ""},
						},
					},
				},
			},
		},
	}
	if base.StructureFingerprint() != cwdAndCmdChanged.StructureFingerprint() {
		t.Fatalf("StructureFingerprint changed when only cwd/command/layout/scrollback changed")
	}

	relaunchChanged := cwdAndCmdChanged
	relaunchChanged.Sessions[0].Windows[0].Panes[0].Relaunch = "claude"
	if base.StructureFingerprint() == relaunchChanged.StructureFingerprint() {
		t.Fatalf("StructureFingerprint did not change when Relaunch changed")
	}
}

func TestStripFormat(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "plain", want: "plain"},
		{in: "  #[fg=#94e2d5]editor#[default]  ", want: "editor"},
		{in: "#[bold]work #[fg=red]1", want: "work 1"},
	}
	for _, tc := range cases {
		if got := manifest.StripFormat(tc.in); got != tc.want {
			t.Errorf("StripFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
