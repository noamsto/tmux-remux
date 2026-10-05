package snapshot

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/noamsto/tmux-remux/internal/tmux"
)

// Lister is the subset of tmux.Client used by Build. Lets tests inject a fake.
type Lister interface {
	ListSessions(context.Context) ([]tmux.SessionRow, error)
	ListWindows(context.Context) ([]tmux.WindowRow, error)
	ListPanes(context.Context) ([]tmux.PaneRow, error)
}

// newChildCounter is childCounter behind a package var, so tests can inject a
// counter that errors without touching the real process table.
var newChildCounter = childCounter

// newProcLookup is ProcInfo behind a package var, so tests can fake the
// process table.
var newProcLookup = func() func(int) (Proc, error) { return ProcInfo }

// Build queries the live tmux server via l and returns a Manifest. ChildCount
// is populated best-effort from the process table; a count error is stored
// as -1 (unknown) rather than mistaken for zero children. A relaunch stamp
// whose recorded owner process is gone is dropped.
func Build(ctx context.Context, l Lister, host string, savedAt int64) (Manifest, error) {
	var sessions []tmux.SessionRow
	var windows []tmux.WindowRow
	var panes []tmux.PaneRow

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		s, err := l.ListSessions(gctx)
		sessions = s
		return err
	})
	g.Go(func() error {
		w, err := l.ListWindows(gctx)
		windows = w
		return err
	})
	g.Go(func() error {
		p, err := l.ListPanes(gctx)
		panes = p
		return err
	})
	if err := g.Wait(); err != nil {
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

	countChildren := newChildCounter()
	lookup := newProcLookup()
	for _, s := range sessions {
		if s.BridgeHost != "" {
			m.Bridged = append(m.Bridged, s.Name)
			continue
		}
		sess := Session{Name: s.Name, LastAttached: s.LastAttached}
		for _, w := range winsBySess[s.Name] {
			layout := w.Layout
			if normalized, err := tmux.NormalizeLayout(layout); err == nil {
				layout = normalized
			}
			win := Window{Index: w.Index, Name: w.Name, Layout: layout, ID: w.ID, AutomaticRename: w.AutomaticRename, Decoration: w.Decoration}
			for _, p := range pansByWin[s.Name][w.Index] {
				if p.Floating {
					continue
				}
				cc, err := countChildren(p.PID)
				if err != nil {
					cc = -1
				}
				relaunch := p.Relaunch
				if relaunch != "" && relaunchStale(p.RelaunchOwner, relaunch, p.PID, lookup) {
					relaunch = ""
				}
				win.Panes = append(win.Panes, Pane{
					Index: p.PaneIndex, Cwd: p.Cwd, Command: p.Command,
					LastUsed:   p.LastUsed,
					ChildCount: cc,
					ID:         p.ID,
					Relaunch:   relaunch,
					Decoration: p.Decoration,
				})
			}
			sess.Windows = append(sess.Windows, win)
		}
		m.Sessions = append(m.Sessions, sess)
	}
	return m, nil
}
