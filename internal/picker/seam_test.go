package picker

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The picker renders decoration columns by ROLE. If an option name ever
// appears here, the config indirection has been bypassed and the coupling this
// package exists to avoid is back — so assert it as a test rather than trusting
// a convention.
func TestPickerNamesNoDecorationOptions(t *testing.T) {
	bad := regexp.MustCompile(`@(issue|pr|crew|window)_[a-z_]+`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		if m := bad.FindAllString(string(src), -1); len(m) > 0 {
			t.Errorf("%s names decoration options %v — route them through config.DecorationColumn instead", name, m)
		}
	}
}
