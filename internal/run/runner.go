// Package run is the single seam through which the engine executes any
// subprocess: the detect_os.sh fetcher today, and later every native install,
// language-adapter install, and postinstall hook.
//
// Centralizing subprocess execution lets us inject timeouts, structured
// logging, and a per-child DEPENGINE_TRACE_ID env in one place, and lets
// tests substitute a fake Runner instead of touching the real process
// table.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Result captures everything a caller needs to decide what happened:
// stdout/stderr (for logging + error messages) and the exit code (detect_os.sh
// uses 1 to mean "partial detection", not failure — see facts.go).
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Err is non-nil only when the process failed to run at all or was
	// killed (timeout, signal, binary not found). A non-zero exit code
	// alone does NOT set Err.
	Err error
}

// Runner executes one command with captured stdout/stderr. Implementations
// must honor ctx cancellation/timeout and must never mutate global state.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) Result
}

// BlockedRunner is an execution boundary for plans that must be observational
// only. Every subprocess execution attempt is rejected before a process can be
// spawned. LookPath remains available because executable lookup itself does not
// execute the target or mutate host state.
// ExecutionPolicy is implemented by runner boundaries that can declare that
// subprocess execution is intentionally disabled. It lets callers avoid
// preparatory host mutations (for example materializing an embedded helper)
// when execution could not happen anyway.
type ExecutionPolicy interface {
	ExecutionAllowed() bool
}

// ExecutionAllowed reports whether rn permits subprocess execution. Runners
// that do not expose an explicit policy are assumed executable for backwards
// compatibility; BlockedRunner and wrappers around it fail closed.
func ExecutionAllowed(rn Runner) bool {
	if rn == nil {
		return false
	}
	policy, ok := rn.(ExecutionPolicy)
	return !ok || policy.ExecutionAllowed()
}

type BlockedRunner struct {
	Reason string
}

func (BlockedRunner) ExecutionAllowed() bool { return false }

func (r BlockedRunner) blocked() Result {
	reason := r.Reason
	if reason == "" {
		reason = "subprocess execution is disabled"
	}
	return Result{Err: errors.New(reason)}
}

func (r BlockedRunner) Run(context.Context, string, ...string) Result {
	return r.blocked()
}

func (r BlockedRunner) RunInDir(context.Context, string, string, ...string) Result {
	return r.blocked()
}

func (BlockedRunner) LookPath(_ context.Context, name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// DirectoryRunner extends Runner for commands that must execute in a specific
// working directory.
type DirectoryRunner interface {
	RunInDir(ctx context.Context, dir, name string, args ...string) Result
}

// PathLookupRunner resolves executables without depending on an external
// `which`/`where` utility.
type PathLookupRunner interface {
	LookPath(ctx context.Context, name string) bool
}

// RunInDir executes a command through rn with dir as its working directory.
func RunInDir(ctx context.Context, rn Runner, dir, name string, args ...string) Result {
	dr, ok := rn.(DirectoryRunner)
	if !ok {
		return Result{Err: errors.New("runner does not support a working directory")}
	}
	return dr.RunInDir(ctx, dir, name, args...)
}

// ElevationSession is implemented by production runners that can obtain and
// renew interactive elevation credentials. Test and remote runners may omit
// it; their command-execution environment owns any required elevation.
type ElevationSession interface {
	StartElevationSession(context.Context) (stop func(), err error)
}

// DefaultEnv copies the parent process env and appends DEPENGINE_TRACE_ID
// when present, so child processes (and our own nested calls) carry the
// trace through the whole install tree. Always returns a fresh slice —
// concurrent Runs never share an underlying env array.
func DefaultEnv() []string {
	out := append([]string(nil), os.Environ()...)
	if id := os.Getenv("DEPENGINE_TRACE_ID"); id != "" {
		out = append(out, "DEPENGINE_TRACE_ID="+id)
	}
	return out
}

// OSExecRunner is the production Runner: real os/exec, capturing buffers.
// The child environment is always the parent env plus DEPENGINE_TRACE_ID
// (when set) via DefaultEnv; injecting it here is what lets trace id
// flow into detect_os.sh and later into every adapter install.
type OSExecRunner struct {
	// Stream optionally receives a live copy of the child's stdout and
	// stderr while the command runs. Capture into Result is unaffected.
	// The zero value (nil) preserves the historical capture-only
	// behavior. A non-nil Stream must be safe for concurrent use
	// (stdout and stderr are copied concurrently) and must not block
	// indefinitely.
	Stream io.Writer
}

// killGracePeriod bounds how long Run waits for grandchildren holding the
// child's pipes after the direct child exits. Without it, a grandchild
// that inherits stdout (e.g. a daemonized helper) makes cmd.Run block
// until the grandchild exits, even after a timeout already fired.
const killGracePeriod = 5 * time.Second

// maxCapturedOutput caps how much of each of stdout/stderr is retained in
// Result. Output beyond the cap is dropped from the head (the tail is
// what diagnostics need); see cappedBuffer. Long installs already surface
// nothing live, so unbounded capture was pure memory risk.
const maxCapturedOutput = 1 << 20 // 1 MiB per stream

// LookPath reports whether name resolves through the child process PATH.
func (OSExecRunner) LookPath(_ context.Context, name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Run executes name with args under ctx, capturing stdout and stderr.
// A non-zero exit is reported in Result.ExitCode, not Result.Err.
func (r OSExecRunner) Run(ctx context.Context, name string, args ...string) Result {
	return runCommand(ctx, r.Stream, "", name, args...)
}

// RunInDir executes name with dir as the child process working directory.
func (r OSExecRunner) RunInDir(ctx context.Context, dir, name string, args ...string) Result {
	return runCommand(ctx, r.Stream, dir, name, args...)
}

// cappedBuffer is an io.Writer that retains at most max bytes: the tail.
// Writes past the cap evict from the head, so peak memory stays O(max)
// no matter how much a child emits. Embedded credentials are redacted at
// the CheckResult/logging layer, not here, so the retained tail is exact.
type cappedBuffer struct {
	max int
	buf []byte
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) >= b.max {
		b.buf = append(b.buf[:0], p[len(p)-b.max:]...)
		return len(p), nil
	}
	if overflow := len(b.buf) + len(p) - b.max; overflow > 0 {
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// Bytes returns the retained tail. The caller must not mutate it.
func (b *cappedBuffer) Bytes() []byte { return b.buf }

func runCommand(ctx context.Context, stream io.Writer, dir, name string, args ...string) Result {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = DefaultEnv()
	cmd.Dir = dir
	// Own process group so cancellation signals the whole tree, and a
	// bounded WaitDelay so a grandchild holding the pipes cannot block
	// Wait indefinitely after a timeout or SIGTERM.
	setupChild(cmd)
	cmd.Cancel = func() error { return terminateTree(cmd) }
	cmd.WaitDelay = killGracePeriod

	stdout := newCappedBuffer(maxCapturedOutput)
	stderr := newCappedBuffer(maxCapturedOutput)
	if stream != nil {
		// One copying goroutine per pipe inside os/exec; MultiWriter
		// itself needs no extra synchronization for this use.
		cmd.Stdout = io.MultiWriter(stdout, stream)
		cmd.Stderr = io.MultiWriter(stderr, stream)
	} else {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
	}

	runErr := cmd.Run()
	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}

	// cmd.Run() returns a non-nil *exec.ExitError for ANY non-zero exit —
	// that's a command that ran fine and just said "no" (e.g. `which cargo`
	// when cargo isn't installed, or `apt-get update` hitting a dead repo).
	// Reporting that through Err would violate the Result contract above
	// (and the one FakeRunner already enforces — see
	// TestResultNonZeroExitDoesNotSetErr) and makes every caller that
	// branches on Err vs ExitCode treat routine "ran, said no" the same as
	// "never ran at all". Only keep Err for errors that are NOT a plain
	// exit-status result: binary not found, permission denied, killed by
	// signal, context deadline/cancellation, etc.
	var exitErr *exec.ExitError
	if runErr != nil && errors.As(runErr, &exitErr) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// The operation's context expired: the result is untrustworthy
			// (partial output, killed tree) so cancellation wins over the
			// exit code. This also covers Windows, where a killed process
			// reports a plain non-zero exit indistinguishable from failure.
			runErr = ctxErr
		} else if exit >= 0 {
			// A normal process exit, even when non-zero, belongs exclusively
			// in ExitCode.
			runErr = nil
		}
		// Otherwise (signal death with a live context) keep the ExitError.
	}

	return Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exit,
		Err:      runErr,
	}
}

