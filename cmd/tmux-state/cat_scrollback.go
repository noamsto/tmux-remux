package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/noamsto/tmux-state/internal/scrollback"
)

var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// newCatScrollbackCmd is an INTERNAL helper used by restore plans. It is
// hidden from --help and the API is not stable. The pane-creation command
// emitted by restore.BuildPlan invokes:
//
//	<tmux-state-binary> cat-scrollback <sha>
//
// to render saved scrollback as static terminal output before exec'ing the
// pane's interactive program. See spec 2026-05-10-fast-restore-design.md.
func newCatScrollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "cat-scrollback <sha>",
		Short:  "Stream stored scrollback to stdout (internal helper)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			if err := cfg.EnsureDirs(); err != nil {
				return err
			}
			store := scrollback.New(cfg.ScrollbackDir)
			return runCatScrollback(cmd.Context(), store, args[0], os.Stdout)
		},
	}
}

// runCatScrollback streams the scrollback identified by sha to w.
//
// Behavior contract (must match spec):
//   - Valid sha + file present  → stream content, return nil.
//   - Valid sha + file missing  → write nothing, return nil (silent degrade).
//   - Mid-stream I/O error      → return nil after partial write
//     (restore must never fail because of stale scrollback).
//   - Malformed sha             → return error (BuildPlan bug, not user-facing).
func runCatScrollback(ctx context.Context, store *scrollback.Store, sha string, w io.Writer) error {
	if !shaPattern.MatchString(sha) {
		return fmt.Errorf("invalid sha: %q", sha)
	}
	rc, err := store.Stream(ctx, sha)
	if err != nil {
		// degrade silently — restore must never fail because of stale or
		// corrupt scrollback references; the pane gets a fresh shell instead.
		return nil
	}
	defer func() { _ = rc.Close() }()
	_, _ = io.Copy(w, rc) // mid-stream errors swallowed by design
	return nil
}
