// Package agenthook wires agent CLI start hooks to tmux-remux relaunch-stamp so
// panes restore their exact prior session. Each installer is idempotent and
// preserves the user's existing config.
package agenthook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codexMarker guards our managed block in ~/.codex/config.toml. Codex auto-loads
// only that one global file (no drop-in hooks dir), so we marker-append rather
// than own a file — matching lazytmux's proven approach.
const codexMarker = "# tmux-remux-managed: codex resume-on-restore SessionStart hook"

// InstallCodex idempotently appends a SessionStart hook block to the Codex
// config at path, wiring `<binary> relaunch-stamp --agent codex`. Creates the
// file if absent, never rewrites existing content, no-ops (changed=false) when
// the marker is already present. binary should be an absolute path.
func InstallCodex(path, binary string) (changed bool, err error) {
	existing, err := os.ReadFile(path) // #nosec G304 — path is ~/.codex/config.toml
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(existing), codexMarker) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 — user config dir
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) // #nosec G302, G304
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(codexBlock(binary)); err != nil {
		return false, err
	}
	return true, nil
}

func codexBlock(binary string) string {
	return fmt.Sprintf(`
%s
[[hooks.SessionStart]]
matcher = "startup|resume"

[[hooks.SessionStart.hooks]]
type = "command"
command = %q
timeout = 30
`, codexMarker, binary+" relaunch-stamp --agent codex")
}
