package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/noamsto/tmux-remux/internal/snapshot"
)

type setCall struct{ pane, name, value string }

type fakeSetter struct {
	calls []setCall
	err   error

	panePID      int
	defaultShell string
	paneErr      error
}

func (f *fakeSetter) SetPaneOption(_ context.Context, pane, name, value string) error {
	f.calls = append(f.calls, setCall{pane, name, value})
	return f.err
}

func (f *fakeSetter) PaneProcess(context.Context, string) (int, string, error) {
	return f.panePID, f.defaultShell, f.paneErr
}

func run(t *testing.T, stdin string, opts relaunchStampOpts) *fakeSetter {
	t.Helper()
	return runWith(t, &fakeSetter{}, stdin, opts)
}

func runWith(t *testing.T, f *fakeSetter, stdin string, opts relaunchStampOpts) *fakeSetter {
	t.Helper()
	if err := runRelaunchStamp(context.Background(), f, strings.NewReader(stdin), opts); err != nil {
		t.Fatalf("runRelaunchStamp: %v", err)
	}
	return f
}

func assertCalls(t *testing.T, got []setCall, want ...setCall) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("calls = %+v, want %+v", got, want)
	}
}

func TestRelaunchStampClaudePreset(t *testing.T) {
	f := run(t, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	assertCalls(t, f.calls,
		setCall{"%3", "@remux_relaunch_owner", ""},
		setCall{"%3", "@remux_relaunch", "claude --resume abc-123"})
}

func TestRelaunchStampCodexPresetPositional(t *testing.T) {
	f := run(t, `{"session_id":"t-9"}`, relaunchStampOpts{agent: "codex", pane: "%1"})
	assertCalls(t, f.calls,
		setCall{"%1", "@remux_relaunch_owner", ""},
		setCall{"%1", "@remux_relaunch", "codex resume t-9"})
}

func TestRelaunchStampCmdEscapeHatch(t *testing.T) {
	f := run(t, `{"session_id":"z"}`, relaunchStampOpts{cmdTemplate: "cursor-agent --resume {id}", pane: "%1"})
	assertCalls(t, f.calls,
		setCall{"%1", "@remux_relaunch_owner", ""},
		setCall{"%1", "@remux_relaunch", "cursor-agent --resume z"})
}

func TestRelaunchStampClearUnsetsWithoutStdin(t *testing.T) {
	f := run(t, "", relaunchStampOpts{pane: "%2", clear: true, owner: func(int, string) (int, int64) {
		t.Error("owner resolved on --clear")
		return 0, 0
	}})
	assertCalls(t, f.calls,
		setCall{"%2", "@remux_relaunch_owner", ""},
		setCall{"%2", "@remux_relaunch", ""})
}

func TestRelaunchStampRecordsOwner(t *testing.T) {
	f := &fakeSetter{panePID: 77, defaultShell: "/bin/zsh"}
	runWith(t, f, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3",
		owner: func(panePID int, defaultShell string) (int, int64) {
			if panePID != 77 || defaultShell != "/bin/zsh" {
				t.Errorf("owner(%d, %q), want (77, \"/bin/zsh\")", panePID, defaultShell)
			}
			return 4242, 99
		}})
	assertCalls(t, f.calls,
		setCall{"%3", "@remux_relaunch_owner", "4242 99 claude --resume abc-123"},
		setCall{"%3", "@remux_relaunch", "claude --resume abc-123"})
}

func TestRelaunchStampUnresolvedOwnerStillStamps(t *testing.T) {
	f := runWith(t, &fakeSetter{panePID: 77}, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3",
		owner: func(int, string) (int, int64) { return 0, 0 }})
	assertCalls(t, f.calls,
		setCall{"%3", "@remux_relaunch_owner", ""},
		setCall{"%3", "@remux_relaunch", "claude --resume abc-123"})
}

func TestRelaunchStampPaneProcessErrorStillStamps(t *testing.T) {
	f := runWith(t, &fakeSetter{paneErr: errors.New("no pane")}, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3",
		owner: func(int, string) (int, int64) {
			t.Error("owner resolved despite PaneProcess error")
			return 4242, 99
		}})
	assertCalls(t, f.calls,
		setCall{"%3", "@remux_relaunch_owner", ""},
		setCall{"%3", "@remux_relaunch", "claude --resume abc-123"})
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

