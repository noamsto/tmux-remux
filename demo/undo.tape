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

# demo/setup builds the scene on a detached throwaway server, and the attach is
# hidden with it: the command is socket plumbing that says nothing about
# tmux-remux, and at typing speed it flashes past unreadably anyway. The
# recording opens on the scene itself, already attached.
Hide
Type "./demo/setup"
Enter
Sleep 7s
Type "clear; tmux -L vhs attach -t demo"
Enter
Sleep 3s
Show

# An agent mid-task in the lower pane. A beat to read it before anything moves.
Sleep 1500ms

# Oops. The beat after the prefix keeps the two-step press legible; the bind
# itself then names the key it fired, as a badge along the bottom.
Ctrl+b
Sleep 400ms
Type "x"
Sleep 3s

# Undo.
Ctrl+b
Sleep 400ms
Type "u"
Sleep 6s

Hide
Ctrl+b
Type ":kill-server"
Enter
