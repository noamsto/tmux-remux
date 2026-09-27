package snapshot

import "golang.org/x/sys/unix"

// ChildCount returns the number of direct children of pid. macOS has no
// /proc, so it scans the process table for entries whose parent is pid.
// Returns 0 (no error) if pid is gone.
func ChildCount(pid int) (int, error) { return childCounter()(pid) }

// ParentPID returns pid's parent process id via the kern.proc.pid sysctl.
// Returns an error if pid is gone.
func ParentPID(pid int) (int, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	return int(kp.Eproc.Ppid), nil
}

// childCounter reads the process table once and answers every pid from it,
// so a Build costs one sysctl rather than one per pane.
func childCounter() func(int) (int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return func(int) (int, error) { return 0, err }
	}
	counts := make(map[int]int, len(procs))
	for i := range procs {
		counts[int(procs[i].Eproc.Ppid)]++
	}
	return func(pid int) (int, error) { return counts[pid], nil }
}
