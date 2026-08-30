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

# demo/setup.fish builds the whole scene against a detached server (isolated
# data dir and socket, split, relaunch stamp, snapshot, agent running), waits
# until the agent is live, and exec-attaches. Hidden: the gif opens attached.
Hide
Type "./demo/setup.fish"
Enter
Sleep 8s
Show
Sleep 2.5s

# Oops. The pane-border caption is the keycast: VHS has no built-in one, a
# display-message dies at the next keypress, status-right is overridden by
# status-format themes, and typing into the attached client would echo into
# the agent pane — so it's set via the (hidden) command prompt.
Hide
Ctrl+b
Type ":set -g pane-border-status top; set -g pane-border-format '#[align=centre,bold] prefix + x — kill pane '"
Enter
Show
Sleep 800ms
Ctrl+b
Type "x"
Sleep 2.5s

# Undo.
Hide
Ctrl+b
Type ":set -g pane-border-format '#[align=centre,bold] prefix + u — undo '"
Enter
Show
Sleep 800ms
Ctrl+b
Type "u"
Sleep 4.5s

Hide
Ctrl+b
Type ":kill-server"
Enter
Sleep 1s
