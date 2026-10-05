//go:build !darwin

package snapshot

import (
	"fmt"
	"strings"
	"testing"
)

// statLine builds a stat line with fields 3..51 filled as 0 except state,
// ppid (field 4) and starttime (field 22).
func statLine(head, state string, ppid int, start int64) string {
	fields := make([]string, 49)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = state
	fields[1] = fmt.Sprint(ppid)
	fields[19] = fmt.Sprint(start)
	return head + " " + strings.Join(fields, " ")
}

func TestParseStat(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		want      Proc
		wantState byte
	}{
		{"normal", statLine("123 (bash)", "S", 45, 987654), Proc{PPID: 45, Start: 987654, Comm: "bash"}, 'S'},
		{"comm with parens and spaces", statLine("123 (a) (b c)", "R", 7, 42), Proc{PPID: 7, Start: 42, Comm: "a) (b c"}, 'R'},
		{"zombie", statLine("123 (defunct)", "Z", 1, 5), Proc{PPID: 1, Start: 5, Comm: "defunct"}, 'Z'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, state, err := parseStat(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || state != tt.wantState {
				t.Errorf("parseStat = %+v, %q; want %+v, %q", got, state, tt.want, tt.wantState)
			}
		})
	}
}

func TestParseStatErrors(t *testing.T) {
	tests := map[string]string{
		"too few fields":    "123 (bash) S 45 0 0",
		"missing close":     "123 (bash S 45 0 0",
		"missing parens":    "123 bash S 45 0 0",
		"close before open": "123 ) bash ( S 45",
		"bad ppid":          "123 (bash) S x" + strings.Repeat(" 0", 18) + " 1",
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseStat(line); err == nil {
				t.Errorf("parseStat(%q) succeeded, want error", line)
			}
		})
	}
}
