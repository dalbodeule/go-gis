// Package debug provides opt-in diagnostics for CLI and driver work.
package debug

import (
	"io"
	"log/slog"
	"os"
)

// Enabled reports whether GoGIS diagnostics were requested.
func Enabled() bool {
	return os.Getenv("GOGIS_DEBUG") == "1" || os.Getenv("GOGIS_DEBUG") == "true"
}

// NewLogger returns a structured logger. Diagnostics are discarded unless
// GOGIS_DEBUG is enabled, keeping normal CLI output clean.
func NewLogger(writer io.Writer) *slog.Logger {
	if writer == nil {
		writer = io.Discard
	}
	level := slog.LevelError
	if Enabled() {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level}))
}
