//go:build !darwin

package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// childCounter returns the per-pid counter a Build uses. /proc answers each pid
// directly, so there is nothing to share across panes.
func childCounter() func(int) (int, error) { return ChildCount }

// ChildCount returns the number of direct children of pid, by reading
// /proc/<pid>/task/*/children. Returns 0 (no error) if pid is gone, and an
// error if pid is alive but has no children files (a kernel without
// CONFIG_PROC_CHILDREN), where the count is unknown rather than zero.
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
		for f := range strings.FieldsSeq(string(data)) {
			n, err := strconv.Atoi(f)
			if err == nil {
				seen[n] = struct{}{}
			}
		}
	}
	return len(seen), nil
}

// ProcInfo reads pid's parent, start time and command name from
// /proc/<pid>/stat. A missing, zombie or dead pid yields ErrNoProcess.
func ProcInfo(pid int) (Proc, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) //nolint:gosec // /proc paths are project-controlled
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return Proc{}, fmt.Errorf("pid %d: %w", pid, ErrNoProcess)
		}
		return Proc{}, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}
	p, state, err := parseStat(string(data))
	if err != nil {
		return Proc{}, fmt.Errorf("pid %d: %w", pid, err)
	}
	if state == 'Z' || state == 'X' {
		return Proc{}, fmt.Errorf("pid %d: %w", pid, ErrNoProcess)
	}
	return p, nil
}

// parseStat splits a /proc/<pid>/stat line. comm may itself contain spaces and
// parentheses, so it spans the first '(' to the last ')'; the fixed fields
// follow it.
func parseStat(data string) (Proc, byte, error) {
	open := strings.IndexByte(data, '(')
	closing := strings.LastIndexByte(data, ')')
	if open < 0 || closing < open {
		return Proc{}, 0, errors.New("malformed stat: no comm parentheses")
	}
	fields := strings.Fields(data[closing+1:])
	if len(fields) < 20 {
		return Proc{}, 0, fmt.Errorf("malformed stat: %d fields after comm, want >= 20", len(fields))
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return Proc{}, 0, fmt.Errorf("parse ppid: %w", err)
	}
	start, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return Proc{}, 0, fmt.Errorf("parse starttime: %w", err)
	}
	return Proc{PPID: ppid, Start: start, Comm: data[open+1 : closing]}, fields[0][0], nil
}
