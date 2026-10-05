package snapshot

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// szomb is the kernel's SZOMB process state.
const szomb = 5

// ChildCount returns the number of direct children of pid. macOS has no
// /proc, so it scans the process table for entries whose parent is pid.
// Returns 0 (no error) if pid is gone.
func ChildCount(pid int) (int, error) { return childCounter()(pid) }

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

// ProcInfo reads pid's parent, start time and command name from the kernel
// process table. A missing or zombie pid yields ErrNoProcess.
func ProcInfo(pid int) (Proc, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		// A gone pid makes the sysctl return a short read, which x/sys reports as EIO.
		if errors.Is(err, unix.EIO) || errors.Is(err, unix.ESRCH) {
			return Proc{}, fmt.Errorf("pid %d: %w", pid, ErrNoProcess)
		}
		return Proc{}, fmt.Errorf("sysctl kern.proc.pid %d: %w", pid, err)
	}
	if kp.Proc.P_stat == szomb {
		return Proc{}, fmt.Errorf("pid %d: %w", pid, ErrNoProcess)
	}
	return Proc{
		PPID:  int(kp.Eproc.Ppid),
		Start: kp.Proc.P_starttime.Sec*1_000_000 + int64(kp.Proc.P_starttime.Usec),
		Comm:  unix.ByteSliceToString(kp.Proc.P_comm[:]),
	}, nil
}
