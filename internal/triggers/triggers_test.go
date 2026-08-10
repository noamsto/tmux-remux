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
