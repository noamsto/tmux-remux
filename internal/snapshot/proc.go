//go:build !darwin

package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// childCounter returns the per-pid counter a Build uses. /proc answers each pid
// directly, so there is nothing to share across panes.
func childCounter() func(int) (int, error) { return ChildCount }

// ParentPID returns pid's parent process id, read from /proc/<pid>/stat.
// Returns an error if pid is gone.
func ParentPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) //nolint:gosec // /proc paths are project-controlled
	if err != nil {
		return 0, err
	}
	// comm (2nd field) is wrapped in parens and may itself contain spaces or
	// parens, so anchor on the LAST ')' rather than splitting naively.
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return 0, fmt.Errorf("parse /proc/%d/stat: no ')' found", pid)
	}
	fields := strings.Fields(string(data)[i+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("parse /proc/%d/stat: too few fields after comm", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("parse /proc/%d/stat ppid: %w", pid, err)
	}
	return ppid, nil
}

// ChildCount returns the number of direct children of pid, by reading
// /proc/<pid>/task/*/children. Returns 0 (no error) if pid is gone. If pid is
// alive but the glob matches nothing (kernel built without
// CONFIG_PROC_CHILDREN), the count is unknown rather than zero, so this
// returns an error instead of silently reporting an idle process.
func ChildCount(pid int) (int, error) {
	matches, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", pid))
	if err != nil {
		return 0, err
	}
	if len(matches) == 0 {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
			return 0, fmt.Errorf("no children files under /proc/%d/task", pid)
		}
		return 0, nil
	}
	seen := map[int]struct{}{}
	for _, m := range matches {
		data, err := os.ReadFile(m) //nolint:gosec // /proc paths are project-controlled
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("read %s: %w", m, err)
		}
		for _, f := range strings.Fields(string(data)) {
			n, err := strconv.Atoi(f)
			if err == nil {
				seen[n] = struct{}{}
			}
		}
	}
	return len(seen), nil
}
