// Package main is the tmux-state CLI entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/noamsto/tmux-state/internal/config"
)

// Version is the released version. Bumped on tagged releases.
const Version = "0.1.0"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tmux-state: error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tmux-state",
		Short:         "Fast, smart tmux state persistence",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newVersionCmd(),
		newSaveCmd(),
		newRestoreCmd(),
		newUndoCmd(),
		newPickCmd(),
		newCaptureEventCmd(),
		newIndexUpdateCmd(),
		newListCmd(),
		newPruneCmd(),
		newGCCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(*cobra.Command, []string) {
			fmt.Println(Version)
		},
	}
}

// newSaveCmd returns the save subcommand. Wired in a later task.
func newSaveCmd() *cobra.Command {
	return &cobra.Command{Use: "save", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newRestoreCmd returns the restore subcommand. Wired in a later task.
func newRestoreCmd() *cobra.Command {
	return &cobra.Command{Use: "restore", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newUndoCmd returns the undo subcommand. Wired in a later task.
func newUndoCmd() *cobra.Command {
	return &cobra.Command{Use: "undo", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newPickCmd returns the pick subcommand. Wired in a later task.
func newPickCmd() *cobra.Command {
	return &cobra.Command{Use: "pick", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newCaptureEventCmd returns the capture-event subcommand. Wired in a later task.
func newCaptureEventCmd() *cobra.Command {
	return &cobra.Command{Use: "capture-event", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newIndexUpdateCmd returns the index-update subcommand. Wired in a later task.
func newIndexUpdateCmd() *cobra.Command {
	return &cobra.Command{Use: "index-update", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newListCmd returns the list subcommand. Wired in a later task.
func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newPruneCmd returns the prune subcommand. Wired in a later task.
func newPruneCmd() *cobra.Command {
	return &cobra.Command{Use: "prune", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newGCCmd returns the gc subcommand. Wired in a later task.
func newGCCmd() *cobra.Command {
	return &cobra.Command{Use: "gc", RunE: func(*cobra.Command, []string) error { return nil }}
}

//nolint:unused // wired by save/restore/etc. subcommands in subsequent tasks
func signalCtx() (context.Context, func()) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

//nolint:unused // wired by save/restore/etc. subcommands in subsequent tasks
func loadConfig() config.Config { return config.Default() }
