package tmux

import "testing"

func TestParseOptionLines(t *testing.T) {
	out := "@thm_bg \"#1e1e2e\"\n" +
		"@remux_columns \"@issue_id:id:10,@pr_number:badge:6\"\n" +
		"status on\n" +
		"malformed\n" +
		"@empty \n"
	got := ParseOptionLines(out)

	for _, tc := range []struct{ key, want string }{
		{"@thm_bg", "#1e1e2e"},
		{"@remux_columns", "@issue_id:id:10,@pr_number:badge:6"},
		{"status", "on"},
	} {
		if got[tc.key] != tc.want {
			t.Errorf("ParseOptionLines()[%q] = %q, want %q", tc.key, got[tc.key], tc.want)
		}
	}
	if _, ok := got["malformed"]; ok {
		t.Error("a line with no separator must not produce a key")
	}
}
