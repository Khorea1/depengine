// Package run is the single seam through which the engine executes any
// subprocess: host probes, native installs, language-adapter installs, and
// postinstall hooks.
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
	"runtime"
	"strings"
	"sync"
	"time"
)

// Result captures everything a caller needs to decide what happened:
// stdout/stderr (for logging + error messages) and the exit code.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Err is non-nil only when the process failed to run at all or was
	// killed (timeout, signal, binary not found). A non-zero exit code
	// alone does NOT set Err.
	Err error
}

type omittedEnvKey struct{}

// WithOmittedEnv returns a child context that tells OSExecRunner to omit the
// named environment variables from processes started with that context. Names
// apply to Run, RunWithEnv, and RunInDir, including per-child overrides. The
// parent process environment is not changed.
func WithOmittedEnv(ctx context.Context, names ...string) context.Context {
	omitted := make(map[string]struct{}, len(names))
	if existing, ok := ctx.Value(omittedEnvKey{}).(map[string]struct{}); ok {
		for name := range existing {
			omitted[name] = struct{}{}
		}
	}
	for _, name := range names {
		if name != "" {
			omitted[omittedEnvName(name)] = struct{}{}
		}
	}
	return context.WithValue(ctx, omittedEnvKey{}, omitted)
}

func omittedEnvName(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

// Runner executes one command with captured stdout/stderr. Implementations
// must honor ctx cancellation/timeout and WithOmittedEnv when launching child
// processes, and must never mutate global state.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) Result
}

// StdoutPipe represents one running child whose stdout is consumed as a
// stream instead of being captured in memory. Callers must drain Reader or
// call Abort before Wait. Wait is idempotent and returns the same Result on
// every call; streamed stdout is intentionally absent from Result.Stdout.
type StdoutPipe struct {
	Reader io.ReadCloser

	wait     func() Result
	abort    func() error
	waitOnce sync.Once
	result   Result
}

// NewStdoutPipe constructs a streaming command handle for custom Runner
// implementations. wait should return the normalized child Result; abort may
// be nil when the producer has no separate cancellation action.
func NewStdoutPipe(reader io.ReadCloser, wait func() Result, abort func() error) *StdoutPipe {
	return &StdoutPipe{Reader: reader, wait: wait, abort: abort}
}

// Wait waits for the streaming child to exit and returns its normalized
// process result. It is safe to call more than once.
func (p *StdoutPipe) Wait() Result {
	if p == nil {
		return Result{Err: errors.New("stdout pipe is nil")}
	}
	p.waitOnce.Do(func() {
		if p.wait == nil {
			p.result = Result{Err: errors.New("stdout pipe has no wait function")}
			return
		}
		p.result = p.wait()
	})
	return p.result
}

