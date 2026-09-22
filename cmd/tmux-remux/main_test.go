package main

import (
	"slices"
	"testing"
)

// TestSplitCommaList covers the @remux_decoration_options /
// @remux_pane_decoration_options parsing helper: comma-separated, trimmed,
// empty entries dropped.
func TestSplitCommaList(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want []string
	}{
		{"empty", "", nil},
		{"single", "@crew_name", []string{"@crew_name"}},
		{"multiple with spaces", "@crew_name, pane-border-style ,@crew_color", []string{"@crew_name", "pane-border-style", "@crew_color"}},
		{"trailing comma", "@crew_name,", []string{"@crew_name"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCommaList(tt.spec)
			if !slices.Equal(got, tt.want) {
				t.Errorf("splitCommaList(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}
