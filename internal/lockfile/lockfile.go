// Package lockfile provides a simple advisory file lock for serializing
// tmux-remux writers (save, capture-event).
package lockfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// retryInterval bounds how often Acquire re-polls a contended lock; short
// enough to feel instant, long enough not to busy-spin.
const retryInterval = 50 * time.Millisecond

// Lock holds an open file descriptor with an exclusive flock.
type Lock struct {
	f *os.File
}

// Acquire takes an exclusive flock on path, creating the file if needed,
// blocking until the lock is free or ctx is cancelled. Polling (rather than a
// blocking LOCK_EX) keeps the wait cancellable — a wedged holder must not trap
// every later invocation with no way to Ctrl+C out. Caller must Release.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	for {
		l, err := TryAcquire(path)
		if err != nil {
			return nil, err
		}
		if l != nil {
			return l, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}

// TryAcquire is like Acquire but returns (nil, nil) immediately if the lock
// is held by another process, instead of blocking.
func TryAcquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // path is project-controlled (cfg.LockPath)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // fd from os.File fits in int on all supported platforms
		_ = f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, nil
		}
		return nil, fmt.Errorf("flock %q: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// Release releases the flock and closes the file. Safe to call multiple times.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:gosec // fd from os.File fits in int on all supported platforms
	return f.Close()
}
