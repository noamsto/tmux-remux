package snapshot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// FormatRelaunchOwner encodes the @remux_relaunch_owner record. It binds the
// owner to the exact stamp it vouches for, so a stamp rewritten by another
// writer is never judged by a stale owner.
func FormatRelaunchOwner(pid int, start int64, stamp string) string {
	return fmt.Sprintf("%d %d %s", pid, start, stamp)
}

// parseRelaunchOwner splits a record made by FormatRelaunchOwner. The stamp is
// the remainder and may itself contain spaces.
func parseRelaunchOwner(v string) (pid int, start int64, stamp string, ok bool) {
	parts := strings.SplitN(v, " ", 3)
	if len(parts) != 3 {
		return 0, 0, "", false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		return 0, 0, "", false
	}
	start, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, "", false
	}
	return pid, start, parts[2], true
}

// relaunchStale reports whether the process that recorded owner for stamp is
// gone. A missing, unparsable, or differently-stamped record is owner-less
// and never stale, leaving restore's idle-shell rule to decide. A lookup error
// other than ErrNoProcess is unknown, not gone, and fails open like a
// ChildCount of -1.
func relaunchStale(owner, stamp string, panePID int, lookup func(int) (Proc, error)) bool {
	pid, start, ownerStamp, ok := parseRelaunchOwner(owner)
	if !ok || ownerStamp != stamp {
		return false
	}
	p, err := lookup(pid)
	if err != nil {
		return errors.Is(err, ErrNoProcess)
	}
	if p.Start != start {
		return true
	}
	return pid != panePID && p.PPID != panePID
}