// Abort closes the stdout reader and asks the runner to terminate the child
// process tree. It is intended for consumers that reject the stream before
// EOF and therefore must not leave the producer blocked on a full pipe.
func (p *StdoutPipe) Abort() error {
	if p == nil {
		return nil
	}
	if p.Reader != nil {
		_ = p.Reader.Close()
	}
	if p.abort == nil {
		return nil
	}
	err := p.abort()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

// StdoutPipeRunner is an optional Runner capability for commands whose stdout
// must be consumed incrementally. It keeps subprocess creation behind the same
// execution boundary as Run while avoiding unbounded capture of binary data.
type StdoutPipeRunner interface {
	OpenStdoutPipe(ctx context.Context, name string, args ...string) (*StdoutPipe, error)
}

// OpenStdoutPipe opens a streaming stdout command through rn. Runners that do
// not expose the capability fail closed rather than bypassing the Runner seam.
func OpenStdoutPipe(ctx context.Context, rn Runner, name string, args ...string) (*StdoutPipe, error) {
	if rn == nil {
		return nil, errors.New("stdout streaming requires a runner")
	}
	streamer, ok := rn.(StdoutPipeRunner)
	if !ok {
		return nil, errors.New("runner does not support stdout streaming")
	}
	return streamer.OpenStdoutPipe(ctx, name, args...)
}

// EnvironmentRunner accepts environment overrides for one child process.
// Callers must use RunWithEnv so sensitive output is redacted before it escapes.
// Implementations that launch child processes must honor WithOmittedEnv.
type EnvironmentRunner interface {
	RunWithEnv(ctx context.Context, env map[string]string, sensitive []string, name string, args ...string) Result
}

// RunWithEnv executes one child with per-call environment overrides. It fails
// closed when the runner cannot provide this boundary. For sensitive calls,
// captured output and process errors are suppressed because a child can echo
// credentials in encodings that literal redaction cannot reliably recognize.
func RunWithEnv(ctx context.Context, rn Runner, env map[string]string, sensitive []string, name string, args ...string) Result {
	if len(env) == 0 {
		return rn.Run(ctx, name, args...)
	}
	er, ok := rn.(EnvironmentRunner)
	if !ok {
		return Result{Err: errors.New("runner does not support per-call environment")}
	}
	return redactResult(er.RunWithEnv(ctx, env, sensitive, name, args...), sensitive)
}

func redactResult(result Result, sensitive []string) Result {
	if len(sensitive) > 0 {
		result.Stdout = nil
		result.Stderr = nil
		if result.Err != nil {
			result.Err = errors.New("sensitive subprocess execution failed")
		}
	}
	return result
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

func (r BlockedRunner) OpenStdoutPipe(context.Context, string, ...string) (*StdoutPipe, error) {
	return nil, r.blocked().Err
}

func (r BlockedRunner) RunWithEnv(context.Context, map[string]string, []string, string, ...string) Result {
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
// (when set) via DefaultEnv; injecting it here lets the trace id flow into
// host probes and every adapter install.
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
	return runCommand(ctx, r.Stream, "", nil, name, args...)
}

// OpenStdoutPipe starts name with stdout exposed as a pipe and stderr retained
// in the same bounded diagnostic buffer used by Run. Binary stdout is never
// copied to the optional live Stream because callers own and interpret it.
func (r OSExecRunner) OpenStdoutPipe(ctx context.Context, name string, args ...string) (*StdoutPipe, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- OSExecRunner is the intentional subprocess boundary; validated callers select argv at runtime.
	cmd.Env = omitEnv(DefaultEnv(), ctx)
	setupChild(cmd)
	cmd.Cancel = func() error { return terminateTree(cmd) }
	cmd.WaitDelay = killGracePeriod

	stderr := newCappedBuffer(maxCapturedOutput)
	if r.Stream != nil {
		cmd.Stderr = io.MultiWriter(stderr, r.Stream)
	} else {
		cmd.Stderr = stderr
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe for %s: %w", name, RedactError(err))
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("start %s: %w", name, RedactError(err))
	}

	return &StdoutPipe{
		Reader: stdout,
		abort:  cmd.Cancel,
		wait: func() Result {
			return commandResult(ctx, cmd, cmd.Wait(), nil, stderr.Bytes())
		},
	}, nil
}

// RunInDir executes name with dir as the child process working directory.
func (r OSExecRunner) RunInDir(ctx context.Context, dir, name string, args ...string) Result {
	return runCommand(ctx, r.Stream, dir, nil, name, args...)
}

// RunWithEnv suppresses live streaming because child output may contain a
// credential. The RunWithEnv helper redacts captured output on return.
func (OSExecRunner) RunWithEnv(ctx context.Context, env map[string]string, _ []string, name string, args ...string) Result {
	return runCommand(ctx, nil, "", env, name, args...)
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

func runCommand(ctx context.Context, stream io.Writer, dir string, overrides map[string]string, name string, args ...string) Result {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- OSExecRunner is the intentional subprocess boundary; validated adapters and explicitly authorized hooks select argv at runtime.
	cmd.Env = omitEnv(mergeEnv(DefaultEnv(), overrides), ctx)
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

	return commandResult(ctx, cmd, cmd.Run(), stdout.Bytes(), stderr.Bytes())
}

func commandResult(ctx context.Context, cmd *exec.Cmd, runErr error, stdout, stderr []byte) Result {
	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}

	// os/exec returns a non-nil *exec.ExitError for any ordinary non-zero
	// exit. Preserve the Runner contract by reporting that only via ExitCode;
	// Err is reserved for spawn failure, cancellation, timeout, or signals.
	var exitErr *exec.ExitError
	if runErr != nil && errors.As(runErr, &exitErr) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			runErr = ctxErr
		} else if exit >= 0 {
			runErr = nil
		}
	}

	return Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exit,
		Err:      runErr,
	}
}

func omitEnv(env []string, ctx context.Context) []string {
	omitted, ok := ctx.Value(omittedEnvKey{}).(map[string]struct{})
	if !ok || len(omitted) == 0 {
		return env
	}
	filtered := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if _, skip := omitted[omittedEnvName(name)]; !skip {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	for key, value := range overrides {
		prefix := key + "="
		found := false
		for i, entry := range base {
			if strings.HasPrefix(entry, prefix) {
				base[i] = prefix + value
				found = true
			}
		}
		if !found {
			base = append(base, prefix+value)
		}
	}
	return base
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
var _ StdoutPipeRunner = OSExecRunner{}
var _ DirectoryRunner = BlockedRunner{}
var _ PathLookupRunner = BlockedRunner{}
var _ StdoutPipeRunner = BlockedRunner{}
