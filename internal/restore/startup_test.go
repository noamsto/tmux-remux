package restore_test

import (
	"testing"

	"github.com/noamsto/tmux-state/internal/restore"
)

func TestBuildStartupCommand(t *testing.T) {
	tests := []struct {
		name string
		opts restore.StartupOpts
		want string
	}{
		{
			name: "empty: no scrollback no relaunch",
			opts: restore.StartupOpts{Self: "/usr/bin/tmux-state", DefaultShell: "/bin/zsh"},
			want: "",
		},
		{
			name: "relaunch only",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-state", DefaultShell: "/bin/zsh",
				RelaunchCmd:  "nvim",
				RelaunchArgs: []string{"file.go"},
			},
			want: `nvim "file.go"`,
		},
		{
			name: "scrollback only",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-state", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc123",
			},
			want: `'/usr/bin/tmux-state' cat-scrollback abc123; exec /bin/zsh`,
		},
		{
			name: "scrollback + relaunch",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-state", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc123",
				RelaunchCmd:   "htop",
			},
			want: `'/usr/bin/tmux-state' cat-scrollback abc123; exec htop`,
		},
		{
			name: "scrollback + bash gets -l",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-state", DefaultShell: "/usr/bin/bash", IsBash: true,
				ScrollbackSHA: "abc123",
			},
			want: `'/usr/bin/tmux-state' cat-scrollback abc123; exec /usr/bin/bash -l`,
		},
		{
			name: "self path with single quote gets escaped",
			opts: restore.StartupOpts{
				Self: "/weird'path/tmux-state", DefaultShell: "/bin/zsh",
				ScrollbackSHA: "abc",
			},
			want: `'/weird'\''path/tmux-state' cat-scrollback abc; exec /bin/zsh`,
		},
		{
			name: "relaunch with multiple quoted args",
			opts: restore.StartupOpts{
				Self: "/usr/bin/tmux-state", DefaultShell: "/bin/zsh",
				RelaunchCmd:  "ssh",
				RelaunchArgs: []string{"-p", "2222", "host"},
			},
			want: `ssh "-p" "2222" "host"`,
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
