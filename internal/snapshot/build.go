package snapshot

import (
	"context"

	"github.com/noamsto/tmux-state/internal/tmux"
)

// Lister is the subset of tmux.Client used by Build. Lets tests inject a fake.
type Lister interface {
	ListSessions(context.Context) ([]tmux.SessionRow, error)
	ListWindows(context.Context) ([]tmux.WindowRow, error)
	ListPanes(context.Context) ([]tmux.PaneRow, error)
}

// Build queries the live tmux server via l and returns a Manifest. ChildCount
// for each pane is left zero — populate via /proc separately if desired.
func Build(ctx context.Context, l Lister, host string, savedAt int64) (Manifest, error) {
	sessions, err := l.ListSessions(ctx)
	if err != nil {
		return Manifest{}, err
	}
	windows, err := l.ListWindows(ctx)
	if err != nil {
		return Manifest{}, err
	}
	panes, err := l.ListPanes(ctx)
	if err != nil {
		return Manifest{}, err
	}

	m := Manifest{V: 1, Host: host, SavedAt: savedAt}

	winsBySess := map[string][]tmux.WindowRow{}
	for _, w := range windows {
		winsBySess[w.Session] = append(winsBySess[w.Session], w)
	}
	pansByWin := map[string]map[int][]tmux.PaneRow{}
	for _, p := range panes {
		if pansByWin[p.Session] == nil {
			pansByWin[p.Session] = map[int][]tmux.PaneRow{}
		}
		pansByWin[p.Session][p.WindowIndex] = append(pansByWin[p.Session][p.WindowIndex], p)
	}

	for _, s := range sessions {
		sess := Session{Name: s.Name, LastAttached: s.LastAttached}
		for _, w := range winsBySess[s.Name] {
			win := Window{Index: w.Index, Name: w.Name, Layout: w.Layout}
			for _, p := range pansByWin[s.Name][w.Index] {
				win.Panes = append(win.Panes, Pane{
					Index: p.PaneIndex, Cwd: p.Cwd, Command: p.Command,
					LastUsed: p.LastUsed,
				})
			}
			sess.Windows = append(sess.Windows, win)
		}
		m.Sessions = append(m.Sessions, sess)
	}
	return m, nil
}
