package config

import (
	"reflect"
	"testing"
)

func TestParseDecorationColumns(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec string
		want []DecorationColumn
	}{
		{"empty", "", nil},
		{
			"two columns",
			"@issue_id:id:10,@pr_number:badge:6",
			[]DecorationColumn{
				{Option: "@issue_id", Role: RoleID, Max: 10},
				{Option: "@pr_number", Role: RoleBadge, Max: 6},
			},
		},
		{
			"text role",
			"@issue_title:text:0",
			[]DecorationColumn{{Option: "@issue_title", Role: RoleText}},
		},
		// A bad entry costs that column, not the whole list: an unparsable
		// spec must not blank the picker.
		{
			"malformed entry skipped",
			"@issue_id:id:10,garbage,@pr_number:badge:6",
			[]DecorationColumn{
				{Option: "@issue_id", Role: RoleID, Max: 10},
				{Option: "@pr_number", Role: RoleBadge, Max: 6},
			},
		},
		{"unknown role skipped", "@x:bogus:4", nil},
		{"non-numeric max skipped", "@x:id:wide", nil},
		{"missing @ prefix skipped", "issue_id:id:10", nil},
		{"whitespace tolerated", " @issue_id : id : 10 ", []DecorationColumn{
			{Option: "@issue_id", Role: RoleID, Max: 10},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDecorationColumns(tc.spec)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseDecorationColumns(%q) = %#v, want %#v", tc.spec, got, tc.want)
			}
		})
	}
}

// RoleText is the flex column; Max is meaningless for it and must not be
// carried, or a later width calculation could silently cap the title.
func TestParseDecorationColumns_TextIgnoresMax(t *testing.T) {
	got := ParseDecorationColumns("@issue_title:text:8")
	if len(got) != 1 || got[0].Max != 0 {
		t.Errorf("got %#v, want a single column with Max 0", got)
	}
}

// Capture must be the union: dropping the restore defaults would stop
// decoration surviving a restart, which is what DecorationOptions exists for.
func TestCaptureOptionsIsUnion(t *testing.T) {
	c := Default()
	c.DecorationColumns = []DecorationColumn{
		{Option: "@issue_id", Role: RoleID, Max: 10},
		{Option: "@crew_name", Role: RoleText}, // already a default; must not duplicate
	}
	got := c.CaptureOptions()
	want := []string{
		"@crew_name", "@crew_color",
		"pane-border-style", "pane-active-border-style",
		"pane-border-format", "pane-border-status",
		"@issue_id",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CaptureOptions() = %v, want %v", got, want)
	}
}

func TestDefaultHasNoDecorationColumns(t *testing.T) {
	if got := Default().DecorationColumns; got != nil {
		t.Errorf("Default().DecorationColumns = %#v, want nil — remux must not ship another tool's schema", got)
	}
}

func TestParseIgnoreWindows(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want []string
	}{
		{"", nil},
		{"[fingers]", []string{"[fingers]"}},
		{" [fingers] , scratch-* ", []string{"[fingers]", "scratch-*"}},
		{"[fingers],,", []string{"[fingers]"}}, // a trailing comma is harmless
	} {
		if got := ParseIgnoreWindows(tc.spec); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseIgnoreWindows(%q) = %#v, want %#v", tc.spec, got, tc.want)
		}
	}
}

func TestIgnoredWindow(t *testing.T) {
	pats := []string{"[fingers]", "scratch-*"}
	for _, tc := range []struct {
		name string
		want bool
	}{
		// Matched literally — as a glob "[fingers]" is a character class, so
		// path.Match alone would never match the window of that name.
		{"[fingers]", true},
		{"f", false}, // and it must not match the class's members either
		{"scratch-1", true},
		{"scratch", false}, // the * needs something to match
		{"my-[fingers]", false},
		{"editor", false},
	} {
		if got := IgnoredWindow(pats, tc.name); got != tc.want {
			t.Errorf("IgnoredWindow(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
	// A malformed glob costs its own entry, not the list. It needs a wildcard
	// to be treated as a glob at all — without one it is just a literal that
	// happens to contain a bracket.
	if IgnoredWindow([]string{"[unclosed*"}, "anything") {
		t.Error("a malformed glob should match nothing")
	}
	if !IgnoredWindow([]string{"[unclosed*", "editor"}, "editor") {
		t.Error("a malformed glob should not disable the entries after it")
	}
	if IgnoredWindow(nil, "editor") {
		t.Error("no patterns should ignore nothing")
	}
}
