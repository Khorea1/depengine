package run

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/log"
)

func TestLoggingRunnerPassesStdoutPipeThrough(t *testing.T) {
	inner := &FakeRunner{Stdout: "payload"}
	cap := log.NewTestLogger(t)
	runner := NewLoggingRunner(inner, cap.Logger)

	pipe, err := OpenStdoutPipe(context.Background(), runner, "decoder", "-dc")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(pipe.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if result := pipe.Wait(); result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("stream result = %+v", result)
	}
	if string(got) != "payload" {
		t.Fatalf("streamed stdout = %q, want payload", got)
	}
	cap.AssertContains(t, "run stream")
	cap.AssertContains(t, "run stream ok")
}

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

func TestLoggingRunnerPassesWorkingDirectoryThroughOnce(t *testing.T) {
	inner := &FakeRunner{}
	runner := NewLoggingRunner(inner, log.NewTestLogger(t).Logger)

	runner.RunInDir(context.Background(), "/repo", "make")

	if len(inner.Calls) != 1 || inner.Calls[0].Dir != "/repo" {
		t.Fatalf("calls = %+v, want one call in /repo", inner.Calls)
	}
}

func TestLoggingRunnerPassesPathLookupThroughOnce(t *testing.T) {
	inner := &FakeRunner{}
	runner := NewLoggingRunner(inner, log.NewTestLogger(t).Logger)

	if !runner.LookPath(context.Background(), "tool") {
		t.Fatal("lookup should return the inner result")
	}
	if len(inner.Calls) != 1 || inner.Calls[0].Args[0] != "tool" {
		t.Fatalf("calls = %+v, want one lookup for tool", inner.Calls)
	}
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

func TestLoggingRunnerRedactsSensitiveSpawnError(t *testing.T) {
	inner := &FakeRunner{Err: errors.New("spawn failed for https://alice:secret@example.com/api --token argvsecret")}
	cap := log.NewTestLogger(t)
	runner := NewLoggingRunner(inner, cap.Logger)

	runner.Run(context.Background(), "fetch")

	for _, secret := range []string{"secret", "argvsecret"} {
		cap.AssertNotContains(t, secret)
	}
	cap.AssertContains(t, "https://***@example.com/api")
	cap.AssertContains(t, "--token=***")
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

func TestLoggingRunnerRedactsSensitiveStderr(t *testing.T) {
	inner := &FakeRunner{ExitCode: 1, Stderr: "request failed for https://alice:secret@example.com/api\nAuthorization: Bearer hidden"}
	cap := log.NewTestLogger(t)
	runner := NewLoggingRunner(inner, cap.Logger)

	runner.Run(context.Background(), "fetch", "--token", "argvsecret")

	for _, secret := range []string{"secret", "hidden", "argvsecret"} {
		cap.AssertNotContains(t, secret)
	}
	cap.AssertContains(t, "Authorization: ***")
}

func TestLoggingRunnerRedactsSensitiveURLQuery(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	inner := &FakeRunner{}
	lr := NewLoggingRunner(inner, logger)

	lr.Run(context.Background(), "curl", "https://example.com/file?token=topsecret&keep=yes&sig=signed")
	out := buf.String()
	for _, secret := range []string{"topsecret", "signed"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "token=***") || !strings.Contains(out, "sig=***") {
		t.Fatalf("log missing redaction markers: %s", out)
	}
}

func TestLoggingRunnerPreservesBlockedExecutionPolicy(t *testing.T) {
	lr := NewLoggingRunner(BlockedRunner{Reason: "dry-run"}, log.NewTestLogger(t).Logger)
	if ExecutionAllowed(lr) {
		t.Fatal("LoggingRunner hid blocked execution policy")
	}
}
