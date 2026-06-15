package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldSep is ASCII unit separator (U+001F), used as a tmux -F field separator.
// Safe because tmux session/window/pane names cannot contain control characters.
const FieldSep = "\x1f"

// SessionRow is a parsed tmux list-sessions row.
type SessionRow struct {
	Name         string
	LastAttached int64
}

// ParseSessions parses the output of `tmux list-sessions -F "<name>\x1f<last_attached>"`.
func ParseSessions(s string) ([]SessionRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []SessionRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: expected 2 fields, got %d (%q)", i+1, len(fields), line)
		}
		la, err := parseIntOrZero(fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: parse last_attached: %w", i+1, err)
		}
		out = append(out, SessionRow{Name: fields[0], LastAttached: la})
	}
	return out, nil
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// WindowRow is a parsed tmux list-windows row.
type WindowRow struct {
	Session         string
	Index           int
	Name            string
	Layout          string
	ID              string // tmux window id, e.g. "@4"
	AutomaticRename bool   // #{E:automatic-rename} == "1"
}

// ParseWindows parses tmux list-windows -F output.
func ParseWindows(s string) ([]WindowRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []WindowRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		if len(fields) != 6 {
			return nil, fmt.Errorf("window line %d: expected 6 fields, got %d", i+1, len(fields))
		}
		idx, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("window line %d: index: %w", i+1, err)
		}
		out = append(out, WindowRow{
			Session: fields[0], Index: idx, Name: fields[2], Layout: fields[3], ID: fields[4],
			AutomaticRename: fields[5] == "1",
		})
	}
	return out, nil
}

// PaneRow is a parsed tmux list-panes row.
type PaneRow struct {
	Session     string
	WindowIndex int
	PaneIndex   int
	Cwd         string
	Command     string
	PID         int
	LastUsed    int64
	ID          string // tmux pane id, e.g. "%3"
}

// ParsePanes parses tmux list-panes -F output.
func ParsePanes(s string) ([]PaneRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []PaneRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		if len(fields) != 8 {
			return nil, fmt.Errorf("pane line %d: expected 8 fields, got %d", i+1, len(fields))
		}
		wi, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: window_index: %w", i+1, err)
		}
		pi, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: pane_index: %w", i+1, err)
		}
		pid, err := strconv.Atoi(fields[5])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: pid: %w", i+1, err)
		}
		lu, err := parseIntOrZero(fields[6])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: last_used: %w", i+1, err)
		}
		out = append(out, PaneRow{
			Session: fields[0], WindowIndex: wi, PaneIndex: pi,
			Cwd: fields[3], Command: fields[4], PID: pid, LastUsed: lu, ID: fields[7],
		})
	}
	return out, nil
}

// parseIntOrZero parses s as an int64 in base 10. Empty strings return 0
// (handles tmux's empty session_last_attached / pane_last_used for never-
// attached sessions and freshly-created panes).
func parseIntOrZero(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}
