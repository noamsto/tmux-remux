package tmux

import (
	"os/exec"
	"strings"
)

// GlobalOptions returns `tmux show -g` as a map. Best effort: a failure to
// reach the server yields nil, and every caller treats a missing key as
// "unset" anyway.
func GlobalOptions(binary string) map[string]string {
	out, err := exec.Command(binary, "show", "-g").Output()
	if err != nil {
		return nil
	}
	return ParseOptionLines(string(out))
}

// ParseOptionLines parses `tmux show -g` output. Values are unquoted; a line
// with no space separator is skipped.
func ParseOptionLines(out string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		i := strings.IndexByte(line, ' ')
		if i <= 0 {
			continue
		}
		v := strings.TrimRight(line[i+1:], " \t\r")
		v = strings.Trim(v, "\"")
		m[line[:i]] = v
	}
	return m
}
