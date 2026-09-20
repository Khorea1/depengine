package run

import (
	"context"
	"log/slog"
	"net/url"
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
	return lr.run(ctx, "", name, args...)
}

// RunInDir executes and logs a command with an explicit working directory.
func (lr *LoggingRunner) RunInDir(ctx context.Context, dir, name string, args ...string) Result {
	return lr.run(ctx, dir, name, args...)
}

// LookPath resolves an executable through the wrapped runner.
func (lr *LoggingRunner) LookPath(ctx context.Context, name string) bool {
	found := LookPath(ctx, lr.inner, name)
	lr.logger.Debug("look path", "name", name, "found", found)
	return found
}

func (lr *LoggingRunner) run(ctx context.Context, dir, name string, args ...string) Result {
	loggedArgs := formatArgsForLog(args)
	baseAttrs := []any{
		"cmd", name,
		"args", loggedArgs,
	}
	if dir != "" {
		baseAttrs = append(baseAttrs, "dir", dir)
	}
	if lr.ctx.Tool != "" {
		baseAttrs = append(baseAttrs, "tool", lr.ctx.Tool)
	}
	if lr.ctx.Method != "" {
		baseAttrs = append(baseAttrs, "method", lr.ctx.Method)
	}

	lr.logger.Debug("run", baseAttrs...)

	start := time.Now()
	var result Result
	if dir == "" {
		result = lr.inner.Run(ctx, name, args...)
	} else {
		result = RunInDir(ctx, lr.inner, dir, name, args...)
	}
	elapsed := time.Since(start)

	// Build structured log with duration, exit code.
	attrs := append([]any{
		"cmd", name,
		"args", loggedArgs,
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

	switch {
	case result.Err != nil:
		// Process failed to start or was killed (timeout, signal).
		lr.logger.Log(ctx, failLevel, "run failed", append(attrs,
			"error", RedactSensitiveText(result.Err.Error()),
			"stderr", truncateStderr(result.Stderr),
		)...)
	case result.ExitCode != 0:
		// Process ran but exited non-zero.
		lr.logger.Log(ctx, failLevel, "run exited non-zero", append(attrs,
			"stderr", truncateStderr(result.Stderr),
		)...)
	default:
		lr.logger.Debug("run ok", attrs...)
	}

	return result
}

// StartElevationSession preserves the optional elevation capability of the
// wrapped runner. A wrapper around a fake or remote runner intentionally has
// no host elevation session to start.
func (lr *LoggingRunner) StartElevationSession(ctx context.Context) (func(), error) {
	session, ok := lr.inner.(ElevationSession)
	if !ok {
		return func() {}, nil
	}
	return session.StartElevationSession(ctx)
}

// truncateStderr limits stderr to 1KB to avoid bloating log output with
// massive compiler errors or apt-get wall text. Full stderr is still
// available via Result.Stderr for programmatic inspection.
func truncateStderr(data []byte) string {
	s := RedactSensitiveText(strings.TrimSpace(string(data)))
	if len(s) > 1024 {
		return s[:1024] + "... (truncated)"
	}
	return s
}

var _ DirectoryRunner = (*LoggingRunner)(nil)
var _ PathLookupRunner = (*LoggingRunner)(nil)

// formatArgsForLog returns a display-only argv with common credential forms
// redacted. The original args are still passed unchanged to the subprocess.
// This is defense in depth: adapters should avoid putting secrets in argv at
// all, but logging must not turn one adapter mistake into a persistent leak.
func formatArgsForLog(args []string) string {
	redacted := append([]string(nil), args...)
	secretNext := false
	headerNext := false
	for i, arg := range redacted {
		lower := strings.ToLower(arg)
		if secretNext {
			redacted[i] = "***"
			secretNext = false
			continue
		}
		if headerNext {
			if isSensitiveHeader(arg) {
				redacted[i] = redactHeader(arg)
			}
			headerNext = false
			continue
		}
		if isSecretFlag(lower) {
			secretNext = true
			continue
		}
		if lower == "-h" || lower == "--header" {
			headerNext = true
			continue
		}
		if key, _, ok := strings.Cut(arg, "="); ok && isSecretFlag(strings.ToLower(key)) {
			redacted[i] = key + "=***"
			continue
		}
		if isSensitiveHeader(arg) {
			redacted[i] = redactHeader(arg)
			continue
		}
		redacted[i] = RedactSensitiveText(redactURLUserinfo(arg))
	}
	return strings.Join(redacted, " ")
}

func isSecretFlag(arg string) bool {
	switch arg {
	case "--token", "--password", "--passwd", "--secret", "--auth-token", "--access-token", "--api-key", "--apikey":
		return true
	default:
		return false
	}
}

func isSensitiveHeader(arg string) bool {
	name, _, ok := strings.Cut(arg, ":")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func redactHeader(arg string) string {
	name, _, ok := strings.Cut(arg, ":")
	if !ok {
		return "***"
	}
	return name + ": ***"
}

func redactURLUserinfo(arg string) string {
	u, err := url.Parse(arg)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User == nil {
		return arg
	}
	u.User = url.User("***")
	return u.String()
}
