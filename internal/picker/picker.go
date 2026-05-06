// Package picker provides a bubbletea-based event picker.
package picker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/noamsto/tmux-state/internal/store"
)

// Item is one selectable row in the picker.
type Item struct {
	Key     string
	Display string
}

// Pick opens an interactive picker over rows and returns the selected key.
// Empty key = no selection (user cancelled with Esc/Ctrl-C). Renders against
// /dev/tty so it works inside `tmux display-popup -E`, where stdin/stdout
// may be redirected.
func Pick(ctx context.Context, rows []Item) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open /dev/tty: %w", err)
	}
	defer func() { _ = tty.Close() }()

	m := newModel(rows)
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(tty),
		tea.WithOutput(tty),
		tea.WithAltScreen(),
	)
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	return final.(model).selected, nil
}

// FormatRow renders an event for display. The leading event ID is carried
// separately as Item.Key so it doesn't need to be in the visible text.
func FormatRow(ev store.Event) string {
	t := time.UnixMilli(ev.Ts).Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%s  %-15s  %s", t, ev.Kind, ev.Reason)
}

type model struct {
	items    []Item
	filtered []int // indices into items
	cursor   int   // index into filtered
	query    string
	selected string
	quitting bool
	height   int
}

func newModel(items []Item) model {
	m := model{items: items, height: 20}
	m.refilter()
	return m
}

func (m *model) refilter() {
	q := strings.ToLower(m.query)
	m.filtered = m.filtered[:0]
	for i, it := range m.items {
		if q == "" || strings.Contains(strings.ToLower(it.Display), q) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if len(m.filtered) > 0 {
				m.selected = m.items[m.filtered[m.cursor]].Key
			}
			m.quitting = true
			return m, tea.Quit
		case "up", "ctrl+p", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+n", "ctrl+j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "backspace":
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.refilter()
			}
		default:
			if len(msg.Runes) > 0 {
				m.query += string(msg.Runes)
				m.refilter()
			}
		}
	}
	return m, nil
}

var (
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#cba6f7")).Bold(true)
	pointerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5e0dc")).Bold(true)
	selectedRow  = lipgloss.NewStyle().Background(lipgloss.Color("#313244")).Foreground(lipgloss.Color("#cdd6f4"))
	countStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086"))
)

func (m model) View() string {
	if m.quitting {
		return ""
	}
	listHeight := m.height - 2
	if listHeight < 1 {
		listHeight = 1
	}
	start := 0
	if m.cursor >= listHeight {
		start = m.cursor - listHeight + 1
	}
	end := start + listHeight
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		row := m.items[m.filtered[i]].Display
		if i == m.cursor {
			b.WriteString(pointerStyle.Render("▌ "))
			b.WriteString(selectedRow.Render(row))
		} else {
			b.WriteString("  ")
			b.WriteString(row)
		}
		b.WriteByte('\n')
	}
	for i := end - start; i < listHeight; i++ {
		b.WriteByte('\n')
	}
	b.WriteString(countStyle.Render(fmt.Sprintf("%d/%d", len(m.filtered), len(m.items))))
	b.WriteByte('\n')
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(m.query)
	return b.String()
}
