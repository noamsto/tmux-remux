// Package log provides a thin wrapper around log/slog with consistent setup.
package log

import (
	"io"
	"log/slog"
)

// Level is an alias for slog.Level, re-exported so callers do not need to
// import log/slog directly when configuring loggers.
type Level = slog.Level

// Log levels mirroring slog's standard levels.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Format selects the output encoding for a logger created by New.
type Format int

// Supported output formats.
const (
	FormatText Format = iota
	FormatJSON
)

// New returns a *slog.Logger that writes to w at the given level using the
// specified format. Unknown formats fall back to text.
func New(w io.Writer, level Level, format Format) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format {
	case FormatJSON:
		handler = slog.NewJSONHandler(w, opts)
	default:
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}
