package tmux

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a tmux release identified by major and minor number. Suffix
// letters (3.5a) and the "next-" prefix on development builds are dropped: a
// next-3.8 build carries the 3.8 feature set, which is what callers gate on.
type Version struct {
	Major int
	Minor int
}

func (v Version) String() string { return fmt.Sprintf("%d.%d", v.Major, v.Minor) }

// AtLeast reports whether v is major.minor or newer.
func (v Version) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

var versionRe = regexp.MustCompile(`([0-9]+)\.([0-9]+)`)

// ParseVersion reads a `tmux -V` line ("tmux 3.5a", "tmux next-3.8") or a bare
// "3.8" and returns the release it names.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("tmux: unrecognized version %q", s)
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return Version{}, fmt.Errorf("tmux: unrecognized version %q", s)
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return Version{}, fmt.Errorf("tmux: unrecognized version %q", s)
	}
	return Version{Major: major, Minor: minor}, nil
}

// Version runs `tmux -V` and parses the result.
func (c *Client) Version(ctx context.Context) (Version, error) {
	out, err := c.Run(ctx, []string{"-V"})
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(out)
}
