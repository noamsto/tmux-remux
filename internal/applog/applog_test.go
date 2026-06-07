package applog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/applog"
)

func TestLogfAppendsTimestampedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.log")
	l, err := applog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Logf("restore: %d sessions", 3)
	l.Logf("second line")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), data)
	}
	if !strings.HasSuffix(lines[0], "restore: 3 sessions") {
		t.Errorf("line 0 = %q, want suffix \"restore: 3 sessions\"", lines[0])
	}
	// RFC3339 timestamps start with the year.
	if !strings.HasPrefix(lines[0], "20") {
		t.Errorf("line 0 = %q, want RFC3339 timestamp prefix", lines[0])
	}
}

func TestOpenRotatesOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.log")
	big := strings.Repeat("x", 1<<20+1)
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}

	l, err := applog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if _, err := os.Stat(path + ".old"); err != nil {
		t.Errorf("expected rotated file at %s.old: %v", path, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Errorf("fresh log size = %d, want 0", st.Size())
	}
}

func TestOpenAppendsToExistingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.log")
	l, err := applog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Logf("first")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	l, err = applog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Logf("second")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (second Open must append, not truncate):\n%s", len(lines), data)
	}
}
