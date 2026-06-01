package picker

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down                  key.Binding
	Left, Right               key.Binding
	Tab                       key.Binding
	ToggleIdle                key.Binding
	ToggleDedup               key.Binding
	ToggleAge                 key.Binding
	PreviewUp, PreviewDown    key.Binding
	PreviewLeft, PreviewRight key.Binding
	Enter                     key.Binding
	Help                      key.Binding
	Quit                      key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:         key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse")),
		Right:        key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand")),
		Tab:          key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		ToggleIdle:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "skip idle")),
		ToggleDedup:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dedup running")),
		ToggleAge:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "age ≤24h")),
		PreviewUp:    key.NewBinding(key.WithKeys("alt+k", "pgup"), key.WithHelp("M-k/PgUp", "preview ↑")),
		PreviewDown:  key.NewBinding(key.WithKeys("alt+j", "pgdown"), key.WithHelp("M-j/PgDn", "preview ↓")),
		PreviewLeft:  key.NewBinding(key.WithKeys("alt+h"), key.WithHelp("M-h", "preview ←")),
		PreviewRight: key.NewBinding(key.WithKeys("alt+l"), key.WithHelp("M-l", "preview →")),
		Enter:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "restore")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:         key.NewBinding(key.WithKeys("esc", "ctrl+c", "q"), key.WithHelp("q/esc", "quit")),
	}
}

// ShortHelp / FullHelp wire up bubbles/help.Model.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Right, k.Tab, k.Enter, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right, k.Tab},
		{k.ToggleIdle, k.ToggleDedup, k.ToggleAge},
		{k.Enter, k.Help, k.Quit},
	}
}
