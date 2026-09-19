package config

import (
	"strconv"
	"strings"
)

// ColumnRole is how the close list renders a decoration column. The picker
// switches on the role and never on the option name, so a new field is a
// config change rather than a code change.
type ColumnRole int

// Column roles.
const (
	RoleID    ColumnRole = iota // compact identity, left-anchored ("ENG-8224", "#56")
	RoleBadge                   // short marker, right of the flex column ("#3511")
	RoleText                    // prose; the flex column
)

// DecorationColumn declares one close-list column sourced from a tmux window
// option. Max caps RoleID and RoleBadge; RoleText is the flex column and
// ignores it.
type DecorationColumn struct {
	Option string
	Role   ColumnRole
	Max    int
}

var columnRoles = map[string]ColumnRole{
	"id":    RoleID,
	"badge": RoleBadge,
	"text":  RoleText,
}

// ParseDecorationColumns reads the `@remux_columns` spec: comma-separated
// `option:role:max` triples. An entry that does not parse is skipped — an
// unusable column should cost that column, not blank the picker.
func ParseDecorationColumns(spec string) []DecorationColumn {
	var cols []DecorationColumn
	for _, entry := range strings.Split(spec, ",") {
		parts := strings.Split(entry, ":")
		if len(parts) != 3 {
			continue
		}
		option := strings.TrimSpace(parts[0])
		role, ok := columnRoles[strings.TrimSpace(parts[1])]
		if !ok || !strings.HasPrefix(option, "@") {
			continue
		}
		colMax, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil || colMax < 0 {
			continue
		}
		if role == RoleText {
			colMax = 0
		}
		cols = append(cols, DecorationColumn{Option: option, Role: role, Max: colMax})
	}
	return cols
}

// CaptureOptions is the allow-list of window options to snapshot: the restore
// allow-list plus every option a column reads. It must stay a union —
// DecorationOptions carries options captured purely so restore can replay
// them, which no column renders.
func (c Config) CaptureOptions() []string {
	seen := make(map[string]bool, len(c.DecorationOptions)+len(c.DecorationColumns))
	out := make([]string, 0, len(c.DecorationOptions)+len(c.DecorationColumns))
	for _, o := range c.DecorationOptions {
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	for _, col := range c.DecorationColumns {
		if !seen[col.Option] {
			seen[col.Option] = true
			out = append(out, col.Option)
		}
	}
	return out
}
