package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/scrollback"
)

func TestCatScrollbackStreamsExistingSha(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := scrollback.New(dir)
	content := []byte("history line one\nhistory line two\n")
	sha, _, err := store.Put(ctx, content)
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runCatScrollback(ctx, store, sha, &stdout); err != nil {
		t.Fatalf("runCatScrollback: %v", err)
	}
	if got := stdout.String(); got != string(content) {
		t.Errorf("stdout = %q, want %q", got, content)
	}
}

func TestCatScrollbackMissingShaSilentExitZero(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := scrollback.New(dir)
	missingSha := strings.Repeat("0", 64)

	var stdout bytes.Buffer
	if err := runCatScrollback(ctx, store, missingSha, &stdout); err != nil {
		t.Fatalf("expected swallow of missing-file error, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty, got %q", stdout.String())
	}
}

func TestCatScrollbackMalformedShaErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := scrollback.New(dir)

	var stdout bytes.Buffer
	if err := runCatScrollback(ctx, store, "not-a-sha", &stdout); err == nil {
		t.Fatal("expected error for malformed sha")
	}
}
