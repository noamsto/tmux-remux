// Package lockfile provides a simple advisory file lock for serializing
// tmux-state writers (save, capture-event, index-update).
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Lock holds an open file descriptor with an exclusive flock.
type Lock struct {
	f *os.File
}

// Acquire opens (creating if needed) the file at path and takes an exclusive
// flock. Caller must Release.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("mkdir lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // path is project-controlled (cfg.LockPath)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil { //nolint:gosec // fd from os.File fits in int on all supported platforms
		_ = f.Close()
		return nil, fmt.Errorf("flock %q: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// TryAcquire is like Acquire but returns (nil, nil) immediately if the lock
// is held by another process, instead of blocking.
func TryAcquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
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
