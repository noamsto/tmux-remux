package snapshot

import "errors"

// Proc is the slice of a process-table entry that relaunch ownership needs.
// Start is an opaque per-platform start time (Linux: clock ticks since boot;
// darwin: microseconds since epoch), only ever compared for equality to
// defeat pid reuse.
type Proc struct {
	PPID  int
	Start int64
	Comm  string
}

// ErrNoProcess reports that the pid is gone or only a zombie.
var ErrNoProcess = errors.New("no such process")
