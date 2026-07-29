package restore

import (
	"strings"
)

// StartupOpts is the input to BuildStartupCommand. Fields not relevant to a
// given branch may be left zero.
type StartupOpts struct {
	// Self is the absolute path of the running tmux-remux binary, used to
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
	// RelaunchArgs are appended to RelaunchCmd, each single-quoted via
	// shellQuoteSingle. tmux runs the startup string through the pane's
	// default-shell (POSIX sh, bash, zsh, or fish), and single quotes are
	// literal in all of them — so single-quoting is what neutralizes shell
	// metacharacters. strconv.Quote would leave $ and backticks live inside
	// its double quotes, so an arg like $(cmd) or `cmd` would be
	// command-substituted by the shell.
	RelaunchArgs []string
	// OverrideCmd, if non-empty, is exec'd verbatim as the relaunch target,
	// taking precedence over RelaunchCmd/RelaunchArgs. It is a full shell
	// command string supplied by the pane's @remux_relaunch option (the owning
	// tool is responsible for quoting); tmux-remux passes it through unaltered.
	OverrideCmd string
}

// BuildStartupCommand composes the shell-command string passed to tmux as the
// trailing argument of new-session / new-window / split-window. Returns an
// empty string when no startup work is needed (caller omits the trailing arg
// so tmux uses its default-command).
//
// Output forms:
//
//	scrollback=no  relaunch=no   ""
//	scrollback=no  relaunch=yes  `<cmd> <quoted-args...>; exec <shell> [-l]`
//	scrollback=yes relaunch=no   `'<self>' cat-scrollback <sha>; exec <shell> [-l]`
//	scrollback=yes relaunch=yes  `'<self>' cat-scrollback <sha>; <cmd> <quoted-args...>; exec <shell> [-l]`
//
// The relaunched command runs as a child, then the pane exec's the default
// shell — so quitting the agent/program drops back to an interactive prompt
// instead of tearing down the pane (and a lone-pane window) with it, matching
// how the command was originally launched from a shell.
//
// An OverrideCmd (pane @remux_relaunch) replaces the <cmd ...> relaunch target
// in the relaunch=yes forms, emitted verbatim (no arg quoting).
//
// tmux spawns the pane by running the output through the pane's default-shell
// (the same shell dropped back into) — POSIX sh, bash, zsh, or fish; `;` and
// `exec` and literal single quotes behave the same across all of them. See
// StartupOpts.RelaunchArgs for the printable-ASCII assumption on args.
func BuildStartupCommand(opts StartupOpts) string {
	relaunch := buildRelaunchTarget(opts)
	shell := shellExec(opts)
	if opts.ScrollbackSHA == "" {
		if relaunch == "" {
			return ""
		}
		return relaunch + "; exec " + shell
	}
	prefix := shellQuoteSingle(opts.Self) + " cat-scrollback " + opts.ScrollbackSHA + "; "
	if relaunch == "" {
		return prefix + "exec " + shell
	}
	return prefix + relaunch + "; exec " + shell
}

// buildRelaunchTarget returns the program-and-args to run before dropping to
// the shell, or "" when the pane has no command to relaunch.
func buildRelaunchTarget(opts StartupOpts) string {
	if opts.OverrideCmd != "" {
		return opts.OverrideCmd
	}
	if opts.RelaunchCmd != "" {
		var b strings.Builder
		b.WriteString(opts.RelaunchCmd)
		for _, a := range opts.RelaunchArgs {
			b.WriteByte(' ')
			b.WriteString(shellQuoteSingle(a))
		}
		return b.String()
	}
	return ""
}

// shellExec returns the default-shell exec line, with -l for bash.
func shellExec(opts StartupOpts) string {
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
