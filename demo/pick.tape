# Browse earlier snapshots and see what each pane had on screen before choosing.
#   go build -o demo/bin/tmux-remux ./cmd/tmux-remux
#   nix shell nixpkgs#vhs -c vhs demo/pick.tape
Output demo/pick.gif

Set Shell fish
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1500
Set Height 800
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set Padding 12

# Wider than undo.tape deliberately: the picker only lays out its scrollback
# preview at 120 columns or more, and it opens in a popup at 90% of the client —
# so 145 columns here leaves the picker 130, and undo.tape's 95 could never show
# a preview at all.
Hide
Type "./demo/setup --cols 145 --rows 36 --history"
Enter
Sleep 45s
Type "clear; tmux -L vhs attach -t demo"
Enter
Sleep 3s
Show

Sleep 1500ms

# The snapshot picker.
Ctrl+b
Sleep 400ms
Type "R"
Sleep 2500ms

# Tab moves into the contents tree; the panes of a live session count as
# running, so the default filter hides them until d turns it off.
Tab
Sleep 1200ms
Type "d"
Sleep 2s

# Walk down to the agent's pane — the preview shows what it had on screen. One
# stop short of the pane the split had only just created, whose scrollback is
# empty because the hook snapshotted it before its command drew anything.
Down
Sleep 800ms
Down
Sleep 2500ms
Escape
Sleep 800ms

# Now the other picker: close something first so there is something to browse.
# demo/setup handed focus back to the agent's pane, so this closes the one that
# has been logging for half a minute.
Ctrl+b
Sleep 400ms
Type "x"
Sleep 2500ms

# prefix+U lists what was closed. Its panes come out of the snapshot the close
# was diffed against, so they preview the same way.
Ctrl+b
Sleep 400ms
Type "U"
Sleep 2200ms

Tab
Sleep 1000ms
Type "d"
Sleep 1200ms
# Two stops: the first pane of the closed window is the plain shell, the second
# is the agent's, which is the one with output worth recognising.
Down
Sleep 700ms
Down
Sleep 3s

Hide
Escape
Sleep 500ms
Ctrl+b
Type ":kill-server"
Enter