func TestIsShellComm(t *testing.T) {
	for _, tc := range []struct {
		comm, defaultShell, loginShell string
		want                           bool
	}{
		{comm: "zsh", want: true},
		{comm: "-zsh", want: true},
		{comm: ".zsh-wrapped", want: true},
		{comm: "fish", want: true},
		{comm: "dash", defaultShell: "/bin/dash", want: true},
		{comm: "dash", want: false},
		{comm: "nu", loginShell: "/usr/bin/nu", want: true},
		{comm: "claude", defaultShell: "/bin/zsh", loginShell: "/bin/zsh", want: false},
		{comm: "2.1.3", want: false},
		{comm: "node", want: false},
		{comm: ".", want: false},
	} {
		if got := isShellComm(tc.comm, tc.defaultShell, tc.loginShell); got != tc.want {
			t.Errorf("isShellComm(%q, %q, %q) = %v, want %v", tc.comm, tc.defaultShell, tc.loginShell, got, tc.want)
		}
	}
}

type procLookup = func(int) (snapshot.Proc, error)

// procTree builds a fake process table from (pid, ppid, comm) triples; each
// pid's Start is pid*10 so a returned start can be checked against its owner.
func procTree(entries ...any) map[int]snapshot.Proc {
	tree := map[int]snapshot.Proc{}
	for i := 0; i < len(entries); i += 3 {
		pid := entries[i].(int)
		tree[pid] = snapshot.Proc{PPID: entries[i+1].(int), Start: int64(pid) * 10, Comm: entries[i+2].(string)}
	}
	return tree
}

func treeLookup(tree map[int]snapshot.Proc) procLookup {
	return func(pid int) (snapshot.Proc, error) {
		p, ok := tree[pid]
		if !ok {
			return snapshot.Proc{}, fmt.Errorf("pid %d: %w", pid, snapshot.ErrNoProcess)
		}
		return p, nil
	}
}

// revertedOwner is the reverted owner selection (the hook's ancestor directly
// under the pane), kept only to prove the agent-is-pane and nested-shell rows
// fail under it.
func revertedOwner(self, panePID int, parent func(int) (int, error)) int {
	pid := self
	for range 64 {
		if pid == panePID {
			return panePID
		}
		ppid, err := parent(pid)
		if err != nil || ppid <= 1 {
			return 0
		}
		if ppid == panePID {
			return pid
		}
		pid = ppid
	}
	return 0
}

