package run

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFakeRunnerReplaysCall(t *testing.T) {
	fr := &FakeRunner{Stdout: "{}", ExitCode: 1}
	res := fr.Run(context.Background(), "detect_os.sh", "--json", "--no-prompt")

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(fr.Calls))
	}
	if fr.Calls[0].Name != "detect_os.sh" {
		t.Fatalf("recorded name = %q, want detect_os.sh", fr.Calls[0].Name)
	}
	if len(fr.Calls[0].Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(fr.Calls[0].Args))
	}

	if string(res.Stdout) != "{}" {
		t.Fatalf("stdout = %q, want {}", res.Stdout)
	}
	if res.ExitCode != 1 {
		t.Fatalf("exit = %d, want 1", res.ExitCode)
	}
	if res.Err != nil {
		t.Fatalf("spurious err: %v", res.Err)
	}
}

// Critical invariant: a non-zero exit code MUST NOT set Err. detect_os.sh
// uses exit 1 for partial detection (valid JSON still emitted); callers
// distinguish "didn't run" from "ran, exited non-zero" by checking Err.
func TestResultNonZeroExitDoesNotSetErr(t *testing.T) {
	fr := &FakeRunner{ExitCode: 1, Err: nil}
	res := fr.Run(context.Background(), "anything")

	if res.ExitCode != 1 {
		t.Fatalf("exit = %d, want 1", res.ExitCode)
	}
	if res.Err != nil {
		t.Fatalf("non-zero exit should not set Err, got %v", res.Err)
	}
}

func TestFakeRunnerHonorsCtxCancellation(t *testing.T) {
	fr := &FakeRunner{Delay: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	res := fr.Run(ctx, "slow-cmd")
	if res.Err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want DeadlineExceeded", res.Err)
	}
}

func TestOSExecRunnerRunInDir(t *testing.T) {
	dir := t.TempDir()
	name, args := "pwd", []string(nil)
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/c", "cd"}
	}
	result := OSExecRunner{}.RunInDir(context.Background(), dir, name, args...)
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("RunInDir failed: %+v", result)
	}
	got, err := filepath.Abs(strings.TrimSpace(string(result.Stdout)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("working directory = %q, want %q", got, want)
	}
}

func TestRunInDirRejectsUnsupportedRunner(t *testing.T) {
	type runOnly struct{ Runner }
	result := RunInDir(context.Background(), runOnly{Runner: &FakeRunner{}}, t.TempDir(), "true")
	if result.Err == nil {
		t.Fatal("expected unsupported working-directory error")
	}
}

func TestOSExecRunnerLookPath(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := OSExecRunner{}
	if !runner.LookPath(context.Background(), executable) {
		t.Fatalf("current executable %q should resolve", executable)
	}
	if runner.LookPath(context.Background(), "depengine-test-definitely-missing") {
		t.Fatal("missing executable unexpectedly resolved")
	}
}

// DefaultEnv must carry DEPENGINE_TRACE_ID through to children when set.
// Strict env-isolation: parent env is copied, not aliased.
func TestDefaultEnvPropagatesTraceID(t *testing.T) {
	t.Setenv("DEPENGINE_TRACE_ID", "abc-123")
	env := DefaultEnv()

	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "DEPENGINE_TRACE_ID=") {
			if kv == "DEPENGINE_TRACE_ID=abc-123" {
				found = true
			}
			break
		}
	}
	if !found {
		t.Fatalf("DefaultEnv did not propagate DEPENGINE_TRACE_ID; env = %v", env)
	}
}

func TestBlockedRunnerRejectsExecution(t *testing.T) {
	runner := BlockedRunner{Reason: "dry-run: blocked"}
	for _, result := range []Result{
		runner.Run(context.Background(), "sh", "-c", "exit 0"),
		runner.RunInDir(context.Background(), t.TempDir(), "sh", "-c", "exit 0"),
	} {
		if result.Err == nil || !strings.Contains(result.Err.Error(), "dry-run: blocked") {
			t.Fatalf("expected blocked execution error, got %+v", result)
		}
	}
}

func TestFormatArgsForLogRedactsCredentials(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "token separate", args: []string{"--token", "supersecret", "repo"}, want: "--token *** repo"},
		{name: "password equals", args: []string{"--password=hunter2"}, want: "--password=***"},
		{name: "authorization header", args: []string{"-H", "Authorization: Bearer abc123"}, want: "-H Authorization: ***"},
		{name: "credential URL", args: []string{"https://user:pass@example.com/file"}, want: "https://***@example.com/file"},
		{name: "ordinary args", args: []string{"install", "pkg"}, want: "install pkg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatArgsForLog(tc.args); got != tc.want {
				t.Fatalf("formatArgsForLog(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestRedactSensitiveText(t *testing.T) {
	input := "fetch https://alice:s3cr3t@example.com/x?token=querysecret&keep=yes&sig=signed --token abc123\nAuthorization: Bearer topsecret\nCookie: session=xyz"
	got := RedactSensitiveText(input)
	for _, secret := range []string{"s3cr3t", "querysecret", "signed", "abc123", "topsecret", "session=xyz"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text still contains %q: %q", secret, got)
		}
	}
	for _, marker := range []string{"https://***@example.com/x?token=***&keep=yes&sig=***", "--token=***", "Authorization: ***", "Cookie: ***"} {
		if !strings.Contains(got, marker) {
			t.Fatalf("redacted text missing %q: %q", marker, got)
		}
	}
}

func TestOSExecRunnerTimeoutSetsErr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	name := "sleep"
	args := []string{"5"}
	if runtime.GOOS == "windows" {
		name = "cmd.exe"
		args = []string{"/d", "/c", "ping -n 6 127.0.0.1 >NUL"}
	}
	res := OSExecRunner{}.Run(ctx, name, args...)
	if res.Err == nil {
		t.Fatalf("timed-out process returned nil Err: %+v", res)
	}
	if !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("timed-out process Err = %v, want DeadlineExceeded", res.Err)
	}
}

func TestOSExecRunnerNormalNonZeroExitStillUsesExitCode(t *testing.T) {
	name := "sh"
	args := []string{"-c", "exit 7"}
	if runtime.GOOS == "windows" {
		name = "cmd.exe"
		args = []string{"/d", "/c", "exit /b 7"}
	}
	res := OSExecRunner{}.Run(context.Background(), name, args...)
	if res.Err != nil {
		t.Fatalf("ordinary non-zero exit set Err: %v", res.Err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", res.ExitCode)
	}
}

func TestOSExecRunnerSignalTerminationSetsErr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal semantics")
	}
	res := OSExecRunner{}.Run(context.Background(), "sh", "-c", "kill -TERM $$")
	if res.Err == nil {
		t.Fatalf("signal-terminated process returned nil Err: %+v", res)
	}
	if res.ExitCode != -1 {
		t.Fatalf("signal-terminated ExitCode = %d, want -1", res.ExitCode)
	}
}

func TestRedactErrorPreservesCauseAndHidesSecret(t *testing.T) {
	cause := errors.New("GET https://example.com/file?token=topsecret: failed")
	err := RedactError(cause)
	if !errors.Is(err, cause) {
		t.Fatal("RedactError did not preserve cause")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("RedactError leaked secret: %q", err)
	}
	if !strings.Contains(err.Error(), "token=***") {
		t.Fatalf("RedactError missing marker: %q", err)
	}
}
