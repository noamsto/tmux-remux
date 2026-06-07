package snapshot

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/noamsto/tmux-state/internal/tmux"
)

// Lister is the subset of tmux.Client used by Build. Lets tests inject a fake.
type Lister interface {
	ListSessions(context.Context) ([]tmux.SessionRow, error)
	ListWindows(context.Context) ([]tmux.WindowRow, error)
	ListPanes(context.Context) ([]tmux.PaneRow, error)
	ShowWindowOptions(ctx context.Context, target string) (map[string]string, error)
}

// Build queries the live tmux server via l and returns a Manifest. ChildCount
// is populated best-effort from /proc; errors are ignored (missing PID just
// leaves it zero). Window user-options whose names start with one of
// optionPrefixes are captured into each Window's Options map; a nil/empty
// optionPrefixes disables window-option capture entirely.
func Build(ctx context.Context, l Lister, host string, savedAt int64, optionPrefixes []string) (Manifest, error) {
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

	for _, s := range sessions {
		sess := Session{Name: s.Name, LastAttached: s.LastAttached}
		for _, w := range winsBySess[s.Name] {
			win := Window{Index: w.Index, Name: w.Name, Layout: w.Layout, ID: w.ID}
			if len(optionPrefixes) > 0 {
				// session:index is unambiguous: tmux's session_check_name
				// rejects ':' and '.' in session names, so they can't collide
				// with the target separators (same assumption as SetLayout).
				target := fmt.Sprintf("%s:%d", s.Name, w.Index)
				// Best-effort like ChildCount: a failed show-options just
				// leaves Options empty. The snapshot package has no logger
				// plumbed through Build, so there's nowhere to surface it.
				if opts, err := l.ShowWindowOptions(ctx, target); err == nil {
					win.Options = filterByPrefix(opts, optionPrefixes)
				}
			}
			for _, p := range pansByWin[s.Name][w.Index] {
				cc, _ := ChildCount(p.PID)
				win.Panes = append(win.Panes, Pane{
					Index: p.PaneIndex, Cwd: p.Cwd, Command: p.Command,
					LastUsed:   p.LastUsed,
					ChildCount: cc,
					ID:         p.ID,
				})
			}
			sess.Windows = append(sess.Windows, win)
		}
		m.Sessions = append(m.Sessions, sess)
	}
	return m, nil
}

// filterByPrefix returns the subset of opts whose names start with any of the
// prefixes. Returns nil when nothing matches, so empty windows marshal without
// an "options" key (json omitempty) and don't perturb the fingerprint.
func filterByPrefix(opts map[string]string, prefixes []string) map[string]string {
	var out map[string]string
	for name, val := range opts {
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				if out == nil {
					out = map[string]string{}
				}
				out[name] = val
				break
			}
		}
	}
	return out
}
