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

# demo/setup builds the whole scene on a detached throwaway server, so nothing
# visible depends on typed setup keystrokes. Autosuggestion goes off before
# anything else is typed: fish paints the rest of a matching history entry as
# soon as a command starts typing, and the setup commands land in history.
Hide
Type "set -g fish_autosuggestion_enabled 0"
Enter
Sleep 500ms
Type "./demo/setup; clear"
Enter
Sleep 10s
Show

Type "tmux -L vhs attach -t demo"
Enter
Sleep 3s

# Oops. The beat after the prefix keeps the two-step press legible; the bind
# itself then names the key it fired over the status line.
Ctrl+b
Sleep 400ms
Type "x"
Sleep 3s

# Undo.
Ctrl+b
Sleep 400ms
Type "u"
Sleep 5s

Hide
Ctrl+b
Type ":kill-server"
Enter
