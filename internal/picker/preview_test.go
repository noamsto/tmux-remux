package picker

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/scrollback"
)

func TestLoadScrollbackCmd_ReturnsContent(t *testing.T) {
	tmp := t.TempDir()
	sb := scrollback.New(tmp)
	sha, _, err := sb.Put(context.Background(), []byte("hello scrollback"))
	if err != nil {
		t.Fatalf("seed scrollback: %v", err)
	}

	cmd := loadScrollbackCmd(sb, sha)
	if cmd == nil {
		t.Fatal("loadScrollbackCmd returned nil")
	}
	msg := cmd()
	loaded, ok := msg.(scrollbackLoadedMsg)
	if !ok {
		t.Fatalf("expected scrollbackLoadedMsg, got %T", msg)
	}
	if loaded.sha != sha {
		t.Errorf("sha mismatch: got %q want %q", loaded.sha, sha)
	}
	if loaded.err != nil {
		t.Errorf("unexpected err: %v", loaded.err)
	}
	if !strings.Contains(string(loaded.content), "hello scrollback") {
		t.Errorf("content mismatch: got %q", loaded.content)
	}
}

func TestLoadScrollbackCmd_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	sb := scrollback.New(tmp)
	// all-zeros is not a valid sha256 output for any input, so this file is guaranteed absent
	const missing = "0000000000000000000000000000000000000000000000000000000000000000"

	cmd := loadScrollbackCmd(sb, missing)
	msg := cmd()
	loaded, ok := msg.(scrollbackLoadedMsg)
	if !ok {
		t.Fatalf("expected scrollbackLoadedMsg, got %T", msg)
	}
	if loaded.err == nil {
		t.Fatal("expected err for missing scrollback, got nil")
	}
	if !errors.Is(loaded.err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist-chain error, got %v", loaded.err)
	}
}

func TestPickerModel_HandlesScrollbackLoadedMsg(t *testing.T) {
	m := NewPickerModel(ModeSnapshot, nil, nil, nil)
	msg := scrollbackLoadedMsg{sha: "deadbeef", content: []byte("hi"), err: nil}
	updated, _ := m.Update(msg)
	final := updated.(PickerModel)
	got, ok := final.ScrollbackFor("deadbeef")
	if !ok {
		t.Fatalf("cache miss for sha after loaded msg")
	}
	if string(got) != "hi" {
		t.Errorf("content mismatch: got %q want %q", got, "hi")
	}
}

func TestPickerModel_RemembersScrollbackError(t *testing.T) {
	m := NewPickerModel(ModeSnapshot, nil, nil, nil)
	wantErr := errors.New("boom")
	msg := scrollbackLoadedMsg{sha: "deadbeef", err: wantErr}
	updated, _ := m.Update(msg)
	final := updated.(PickerModel)
	if got := final.ScrollbackError("deadbeef"); !errors.Is(got, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, got)
	}
}
