package scrollback_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/noamsto/tmux-state/internal/scrollback"
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
