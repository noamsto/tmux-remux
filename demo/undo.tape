# Kill the pane a coding agent is working in, then undo it — the pane comes
# back and the agent picks up where it was.
#   go build -o demo/bin/tmux-remux ./cmd/tmux-remux
#   nix shell nixpkgs#vhs -c vhs demo/undo.tape
Output demo/undo.gif

Set Shell fish
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1000
Set Height 560
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Padding 12

# All setup runs against a detached server so nothing races the attach or
# depends on typed keystrokes: isolated data dir (clean history, no host
# sessions), stock C-b prefix, split, relaunch stamp (what an agent start
# hook does), snapshot, agent running. The recording opens on the scene.
Hide
Type "set -x XDG_DATA_HOME /tmp/remux-demo; rm -rf /tmp/remux-demo /tmp/agent-work.step; tmux -L vhs kill-server 2>/dev/null; clear"
Enter
Sleep 1s
Type "tmux -L vhs new -d -s demo -c $PWD; tmux -L vhs set -g prefix C-b"
Enter
Sleep 3s
Type "tmux -L vhs split-window -h -t demo -c $PWD; tmux -L vhs set -p -t demo @remux_relaunch $PWD/demo/agent-work"
Enter
Sleep 500ms
Type "tmux -L vhs run-shell $PWD/demo/bin/tmux-remux' save'; tmux -L vhs send-keys -t demo ./demo/agent-work Enter; clear"
Enter
Sleep 2.5s
Show

Type "tmux -L vhs attach -t demo"
Enter
Sleep 3s

# Oops.
Ctrl+b
Type "x"
Sleep 2.5s

# Undo.
Ctrl+b
Type "u"
Sleep 4.5s

Hide
Ctrl+b
Type ":kill-server"
Enter
