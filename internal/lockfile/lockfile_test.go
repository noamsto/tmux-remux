package lockfile_test

import (
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-remux/internal/lockfile"
)

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	l, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestTryAcquireBlockedReturnsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	l1, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Release()

	l2, err := lockfile.TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire returned error: %v", err)
	}
	if l2 != nil {
		t.Error("TryAcquire should return nil when lock is held")
		l2.Release()
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	l, _ := lockfile.Acquire(path)
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("second Release errored: %v", err)
	}
}
