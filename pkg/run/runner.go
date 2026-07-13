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
	"bytes"
	"context"
	"os"
	"os/exec"
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
type OSExecRunner struct{}

// Run executes name with args under ctx, capturing stdout and stderr.
// A non-zero exit is reported in Result.ExitCode, not Result.Err.
func (OSExecRunner) Run(ctx context.Context, name string, args ...string) Result {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = DefaultEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}

	return Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exit,
		Err:      runErr,
	}
}

// LookPath reports whether name is found on PATH via a `which` lookup
// executed through rn. Returns true iff the lookup exits cleanly with
// code 0 (the binary exists and is executable).
//
// Centralizing this lets adapters and validators share one binary-existence
// contract rather than each cloning the `which {binary}` pattern.
func LookPath(ctx context.Context, rn Runner, name string) bool {
	res := rn.Run(ctx, "which", name)
	return res.Err == nil && res.ExitCode == 0
}