func TestRelaunchOwner(t *testing.T) {
	// Pane is pid 100 throughout; the hook (self) is the last pid of each shape.
	const pane = 100
	for _, tc := range []struct {
		name         string
		tree         map[int]snapshot.Proc
		self         int
		defaultShell string
		// wrap, when set, replaces the tree lookup; it is called per row so
		// any state it keeps starts fresh.
		wrap func(procLookup) procLookup
		want int
		// redProof marks rows that revertedOwner must get wrong.
		redProof bool
	}{
		{
			name: "pane-is-shell zsh(pane)→claude→sh→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "claude", 300, 200, "sh", 400, 300, "tmux-remux"),
			self: 400, want: 200,
		},
		{
			name: "pane-is-shell zsh(pane)→node→codex→sh→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "node", 300, 200, "codex", 400, 300, "sh", 500, 400, "tmux-remux"),
			self: 500, want: 200,
		},
		{
			name: "pane-is-shell restore bash(pane, -c)→claude→stamp",
			tree: procTree(100, 1, "bash", 200, 100, "claude", 300, 200, "tmux-remux"),
			self: 300, want: 200,
		},
		{
			name: "pane-is-shell fish new-window fish(pane)→claude→sh→stamp",
			tree: procTree(100, 1, "fish", 200, 100, "claude", 300, 200, "sh", 400, 300, "tmux-remux"),
			self: 400, want: 200,
		},
		{
			name: "pane-is-shell login -zsh(pane)→claude→sh→stamp",
			tree: procTree(100, 1, "-zsh", 200, 100, "claude", 300, 200, "sh", 400, 300, "tmux-remux"),
			self: 400, want: 200,
		},
		{
			name: "pane-is-shell nix .zsh-wrapped(pane)→claude→sh→stamp",
			tree: procTree(100, 1, ".zsh-wrapped", 200, 100, "claude", 300, 200, "sh", 400, 300, "tmux-remux"),
			self: 400, want: 200,
		},
		{
			name: "pane-is-shell default-shell dash(pane)→claude→sh→stamp",
			tree: procTree(100, 1, "dash", 200, 100, "claude", 300, 200, "sh", 400, 300, "tmux-remux"),
			self: 400, defaultShell: "/bin/dash", want: 200,
		},
		{
			name: "nested-shell zsh(pane)→bash→claude→sh→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "bash", 300, 200, "claude", 400, 300, "sh", 500, 400, "tmux-remux"),
			self: 500, want: 300, redProof: true,
		},
		{
			name: "nested-shell nix shell fish(pane)→fish→.claude-wrapped→bash→stamp",
			tree: procTree(100, 1, "fish", 200, 100, "fish", 300, 200, ".claude-wrapped", 400, 300, "bash", 500, 400, "tmux-remux"),
			self: 500, want: 300, redProof: true,
		},
		{
			name: "accepted fail-open: non-shell launcher owns zsh(pane)→nvim→zsh→claude→sh→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "nvim", 300, 200, "zsh", 400, 300, "claude", 500, 400, "sh", 600, 500, "tmux-remux"),
			self: 600, want: 200,
		},
		{
			name: "agent-is-pane claude(pane)→sh→stamp",
			tree: procTree(100, 1, "claude", 200, 100, "sh", 300, 200, "tmux-remux"),
			self: 300, want: pane, redProof: true,
		},
		{
			name: "agent-is-pane exec-optimised claude(pane)→stamp",
			tree: procTree(100, 1, "claude", 200, 100, "tmux-remux"),
			self: 200, want: pane, redProof: true,
		},
		{
			name: "agent-is-pane version comm 2.1.3(pane)→sh→hookyard→sh→stamp",
			tree: procTree(100, 1, "2.1.3", 200, 100, "sh", 300, 200, "hookyard", 400, 300, "sh", 500, 400, "tmux-remux"),
			self: 500, want: pane, redProof: true,
		},
		{
			name: "agent-is-pane node(pane)→codex→codex-relaunch-→stamp",
			tree: procTree(100, 1, "node", 200, 100, "codex", 300, 200, "codex-relaunch-", 400, 300, "tmux-remux"),
			self: 400, want: pane, redProof: true,
		},
		{
			name: "owner-less self is the pane process",
			tree: procTree(100, 1, "tmux-remux"),
			self: pane, want: 0,
		},
		{
			name: "owner-less broken chain claude(pane)→[missing sh]→stamp",
			tree: procTree(100, 1, "claude", 300, 200, "tmux-remux"),
			self: 300, want: 0,
		},
		{
			name: "owner-less hook outside the pane init→sh→stamp",
			tree: procTree(100, 1, "claude", 500, 1, "sh", 600, 500, "tmux-remux"),
			self: 600, want: 0,
		},
		{
			name: "owner-less 64-step cap: parents climb forever, never reaching the pane",
			tree: procTree(100, 1, "zsh"),
			self: 1000,
			wrap: func(base procLookup) procLookup {
				return func(pid int) (snapshot.Proc, error) {
					if pid == pane {
						return base(pid)
					}
					return snapshot.Proc{PPID: pid + 1, Start: 1, Comm: "sh"}, nil
				}
			},
			want: 0,
		},
		{
			name: "owner-less run by hand at a prompt zsh(pane)→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "tmux-remux"),
			self: 200, want: 0,
		},
		{
			name: "owner-less run by hand in a nested shell zsh(pane)→bash→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "bash", 300, 200, "tmux-remux"),
			self: 300, want: 0, redProof: true,
		},
		{
			name: "owner-less pane comm unreadable claude(pane, EPERM)→stamp",
			tree: procTree(100, 1, "claude", 200, 100, "tmux-remux"),
			self: 200,
			wrap: func(base procLookup) procLookup {
				return func(pid int) (snapshot.Proc, error) {
					if pid == pane {
						return snapshot.Proc{}, errors.New("permission denied")
					}
					return base(pid)
				}
			},
			want: 0,
		},
		{
			name: "owner-less owner exits between walk and start read zsh(pane)→claude→sh→stamp",
			tree: procTree(100, 1, "zsh", 200, 100, "claude", 300, 200, "sh", 400, 300, "tmux-remux"),
			self: 400,
			wrap: func(base procLookup) procLookup {
				reads := 0
				return func(pid int) (snapshot.Proc, error) {
					if pid == 200 {
						reads++
						if reads > 1 {
							return snapshot.Proc{}, fmt.Errorf("pid %d: %w", pid, snapshot.ErrNoProcess)
						}
					}
					return base(pid)
				}
			},
			want: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := treeLookup(tc.tree)
			if tc.wrap != nil {
				info = tc.wrap(info)
			}
			isShell := func(c string) bool { return isShellComm(c, tc.defaultShell, "") }
			pid, start := relaunchOwner(tc.self, pane, info, isShell)
			var wantStart int64
			if tc.want != 0 {
				wantStart = tc.tree[tc.want].Start
			}
			if pid != tc.want || start != wantStart {
				t.Errorf("relaunchOwner = (%d, %d), want (%d, %d)", pid, start, tc.want, wantStart)
			}
			if !tc.redProof {
				return
			}
			parent := func(pid int) (int, error) {
				p, err := treeLookup(tc.tree)(pid)
				return p.PPID, err
			}
			if got := revertedOwner(tc.self, pane, parent); got == tc.want {
				t.Errorf("revertedOwner = %d also picks the pane; row does not discriminate", got)
			}
		})
	}
}
