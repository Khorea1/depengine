package run

import (
	"context"
	"testing"

	"depengine/pkg/log"
)

func TestLoggingRunnerPassesResultThrough(t *testing.T) {
	inner := &FakeRunner{Stdout: "ok", ExitCode: 0}
	cap := log.NewTestLogger(t)
	runner := NewLoggingRunner(inner, cap.Logger)

	res := runner.Run(context.Background(), "true")

	if string(res.Stdout) != "ok" {
		t.Fatalf("stdout = %q, want %q", res.Stdout, "ok")
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
	cap.AssertContains(t, "true")
	cap.AssertContains(t, "run ok")
}

func TestLoggingRunnerLogsNonZeroExit(t *testing.T) {
	inner := &FakeRunner{ExitCode: 1, Stderr: "permission denied"}
	cap := log.NewTestLogger(t)
	runner := NewLoggingRunner(inner, cap.Logger)

	runner.Run(context.Background(), "false")

	cap.AssertContains(t, "run exited non-zero")
	cap.AssertContains(t, "false")
	cap.AssertContains(t, "permission denied")
}

func TestLoggingRunnerLogsError(t *testing.T) {
	inner := &FakeRunner{Err: context.DeadlineExceeded}
	cap := log.NewTestLogger(t)
	runner := NewLoggingRunner(inner, cap.Logger)

	runner.Run(context.Background(), "slow-cmd")

	cap.AssertContains(t, "run failed")
	cap.AssertContains(t, "deadline exceeded")
}

func TestLoggingRunnerNilLoggerDefaults(t *testing.T) {
	inner := &FakeRunner{ExitCode: 0}
	runner := NewLoggingRunner(inner, nil)
	if runner.logger == nil {
		t.Fatal("expected default logger, got nil")
	}
	// We can't assert on output (goes to stderr), just verify no panic.
	res := runner.Run(context.Background(), "true")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
}
