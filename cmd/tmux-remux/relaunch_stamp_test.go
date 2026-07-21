package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type setCall struct{ pane, name, value string }

type fakeSetter struct {
	calls []setCall
	err   error
}

func (f *fakeSetter) SetPaneOption(_ context.Context, pane, name, value string) error {
	f.calls = append(f.calls, setCall{pane, name, value})
	return f.err
}

func run(t *testing.T, stdin string, opts relaunchStampOpts) *fakeSetter {
	t.Helper()
	f := &fakeSetter{}
	if err := runRelaunchStamp(context.Background(), f, strings.NewReader(stdin), opts); err != nil {
		t.Fatalf("runRelaunchStamp: %v", err)
	}
	return f
}

func TestRelaunchStampClaudePreset(t *testing.T) {
	f := run(t, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	want := setCall{"%3", "@remux_relaunch", "claude --resume abc-123"}
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("calls = %+v, want [%+v]", f.calls, want)
	}
}

func TestRelaunchStampCodexPresetPositional(t *testing.T) {
	f := run(t, `{"session_id":"t-9"}`, relaunchStampOpts{agent: "codex", pane: "%1"})
	if len(f.calls) != 1 || f.calls[0].value != "codex resume t-9" {
		t.Errorf("calls = %+v, want value \"codex resume t-9\"", f.calls)
	}
}

func TestRelaunchStampCmdEscapeHatch(t *testing.T) {
	f := run(t, `{"session_id":"z"}`, relaunchStampOpts{cmdTemplate: "cursor-agent --resume {id}", pane: "%1"})
	if len(f.calls) != 1 || f.calls[0].value != "cursor-agent --resume z" {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestRelaunchStampClearUnsetsWithoutStdin(t *testing.T) {
	f := run(t, "", relaunchStampOpts{pane: "%2", clear: true})
	want := setCall{"%2", "@remux_relaunch", ""}
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("calls = %+v, want [%+v]", f.calls, want)
	}
}

func TestRelaunchStampNoIDDoesNotClobber(t *testing.T) {
	f := run(t, `{}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	if len(f.calls) != 0 {
		t.Errorf("expected no set call, got %+v", f.calls)
	}
}

func TestRelaunchStampRejectsShellMetacharacters(t *testing.T) {
	// The id is exec'd verbatim by /bin/sh -c on restore (via @remux_relaunch),
	// so a shell-unsafe id must be dropped, not stamped.
	for _, id := range []string{
		"abc; rm -rf ~",
		"$(touch pwned)",
		"a`id`",
		"a b",
		"a|b",
	} {
		payload, err := json.Marshal(map[string]string{"session_id": id})
		if err != nil {
			t.Fatal(err)
		}
		f := run(t, string(payload), relaunchStampOpts{agent: "claude", pane: "%3"})
		if len(f.calls) != 0 {
			t.Errorf("id %q: expected no stamp, got %+v", id, f.calls)
		}
	}
}

func TestRelaunchStampNoPaneNoOp(t *testing.T) {
	f := run(t, `{"session_id":"x"}`, relaunchStampOpts{agent: "claude", pane: ""})
	if len(f.calls) != 0 {
		t.Errorf("expected no set call, got %+v", f.calls)
	}
}

func TestRelaunchStampUnknownAgentErrors(t *testing.T) {
	f := &fakeSetter{}
	err := runRelaunchStamp(context.Background(), f, strings.NewReader(`{"session_id":"x"}`),
		relaunchStampOpts{agent: "bogus", pane: "%1"})
	if err == nil {
		t.Fatal("expected error for unknown agent with no --cmd")
	}
}
