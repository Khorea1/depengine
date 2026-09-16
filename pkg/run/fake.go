package run

import (
	"context"
	"sync"
	"time"
)

// FakeRunner records every call and returns canned output for assertions.
// It is used across multiple packages' tests.
type FakeRunner struct {
	mu       sync.Mutex
	Calls    []FakeCall
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	// Delay, if set, blocks Run this long (used to test ctx cancellation).
	Delay time.Duration
}

// FakeCall records a single invocation of Run.
type FakeCall struct {
	Name string
	Args []string
	Dir  string
}

func (f *FakeRunner) Run(ctx context.Context, name string, args ...string) Result {
	return f.run(ctx, "", name, args...)
}

// RunInDir records dir along with the command and returns the configured result.
func (f *FakeRunner) RunInDir(ctx context.Context, dir, name string, args ...string) Result {
	return f.run(ctx, dir, name, args...)
}

// LookPath records a logical lookup and returns the configured result.
func (f *FakeRunner) LookPath(ctx context.Context, name string) bool {
	result := f.run(ctx, "", "which", name)
	return result.Err == nil && result.ExitCode == 0
}

func (f *FakeRunner) run(ctx context.Context, dir, name string, args ...string) Result {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{Name: name, Args: append([]string(nil), args...), Dir: dir})
	f.mu.Unlock()

	if f.Delay > 0 {
		select {
		case <-time.After(f.Delay):
		case <-ctx.Done():
			return Result{Err: ctx.Err()}
		}
	}

	return Result{
		Stdout:   []byte(f.Stdout),
		Stderr:   []byte(f.Stderr),
		ExitCode: f.ExitCode,
		Err:      f.Err,
	}
}

var _ DirectoryRunner = (*FakeRunner)(nil)
var _ PathLookupRunner = (*FakeRunner)(nil)
