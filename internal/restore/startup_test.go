package restore_test

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/restore"
)

func TestBuildStartupCommand(t *testing.T) {
	tests := []struct {
		name string
		opts restore.StartupOpts
		want string
	}{
		{
			name: "empty: no scrollback no relaunch",
			opts: restore.StartupOpts{Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh"},
			want: "",
		},
		{
			name: "relaunch only",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				RelaunchCmd:  "nvim",
				RelaunchArgs: []string{"file.go"},
			},
			want: `nvim 'file.go'`,
		},
		{
			name: "scrollback only",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc123",
			},
			want: `'/usr/bin/tmux-remux' cat-scrollback abc123; exec /bin/zsh`,
		},
		{
			name: "scrollback + relaunch",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc123",
				RelaunchCmd:   "htop",
			},
			want: `'/usr/bin/tmux-remux' cat-scrollback abc123; exec htop`,
		},
		{
			name: "scrollback + bash gets -l",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/usr/bin/bash", IsBash: true,
				ScrollbackSHA: "abc123",
			},
			want: `'/usr/bin/tmux-remux' cat-scrollback abc123; exec /usr/bin/bash -l`,
		},
		{
			name: "self path with single quote gets escaped",
			opts: restore.StartupOpts{
				Self: "/weird'path/tmux-remux", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc",
			},
			want: `'/weird'\''path/tmux-remux' cat-scrollback abc; exec /bin/zsh`,
		},
		{
			name: "relaunch with multiple quoted args",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				RelaunchCmd:  "ssh",
				RelaunchArgs: []string{"-p", "2222", "host"},
			},
			want: `ssh '-p' '2222' 'host'`,
		},
		{
			name: "relaunch arg with shell metacharacters is single-quoted",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				RelaunchCmd:  "nvim",
				RelaunchArgs: []string{"$(touch pwned)"},
			},
			want: `nvim '$(touch pwned)'`,
		},
		{
			name: "override only: emitted verbatim, unquoted",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				OverrideCmd: "claude --resume abc-123",
			},
			want: `claude --resume abc-123`,
		},
		{
			name: "override + scrollback",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc123",
				OverrideCmd:   "claude --resume abc-123",
			},
			want: `'/usr/bin/tmux-remux' cat-scrollback abc123; exec claude --resume abc-123`,
		},
		{
			name: "override wins over relaunch",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-remux", DefaultShell: "/bin/zsh",
				RelaunchCmd: "nvim", RelaunchArgs: []string{"file.go"},
				OverrideCmd: "claude --resume abc-123",
			},
			want: `claude --resume abc-123`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := restore.BuildStartupCommand(tt.opts)
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}
