package run

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Context holds optional structured context for logging, correlating
// subprocess executions with the tool and method that triggered them.
type Context struct {
	Tool   string
	Method string

	// Probe marks this run as an availability/version/already-installed
	// check (e.g. "which cargo", "dpkg -s foo") rather than an actual
	// install attempt. A non-zero exit from a probe is the EXPECTED way
	// the executor learns "not available" / "not installed yet" — it is
	// not a failure the user needs to see scroll by. Probe results are
	// logged at DEBUG instead of WARN; real install/build/postinstall
	// commands (Probe: false, the default) keep full WARN visibility.
	Probe bool
}

// command execution. Use it to get a full audit trail of all subprocesses.
//
// Usage:
//
//	inner := run.OSExecRunner{}
//	runner := run.NewLoggingRunner(inner, log.Default)
//	runner.Run(ctx, "apt-get", "install", "-y", "zsh")
//	// → DEBUG: run cmd=apt-get args="install -y zsh"
//	// → INFO:  run done cmd=apt-get exit=0 duration=3.2s
//
// Errors (non-zero exit or runtime failure) are logged at WARN level with
// stderr included for diagnostics.
type LoggingRunner struct {
	inner  Runner
	logger *slog.Logger
	ctx    Context
}

// NewLoggingRunner creates a runner that logs every command execution.
// If logger is nil, it uses slog.Default().
func NewLoggingRunner(inner Runner, logger *slog.Logger) *LoggingRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggingRunner{inner: inner, logger: logger}
}

// WithContext returns a new LoggingRunner that includes the given context
// in all log output. The original runner is unchanged.
func (lr *LoggingRunner) WithContext(ctx Context) *LoggingRunner {
	return &LoggingRunner{
		inner:  lr.inner,
		logger: lr.logger,
		ctx:    ctx,
	}
}

// Run executes the command via the inner runner, logging the call and
// result. The result is passed through unchanged.
func (lr *LoggingRunner) Run(ctx context.Context, name string, args ...string) Result {
	baseAttrs := []any{
		"cmd", name,
		"args", strings.Join(args, " "),
	}
	if lr.ctx.Tool != "" {
		baseAttrs = append(baseAttrs, "tool", lr.ctx.Tool)
	}
	if lr.ctx.Method != "" {
		baseAttrs = append(baseAttrs, "method", lr.ctx.Method)
	}

	lr.logger.Debug("run", baseAttrs...)

	start := time.Now()
	result := lr.inner.Run(ctx, name, args...)
	elapsed := time.Since(start)

	// Build structured log with duration, exit code.
	attrs := append([]any{
		"cmd", name,
		"args", strings.Join(args, " "),
		"exit", result.ExitCode,
		"duration", elapsed.String(),
	}, baseAttrs[4:]...) // skip cmd and args from baseAttrs (already included)

	// Probes (availability/already-installed checks) failing is routine
	// control flow, not something worth a WARN — demote to DEBUG so it
	// only surfaces with --log-level debug / --diagnose.
	failLevel := slog.LevelWarn
	if lr.ctx.Probe {
		failLevel = slog.LevelDebug
	}

	if result.Err != nil {
		// Process failed to start or was killed (timeout, signal).
		lr.logger.Log(ctx, failLevel, "run failed", append(attrs,
			"error", result.Err.Error(),
			"stderr", truncateStderr(result.Stderr),
		)...)
	} else if result.ExitCode != 0 {
		// Process ran but exited non-zero.
		lr.logger.Log(ctx, failLevel, "run exited non-zero", append(attrs,
			"stderr", truncateStderr(result.Stderr),
		)...)
	} else {
		lr.logger.Debug("run ok", attrs...)
	}

	return result
}

// truncateStderr limits stderr to 1KB to avoid bloating log output with
// massive compiler errors or apt-get wall text. Full stderr is still
// available via Result.Stderr for programmatic inspection.
func truncateStderr(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > 1024 {
		return s[:1024] + "... (truncated)"
	}
	return s
}
