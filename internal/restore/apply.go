package restore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
)

// Runner is the subset of tmux.Client used by Apply (lets tests inject a fake).
type Runner interface {
	Run(ctx context.Context, args []string) (string, error)
}

// Apply executes the plan via the Runner. Best-effort: individual failures
// are swallowed so the rest of the plan still runs. RestoreScrollback actions
// are no-ops here — see ApplyWithScrollback.
func Apply(ctx context.Context, t Runner, plan []Action) error {
	for _, a := range plan {
		var args []string
		switch v := a.(type) {
		case CreateSession:
			args = []string{"new-session", "-d", "-s", v.Name, "-c", v.Cwd}
		case CreateWindow:
			args = []string{"new-window", "-t", fmt.Sprintf("%s:%d", v.Session, v.Index), "-n", v.Name, "-c", v.Cwd}
		case SplitPane:
			args = []string{"split-window", "-t", v.Target, "-c", v.Cwd}
		case SetLayout:
			args = []string{"select-layout", "-t", v.Window, v.Layout}
		case RelaunchCommand:
			cmd := v.Command
			for _, a := range v.Args {
				cmd += " " + strconv.Quote(a)
			}
			args = []string{"send-keys", "-t", v.Pane, cmd, "Enter"}
		case RestoreScrollback:
			continue
		default:
			return fmt.Errorf("unknown action: %T", a)
		}
		if _, err := t.Run(ctx, args); err != nil {
			continue
		}
	}
	return nil
}

// ScrollbackReader returns the raw bytes for a scrollback identified by sha.
type ScrollbackReader interface {
	Get(ctx context.Context, sha string) ([]byte, error)
}

// ApplyWithScrollback runs the plan including RestoreScrollback actions.
func ApplyWithScrollback(ctx context.Context, t Runner, sb ScrollbackReader, plan []Action) error {
	for _, a := range plan {
		switch v := a.(type) {
		case RestoreScrollback:
			if err := pasteScrollback(ctx, t, sb, v); err != nil {
				continue
			}
		default:
			if err := Apply(ctx, t, []Action{v}); err != nil {
				continue
			}
		}
	}
	return nil
}

func pasteScrollback(ctx context.Context, t Runner, sb ScrollbackReader, v RestoreScrollback) error {
	content, err := sb.Get(ctx, v.SHA)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "tmux-state-paste-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpName) }()
	if err := os.WriteFile(tmpName, content, 0o600); err != nil {
		return err
	}
	bufID := "tmux-state-" + randHex()
	if _, err := t.Run(ctx, []string{"load-buffer", "-b", bufID, tmpName}); err != nil {
		return err
	}
	if _, err := t.Run(ctx, []string{"paste-buffer", "-b", bufID, "-t", v.Pane}); err != nil {
		return err
	}
	if _, err := t.Run(ctx, []string{"delete-buffer", "-b", bufID}); err != nil {
		return err
	}
	return nil
}

func randHex() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
