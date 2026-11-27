package tmux

import (
	"fmt"
	"regexp"
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
	BridgeHost   string // session-scoped @bridge_host; non-empty marks a dedicated bridge-mirror session
	Created      int64  // #{session_created}, epoch seconds
	Windows      int    // #{session_windows}: number of windows
	Attached     int    // #{session_attached}: number of attached clients
	LastActivity int64  // #{session_activity}, epoch seconds
	// Extra holds the values of the extra -F fields requested via
	// Client.SetExtraFormats, keyed by field name; nil when none were requested.
	Extra map[string]string
}

// ParseSessions parses the output of list-sessions with the base format. It
// accepts the legacy two-field form ("<name>\x1f<last_attached>") and the
// superset form (name, last_attached, created, windows, attached, activity).
// BridgeHost is filled by Client.ListSessions, which reads it session-scoped
// (show-options), never from this -F output — a session format resolves the
// current window first.
func ParseSessions(s string) ([]SessionRow, error) {
	return ParseSessionsExtra(s, nil)
}

// ParseSessionsExtra is ParseSessions for output whose lines carry len(extras)
// trailing fields, stored in SessionRow.Extra by name. With extras the
// superset form is required: the legacy form is accepted only when extras is
// empty, so the field count is never ambiguous.
func ParseSessionsExtra(s string, extras []string) ([]SessionRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []SessionRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		legacy := len(extras) == 0 && len(fields) == 2
		if !legacy && len(fields) != 6+len(extras) {
			return nil, fmt.Errorf("line %d: expected %d fields, got %d (%q)", i+1, 6+len(extras), len(fields), line)
		}
		la, err := parseIntOrZero(fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: parse last_attached: %w", i+1, err)
		}
		row := SessionRow{Name: fields[0], LastAttached: la}
		if !legacy {
			nums := make([]int64, 4)
			for j, name := range []string{"created", "windows", "attached", "activity"} {
				if nums[j], err = parseIntOrZero(fields[2+j]); err != nil {
					return nil, fmt.Errorf("line %d: parse %s: %w", i+1, name, err)
				}
			}
			row.Created, row.Windows, row.Attached, row.LastActivity = nums[0], int(nums[1]), int(nums[2]), nums[3]
			row.Extra = extraMap(extras, fields[6:])
		}
		out = append(out, row)
	}
	return out, nil
}

// extraMap pairs names with values; nil when names is empty.
func extraMap(names, values []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]string, len(names))
	for i, n := range names {
		m[n] = values[i]
	}
	return m
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
	// BridgeHost, BridgeSession and BridgePlaced are the window-scoped
	// @bridge_host/@bridge_session/@bridge_placed a placed bridge mirror
	// carries (og-remote-open --into, tmux-og #985). ListWindows captures them
	// with show-options -w, so a dedicated mirror's session-scoped @bridge_host
	// never appears here.
	BridgeHost    string
	BridgeSession string
	BridgePlaced  bool
	Decoration    map[string]string // allow-listed options captured via show-options -qv; nil when none set
	Active        bool              // #{window_active}
	Panes         int               // #{window_panes}: number of panes
	LastActivity  int64             // #{window_activity}, epoch seconds
	Path          string            // #{pane_current_path} of the window's active pane
	// Extra holds the extra -F fields requested via Client.SetExtraFormats,
	// keyed by field name; nil when none were requested.
	Extra map[string]string
}

// ParseWindows parses list-windows -F output: the legacy six-field base form
// or the ten-field superset (adding active, panes, activity, path).
// Decoration is captured separately via CaptureDecoration and set on
// WindowRow.Decoration by ListWindows.
func ParseWindows(s string) ([]WindowRow, error) {
	return ParseWindowsExtra(s, nil)
}

