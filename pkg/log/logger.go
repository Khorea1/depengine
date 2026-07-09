// Package log provides structured logging for depengine using log/slog.
//
// Usage:
//
//	import "depengine/pkg/log"
//
//	// Use the default logger (stderr, INFO level).
//	log.Default.Info("starting up", "phase", "init")
//
//	// Create a logger with semantic context.
//	l := log.New(os.Stderr, slog.LevelInfo)
//	l = log.WithContext(l, log.LogContext{
//	    TraceID: "abc-123",
//	    Tool:    "zsh",
//	    Method:  "native",
//	    Phase:   "install",
//	})
//	l.Info("installing")
//	l.Debug("running command", "cmd", "apt-get install -y zsh")
//
//	// In tests:
//	cap := log.NewTestLogger(t)
//	// ... code that logs ...
//	cap.AssertContains("installed")
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Level aliases map 1:1 to slog levels for ergonomic access.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// LogContext carries semantic context for every log line. Fields are
// attached as slog attributes so structured output (JSON) remains
// parseable.
type LogContext struct {
	TraceID string // propagated via DEPENGINE_TRACE_ID
	Tool    string // tool being processed (e.g. "zsh")
	Method  string // method being attempted (e.g. "native")
	Distro  string // distro name (e.g. "arch")
	Family  string // resolved clan (e.g. "arch")
	Phase   string // lifecycle phase: "parse" | "resolve" | "install" | "postinstall"
}

// New creates a new slog.Logger writing to out at the given level.
// The default handler is text (human-readable); set env DEPENGINE_LOG_JSON=1
// to switch to JSON for programmatic consumption.
func New(out io.Writer, level slog.Leveler) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if os.Getenv("DEPENGINE_LOG_JSON") == "1" {
		handler = slog.NewJSONHandler(out, opts)
	} else {
		handler = slog.NewTextHandler(out, opts)
	}
	return slog.New(handler)
}

// WithContext returns a logger with LogContext fields pre-attached as
// slog attributes. All subsequent log calls carry these fields.
func WithContext(l *slog.Logger, lc LogContext) *slog.Logger {
	var args []any
	if lc.TraceID != "" {
		args = append(args, "trace_id", lc.TraceID)
	}
	if lc.Tool != "" {
		args = append(args, "tool", lc.Tool)
	}
	if lc.Method != "" {
		args = append(args, "method", lc.Method)
	}
	if lc.Distro != "" {
		args = append(args, "distro", lc.Distro)
	}
	if lc.Family != "" {
		args = append(args, "family", lc.Family)
	}
	if lc.Phase != "" {
		args = append(args, "phase", lc.Phase)
	}
	return l.With(args...)
}

// Default is the package-level logger writing to stderr at WARN level.
// It picks up DEPENGINE_TRACE_ID from the environment automatically.
var Default = New(os.Stderr, slog.LevelWarn)

func init() {
	if id := os.Getenv("DEPENGINE_TRACE_ID"); id != "" {
		Default = Default.With("trace_id", id)
	}
}

// LevelFromString converts a case-insensitive level name to slog.Level.
// Returns LevelInfo for unrecognized strings.
func LevelFromString(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
