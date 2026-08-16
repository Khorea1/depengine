package run

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckResultSuccess(t *testing.T) {
	res := Result{ExitCode: 0}
	if err := CheckResult(res, "cmd"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckResultSpawnFailure(t *testing.T) {
	spawnErr := errors.New("binary not found")
	res := Result{Err: spawnErr, ExitCode: 0}
	err := CheckResult(res, "git")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "git: failed:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "git: failed:")
	}
	if !strings.Contains(err.Error(), "binary not found") {
		t.Fatalf("error = %q, want wrapped message %q", err.Error(), "binary not found")
	}
	if !errors.Is(err, spawnErr) {
		t.Fatalf("expected err to wrap spawnErr (errors.Is), got %v", err)
	}
}

func TestCheckResultNonZeroExit(t *testing.T) {
	res := Result{ExitCode: 128, Stderr: []byte("fatal: not a git repository\n")}
	err := CheckResult(res, "git: clone")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exited 128:") {
		t.Fatalf("error = %q, want exit code 128", err.Error())
	}
	if !strings.Contains(err.Error(), "fatal: not a git repository") {
		t.Fatalf("error = %q, want stderr message", err.Error())
	}
}

func TestCheckResultTrimsStderr(t *testing.T) {
	res := Result{ExitCode: 1, Stderr: []byte("  trailing whitespace  \n\n")}
	err := CheckResult(res, "pip")

	if !strings.Contains(err.Error(), "pip: exited 1: trailing whitespace") {
		t.Fatalf("error = %q, want trimmed stderr", err.Error())
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("error = %q, should not contain newlines from stderr", err.Error())
	}
}

func TestCheckResultErrPrecedenceOverExitCode(t *testing.T) {
	// When both Err and non-zero ExitCode are set, Err wins (spawn failure is
	// more informative than a stale exit code).
	spawnErr := errors.New("killed by signal")
	res := Result{Err: spawnErr, ExitCode: 137}
	err := CheckResult(res, "native")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "native: failed:") {
		t.Fatalf("error = %q, want Err precedence", err.Error())
	}
	if strings.Contains(err.Error(), "exited 137") {
		t.Fatalf("error = %q, should not mention exit code when Err is set", err.Error())
	}
}
