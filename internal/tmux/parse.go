package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldSep is the tab character, used as a tmux -F field separator. tmux 3.4
// vis-encodes "unsafe" control bytes in -F output (e.g. the unit separator
// U+001F becomes the literal 4-char string `\037`), which corrupts parsing;
// tab is one of the few control characters it emits verbatim across all
// versions. tmux names/paths for this tool's sessions never contain a tab.
const FieldSep = "\t"

// SessionRow is a parsed tmux list-sessions row.
type SessionRow struct {
	Name         string
	LastAttached int64
}

// ParseSessions parses the output of `tmux list-sessions -F "<name>\t<last_attached>"`.
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
	Session string
	Index   int
	Name    string
	Layout  string
	ID      string // tmux window id, e.g. "@4"
}

// ParseWindows parses tmux list-windows -F output.
func ParseWindows(s string) ([]WindowRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []WindowRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		if len(fields) != 5 {
			return nil, fmt.Errorf("window line %d: expected 5 fields, got %d", i+1, len(fields))
		}
		idx, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("window line %d: index: %w", i+1, err)
		}
		out = append(out, WindowRow{
			Session: fields[0], Index: idx, Name: fields[2], Layout: fields[3], ID: fields[4],
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

// ParseWindowOptions parses the output of `tmux show-options -w`, returning a
// name→value map. Each line is "<name> <value>". tmux quotes the value when it
// needs to: a value containing spaces or a double quote is double-quoted with
// embedded `"`/`\` backslash-escaped (e.g. `@issue_title "fix \"it\""`), and
// the empty value renders as two single-quote characters. Bare values are
// unquoted.
// (A single-quote inside an otherwise-quote-free value still yields double
// quotes — tmux never single-quotes a non-empty value here.) Lines that don't
// match a known prefix are dropped by the caller, not here.
func ParseWindowOptions(s string) map[string]string {
	out := map[string]string{}
	for _, line := range splitLines(s) {
		name, rawVal, hasVal := strings.Cut(line, " ")
		if name == "" {
			continue
		}
		if !hasVal {
			out[name] = ""
			continue
		}
		out[name] = unquoteOptionValue(rawVal)
	}
	return out
}

// unquoteOptionValue reverses tmux's value quoting. A single-quoted value is
// taken literally (tmux uses single quotes only to render the empty value, and
// does not escape inside them). A double-quoted value has its `\`-escapes
// removed. Bare values are returned as-is.
func unquoteOptionValue(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return v
	}
	inner := v[1 : len(v)-1]
	var b strings.Builder
	b.Grow(len(inner))
	escaped := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
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
