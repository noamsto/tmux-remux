// Command standin-agent mimics an agent's start hook for the integration
// tests: it stamps its pane via relaunch-stamp, then blocks. It is a non-shell
// process, so tests exercise both the agent-is-pane and agent-under-shell
// shapes on a real tmux. testdata/ is skipped by ./..., so it is built
// explicitly.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: standin-agent <remux-bin>")
		os.Exit(2)
	}
	cmd := exec.Command("sh", "-c", shellQuote(os.Args[1])+" relaunch-stamp --agent claude") //nolint:gosec // test stand-in; the binary path is test-controlled
	cmd.Stdin = strings.NewReader(`{"session_id":"standin-1"}`)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "standin-agent: relaunch-stamp: %v\n", err)
	}
	time.Sleep(time.Hour)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
