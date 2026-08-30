#!/usr/bin/env fish
# Demo scaffolding for undo.tape: isolated server + data dir, agent pane
# stamped and snapshotted, then exec-attach. Self-synchronizing so the tape
# needs no timing knowledge; it just waits for this to attach.
cd (dirname (status filename))/..

set -x XDG_DATA_HOME /tmp/remux-demo
tmux -L vhs kill-server 2>/dev/null
rm -rf /tmp/remux-demo /tmp/agent-work.step

tmux -L vhs new -d -s demo -c $PWD
tmux -L vhs set -g prefix C-b
tmux -L vhs split-window -h -t demo -c $PWD
tmux -L vhs set -p -t demo @remux_relaunch $PWD/demo/agent-work

# The relaunch stamp must reach a snapshot before the kill (what the 60s
# timer or next hook save does in real life).
tmux -L vhs run-shell "$PWD/demo/bin/tmux-remux save --reason=demo"

tmux -L vhs send-keys -t demo "./demo/agent-work" Enter
while not test -f /tmp/agent-work.step
    sleep 0.2
end

clear
exec tmux -L vhs attach -t demo
