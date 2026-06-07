// Package applog provides the append-only operations log (state.log next to
// state.db). Restore decisions and command errors land here — run-shell -b
// discards stderr, so without a file the auto-restore path is a black box.
package applog

import (
	"fmt"
	"os"
	"time"
)

// maxSize is the rotation threshold: past it, Open moves the file aside to
// <path>.old and starts fresh. One generation of history is enough for
// diagnosing "what happened at the last server start".
const maxSize = 1 << 20 // 1 MB

// Logger appends timestamped lines to a single log file. Not safe for
// concurrent use within a process; cross-process appends are line-buffered
// single writes, which is sufficient for this log's diagnostic purpose.
type Logger struct {
	f *os.File
}

// Open opens (creating if needed) the log at path, rotating an oversized
// file to path+".old" first. Rotation is not coordinated across processes;
// simultaneous rotations may lose one .old generation — acceptable for a
// diagnostic log.
func Open(path string) (*Logger, error) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxSize {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path is project-controlled (cfg.LogPath)
	if err != nil {
		return nil, fmt.Errorf("open log %q: %w", path, err)
	}
	return &Logger{f: f}, nil
}

// Logf appends one RFC3339-timestamped line. Write errors are dropped — the
// log must never break the operation it documents.
func (l *Logger) Logf(format string, a ...any) {
	_, _ = fmt.Fprintf(l.f, time.Now().Format(time.RFC3339)+" "+format+"\n", a...)
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	return l.f.Close()
}