// LookPath reports whether name is found on PATH. Native runners use the Go
// standard library; custom runners may implement PathLookupRunner or receive
// the legacy `which` fallback for compatibility.
//
// Centralizing this lets adapters and validators share one binary-existence
// contract rather than each cloning the `which {binary}` pattern.
// If rn is nil, a default OSExecRunner is used.
func LookPath(ctx context.Context, rn Runner, name string) bool {
	if rn == nil {
		rn = OSExecRunner{}
	}
	if lookup, ok := rn.(PathLookupRunner); ok {
		return lookup.LookPath(ctx, name)
	}
	res := rn.Run(ctx, "which", name)
	return res.Err == nil && res.ExitCode == 0
}

// CheckResult inspects a Result and returns nil when the command succeeded,
// or a formatted error when it failed. It encodes the canonical pattern
// repeated across every adapter: res.Err is a spawn failure (binary not found,
// timeout, signal), a non-zero ExitCode is a command-level failure whose stderr
// carries the useful message. The prefix labels the error (e.g. "git", "cargo",
// "native: install") so callers see "git: clone exited 128: ...".
//
// The error verbs match existing adapter conventions:
//   - spawn failure: "<prefix>: failed: <err>"
//   - non-zero exit:  "<prefix>: exited <code>: <stderr>"
//
// Use CheckResult when both cases should be errors. When only res.Err matters
// (e.g. asdf plugin-add, best-effort version probes), handle Result inline.
func CheckResult(res Result, prefix string) error {
	if res.Err != nil {
		return &redactedWrappedError{
			message: fmt.Sprintf("%s: failed: %s", prefix, RedactSensitiveText(res.Err.Error())),
			cause:   res.Err,
		}
	}
	if res.ExitCode != 0 {
		stderr := RedactSensitiveText(strings.TrimSpace(string(res.Stderr)))
		return fmt.Errorf("%s: exited %d: %s", prefix, res.ExitCode, stderr)
	}
	return nil
}

// redactedWrappedError preserves errors.Is/errors.As semantics while ensuring
// the displayed error text cannot re-expose credentials from the wrapped
// process error.
type redactedWrappedError struct {
	message string
	cause   error
}

func (e *redactedWrappedError) Error() string { return e.message }
func (e *redactedWrappedError) Unwrap() error { return e.cause }

var _ DirectoryRunner = OSExecRunner{}
var _ PathLookupRunner = OSExecRunner{}
var _ DirectoryRunner = BlockedRunner{}
var _ PathLookupRunner = BlockedRunner{}
