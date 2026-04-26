package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ChildCount returns the number of direct children of pid, by reading
// /proc/<pid>/task/*/children. Returns 0 (no error) if pid is gone.
func ChildCount(pid int) (int, error) {
	matches, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", pid))
	if err != nil {
		return 0, err
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
