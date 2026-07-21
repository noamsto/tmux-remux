package scrollback_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/noamsto/tmux-remux/internal/scrollback"
)

func TestPutHashesAndStores(t *testing.T) {
	dir := t.TempDir()
	store := scrollback.New(dir)
	ctx := context.Background()

	content := []byte("hello scrollback\n")
	sha, n, err := store.Put(ctx, content)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 64 {
		t.Fatalf("sha = %q (len %d), want 64 hex chars", sha, len(sha))
	}
	if n <= 0 {
		t.Errorf("bytes = %d, want > 0", n)
	}

	got, err := store.Get(ctx, sha)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Get returned %q, want %q", got, content)
	}
}

func TestPutIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := scrollback.New(dir)
	ctx := context.Background()

	content := []byte("idempotent")
	sha1, _, err := store.Put(ctx, content)
	if err != nil {
		t.Fatal(err)
	}
	sha2, _, err := store.Put(ctx, content)
	if err != nil {
		t.Fatal(err)
	}
	if sha1 != sha2 {
		t.Errorf("sha mismatch: %s vs %s", sha1, sha2)
	}
}

func TestPutShardsByPrefix(t *testing.T) {
	dir := t.TempDir()
	store := scrollback.New(dir)
	ctx := context.Background()
	sha, _, err := store.Put(ctx, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	expected := dir + "/" + sha[:2] + "/" + sha + ".zst"
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected file at %s: %v", expected, err)
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store := scrollback.New(dir)
	ctx := context.Background()
	sha, _, _ := store.Put(ctx, []byte("delete me"))
	if err := store.Delete(ctx, sha); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, sha); err == nil {
		t.Error("Get after Delete should error")
	}
}

func TestStreamReadsExistingSha(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := scrollback.New(dir)
	content := []byte("hello scrollback")
	sha, _, err := store.Put(ctx, content)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := store.Stream(ctx, sha)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestInvalidShaRejected(t *testing.T) {
	ctx := context.Background()
	store := scrollback.New(t.TempDir())
	// Malformed shas (traversal, short) must be rejected before path() slices
	// sha[:2] or escapes the store dir — not silently probed on disk.
	for _, sha := range []string{"", "a", "../../etc/passwd", "not-hex-" + strings.Repeat("z", 56)} {
		if _, err := store.Get(ctx, sha); err == nil {
			t.Errorf("Get(%q): expected error", sha)
		}
		if _, err := store.Stream(ctx, sha); err == nil {
			t.Errorf("Stream(%q): expected error", sha)
		}
		if err := store.Delete(ctx, sha); err == nil {
			t.Errorf("Delete(%q): expected error", sha)
		}
	}
}

func TestStreamMissingShaReturnsNotExist(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := scrollback.New(dir)
	_, err := store.Stream(ctx, "deadbeef00000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for missing sha")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}
