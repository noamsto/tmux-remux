package restore

import (
	"strconv"
	"strings"
)

// StartupOpts is the input to BuildStartupCommand. Fields not relevant to a
// given branch may be left zero.
type StartupOpts struct {
	// Self is the absolute path of the running tmux-state binary, used to
	// invoke the cat-scrollback subcommand. Single-quoted on output.
	Self string
	// DefaultShell is the resolved shell to exec when no allow-listed
	// command is being relaunched.
	DefaultShell string
	// IsBash adds "-l" to DefaultShell's exec line when true.
	IsBash bool
	// ScrollbackSHA, if non-empty, prepends a cat-scrollback step.
	ScrollbackSHA string
	// RelaunchCmd, if non-empty, becomes the exec target instead of DefaultShell.
	RelaunchCmd string
	// RelaunchArgs are appended to RelaunchCmd, each strconv.Quote'd.
	// Contract: args are assumed to be plain printable ASCII. strconv.Quote
	// produces Go string syntax, not strict POSIX double-quote syntax;
	// non-ASCII or non-printable bytes would emit Go escape sequences
	// (\x.., \u...., \U........) that /bin/sh does not interpret. Snapshot
	// data from tmux's `list-panes -F` allow-listed commands is plain ASCII
	// in practice.
	RelaunchArgs []string
	// OverrideCmd, if non-empty, is exec'd verbatim as the relaunch target,
	// taking precedence over RelaunchCmd/RelaunchArgs. It is a full /bin/sh -c
	// command string supplied by the pane's @ts_relaunch option (the owning
	// tool is responsible for quoting); tmux-state passes it through unaltered.
	OverrideCmd string
}

// BuildStartupCommand composes the shell-command string passed to tmux as the
// trailing argument of new-session / new-window / split-window. Returns an
// empty string when no startup work is needed (caller omits the trailing arg
// so tmux uses its default-command).
//
// Output forms (matches spec §"Plan composition" table):
//
//	scrollback=no  relaunch=no   ""
//	scrollback=no  relaunch=yes  `<cmd> <quoted-args...>`
//	scrollback=yes relaunch=no   `'<self>' cat-scrollback <sha>; exec <shell> [-l]`
//	scrollback=yes relaunch=yes  `'<self>' cat-scrollback <sha>; exec <cmd> <quoted-args...>`
//
// An OverrideCmd (pane @ts_relaunch) replaces the <cmd ...> exec target in the
// relaunch=yes forms, emitted verbatim (no arg quoting).
//
// The output is interpreted by /bin/sh -c when tmux spawns the pane. See
// StartupOpts.RelaunchArgs for the printable-ASCII assumption on args.
func BuildStartupCommand(opts StartupOpts) string {
	relaunch := buildExecTarget(opts)
	if opts.ScrollbackSHA == "" {
		// Without scrollback, an exec wrapper adds nothing; just emit the
		// raw command (tmux runs it via /bin/sh -c).
		if opts.RelaunchCmd == "" && opts.OverrideCmd == "" {
			return ""
		}
		return relaunch
	}
	return shellQuoteSingle(opts.Self) + " cat-scrollback " + opts.ScrollbackSHA + "; exec " + relaunch
}

// buildExecTarget returns the program-and-args portion that follows `exec`,
// or that stands alone when no scrollback is involved.
func buildExecTarget(opts StartupOpts) string {
	if opts.OverrideCmd != "" {
		return opts.OverrideCmd
	}
	if opts.RelaunchCmd != "" {
		var b strings.Builder
		b.WriteString(opts.RelaunchCmd)
		for _, a := range opts.RelaunchArgs {
			b.WriteByte(' ')
			b.WriteString(strconv.Quote(a))
		}
		return b.String()
	}
	if opts.IsBash {
		return opts.DefaultShell + " -l"
	}
	return opts.DefaultShell
}

// shellQuoteSingle wraps s in single quotes, escaping any embedded single
// quote via the standard `'\”` close-quote / escaped-quote / re-open-quote
// trick. Safe for arbitrary filesystem paths.
func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
