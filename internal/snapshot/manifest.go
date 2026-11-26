// Package snapshot defines the manifest schema and the save/restore-relevant
// data shape for tmux state.
//
// The canonical schema types live in github.com/noamsto/tmux-remux/pkg/manifest;
// this package re-exports them as type aliases so existing internal callers
// remain source-compatible.
package snapshot

import "github.com/noamsto/tmux-remux/pkg/manifest"

// StripFormat removes tmux-format directives from `s` and trims surrounding
// whitespace.
func StripFormat(s string) string {
	return manifest.StripFormat(s)
}

// Manifest is the top-level snapshot envelope persisted in events.manifest_json.
type Manifest = manifest.Manifest

// Session captures one tmux session's structure.
type Session = manifest.Session

// PlacedBridge is one remote-bridge mirror placed into a local session.
type PlacedBridge = manifest.PlacedBridge

// Window captures one tmux window's structure within a session.
type Window = manifest.Window

// Pane captures one tmux pane's state, including optional scrollback hash.
type Pane = manifest.Pane
