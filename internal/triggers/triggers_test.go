package triggers_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/noamsto/tmux-remux/internal/tmux"
	"github.com/noamsto/tmux-remux/internal/triggers"
)

var update = flag.Bool("update", false, "rewrite golden files")

// checkGolden compares got against the file at path, rewriting it under -update.
func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // generated config, world-readable by design
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read golden %s (run: go test ./internal/triggers/ -update): %v", path, err)
	}
	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s\nrun: go test ./internal/triggers/ -update", path, diff)
	}
}

func TestRenderLegacy(t *testing.T) {
	got := triggers.Render(triggers.Params{
		Bin:         "tmux-remux",
		Version:     tmux.Version{Major: 3, Minor: 7},
		AutoRestore: true,
	})
	checkGolden(t, filepath.Join("testdata", "legacy.conf"), got)
}

func TestRenderAutoRestoreOff(t *testing.T) {
	got := triggers.Render(triggers.Params{
		Bin:         "tmux-remux",
		Version:     tmux.Version{Major: 3, Minor: 7},
		AutoRestore: false,
	})
	if strings.Contains(got, "restore --auto") {
		t.Error("AutoRestore=false still emitted the restore --auto line")
	}
	if !strings.Contains(got, "set-hook -g session-created") {
		t.Error("AutoRestore=false dropped the save hooks")
	}
}

func TestRenderQuotesBinaryWithSpaces(t *testing.T) {
	got := triggers.Render(triggers.Params{
		Bin:         "/Apps/My Tools/tmux-remux",
		Version:     tmux.Version{Major: 3, Minor: 7},
		AutoRestore: true,
	})
	if !strings.Contains(got, `'\''/Apps/My Tools/tmux-remux'\''`) {
		t.Errorf("path with a space was not quoted:\n%s", got)
	}
}

// render38 is the fragment examples/tmux.conf is generated from.
func render38() string {
	return triggers.Render(triggers.Params{
		Bin:         "tmux-remux",
		Version:     tmux.Version{Major: 3, Minor: 8},
		AutoRestore: true,
	})
}

// examples/tmux.conf is the 3.8 render, checked in so people can read it.
func TestExamplesConfIsTheRender(t *testing.T) {
	checkGolden(t, filepath.Join("..", "..", "examples", "tmux.conf"), render38())
}

func TestRender38UsesTargetSessionForPaneExited(t *testing.T) {
	paneExited := lineContaining(t, render38(), "set-hook -g pane-exited")
	if strings.Contains(paneExited, "hook_session") {
		t.Errorf("3.8 pane-exited still reads hook_session (always empty there):\n%s", paneExited)
	}
	for _, want := range []string{"#{session_id}", "#{session_name}", "#{hook_pane}", "#{hook_window}"} {
		if !strings.Contains(paneExited, want) {
			t.Errorf("3.8 pane-exited missing %s:\n%s", want, paneExited)
		}
	}
}

func TestRender38EmitsMonitorSaveTick(t *testing.T) {
	got := render38()
	for _, want := range []string{
		`set -g @remux_save_tick '%M'`,
		`set-hook -g -B '@remux-save:session:#{T:@remux_save_tick}'`,
		"save --reason=timer",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("3.8 render missing %q", want)
		}
	}
}

func TestRenderLegacyHasNoMonitor(t *testing.T) {
	got := triggers.Render(triggers.Params{
		Bin:         "tmux-remux",
		Version:     tmux.Version{Major: 3, Minor: 7},
		AutoRestore: true,
	})
	if strings.Contains(got, "set-hook -g -B") {
		t.Error("pre-3.8 render emitted a monitor hook, which that tmux cannot parse")
	}
	if strings.Contains(got, "--reason=timer") {
		t.Error("pre-3.8 render emitted a timer save; that tmux needs the external timer")
	}
}

// lineContaining returns the single line of s containing sub.
func lineContaining(t *testing.T, s, sub string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one line containing %q, got %d", sub, len(found))
	}
	return found[0]
}
