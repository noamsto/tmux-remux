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
		la, err := strconv.ParseInt(fields[1], 10, 64)
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