// ParseWindowsExtra is ParseWindows for output with len(extras) trailing
// fields, stored in WindowRow.Extra by name. With extras the superset form is
// required.
func ParseWindowsExtra(s string, extras []string) ([]WindowRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []WindowRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		legacy := len(extras) == 0 && len(fields) == 6
		if !legacy && len(fields) != 10+len(extras) {
			return nil, fmt.Errorf("window line %d: expected %d fields, got %d", i+1, 10+len(extras), len(fields))
		}
		idx, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("window line %d: index: %w", i+1, err)
		}
		row := WindowRow{
			Session: fields[0], Index: idx, Name: fields[2], Layout: fields[3], ID: fields[4],
			AutomaticRename: fields[5] == "1",
		}
		if !legacy {
			panes, err := parseIntOrZero(fields[7])
			if err != nil {
				return nil, fmt.Errorf("window line %d: panes: %w", i+1, err)
			}
			act, err := parseIntOrZero(fields[8])
			if err != nil {
				return nil, fmt.Errorf("window line %d: activity: %w", i+1, err)
			}
			row.Active, row.Panes, row.LastActivity, row.Path = fields[6] == "1", int(panes), act, fields[9]
			row.Extra = extraMap(extras, fields[10:])
		}
		out = append(out, row)
	}
	return out, nil
}

// PaneRow is a parsed tmux list-panes row.
type PaneRow struct {
	Session       string
	WindowIndex   int
	PaneIndex     int
	Cwd           string
	Command       string
	PID           int
	LastUsed      int64
	ID            string // tmux pane id, e.g. "%3"
	Relaunch      string // @remux_relaunch pane option; verbatim relaunch command, empty when unset
	RelaunchOwner string // @remux_relaunch_owner pane option: "<pid> <start> <stamp>" binding Relaunch to its owning process, empty when unset
	Floating      bool
	Decoration    map[string]string // allow-listed options captured via show-options -qv; nil when none set
	Active        bool              // #{pane_active}
	Title         string            // #{pane_title}
	ServerPID     int               // #{pid}: pid of the tmux server answering for the pane
	// Extra holds the extra -F fields requested via Client.SetExtraFormats,
	// keyed by field name; nil when none were requested.
	Extra map[string]string
}

// ParsePanes parses list-panes -F output: the legacy eleven-field base form or
// the fourteen-field superset (adding active, title, server pid).
func ParsePanes(s string) ([]PaneRow, error) {
	return ParsePanesExtra(s, nil)
}

// ParsePanesExtra is ParsePanes for output with len(extras) trailing fields,
// stored in PaneRow.Extra by name. With extras the superset form is required.
func ParsePanesExtra(s string, extras []string) ([]PaneRow, error) {
	if s == "" {
		return nil, nil
	}
	var out []PaneRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		legacy := len(extras) == 0 && len(fields) == 11
		if !legacy && len(fields) != 14+len(extras) {
			return nil, fmt.Errorf("pane line %d: expected %d fields, got %d", i+1, 14+len(extras), len(fields))
		}
		wi, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: window_index: %w", i+1, err)
		}
		pi, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: pane_index: %w", i+1, err)
		}
		pid, err := parseIntOrZero(fields[5])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: pid: %w", i+1, err)
		}
		lu, err := parseIntOrZero(fields[6])
		if err != nil {
			return nil, fmt.Errorf("pane line %d: last_used: %w", i+1, err)
		}
		row := PaneRow{
			Session: fields[0], WindowIndex: wi, PaneIndex: pi,
			Cwd: fields[3], Command: fields[4], PID: int(pid), LastUsed: lu, ID: fields[7],
			Relaunch: fields[8], Floating: fields[9] == "1", RelaunchOwner: fields[10],
		}
		if !legacy {
			spid, err := parseIntOrZero(fields[13])
			if err != nil {
				return nil, fmt.Errorf("pane line %d: server pid: %w", i+1, err)
			}
			row.Active, row.Title, row.ServerPID = fields[11] == "1", fields[12], int(spid)
			row.Extra = extraMap(extras, fields[14:])
		}
		out = append(out, row)
	}
	return out, nil
}

// parseIntOrZero parses s as an int64 in base 10. Empty strings return 0
// (handles tmux's empty session_last_attached / pane_last_used for never-
// attached sessions and freshly-created panes, and the empty pane_pid /
// pane_last_used a dead pane reports under remain-on-exit).
func parseIntOrZero(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

var tmuxStyleRe = regexp.MustCompile(`#\[[^\]]*\]`)

// StripTmuxEscapes removes tmux style directives such as "#[fg=red]" from s
// (as found in window and session names set from a status format) and trims
// surrounding whitespace. It does not unescape backslash sequences: with -u
// and a synthesized TMUX (see Client.Run), -F output is already literal.
func StripTmuxEscapes(s string) string {
	return strings.TrimSpace(tmuxStyleRe.ReplaceAllString(s, ""))
}
