package run

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFakeRunnerStdoutPipe(t *testing.T) {
	fr := &FakeRunner{Stdout: "streamed", Stderr: "note", ExitCode: 7}
	pipe, err := OpenStdoutPipe(context.Background(), fr, "decoder", "-dc", "archive")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(pipe.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "streamed" {
		t.Fatalf("streamed stdout = %q, want streamed", got)
	}
	result := pipe.Wait()
	if result.ExitCode != 7 || string(result.Stderr) != "note" || len(result.Stdout) != 0 {
		t.Fatalf("stream result = %+v", result)
	}
	if len(fr.Calls) != 1 || fr.Calls[0].Name != "decoder" {
		t.Fatalf("calls = %+v", fr.Calls)
	}
}

func TestOSExecRunnerStdoutPipe(t *testing.T) {
	name := "sh"
	args := []string{"-c", "printf streamed"}
	if runtime.GOOS == "windows" {
		name = "cmd.exe"
		args = []string{"/d", "/c", "<nul set /p =streamed"}
	}

	pipe, err := OpenStdoutPipe(context.Background(), OSExecRunner{}, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(pipe.Reader)
	result := pipe.Wait()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := CheckResult(result, "stream"); err != nil {
		t.Fatal(err)
	}
	if string(got) != "streamed" {
		t.Fatalf("streamed stdout = %q, want streamed", got)
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("streamed stdout was also captured: %q", result.Stdout)
	}
}

func TestOpenStdoutPipeRejectsUnsupportedRunner(t *testing.T) {
	type runOnly struct{ Runner }
	_, err := OpenStdoutPipe(context.Background(), runOnly{Runner: &FakeRunner{}}, "decoder")
	if err == nil || !strings.Contains(err.Error(), "stdout streaming") {
		t.Fatalf("error = %v, want unsupported stdout streaming", err)
	}
}

func TestBlockedRunnerRejectsStdoutPipe(t *testing.T) {
	_, err := OpenStdoutPipe(context.Background(), BlockedRunner{Reason: "dry-run"}, "decoder")
	if err == nil || !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("error = %v, want dry-run rejection", err)
	}
}

func TestFakeRunnerReplaysCall(t *testing.T) {
	fr := &FakeRunner{Stdout: "{}", ExitCode: 1}
	res := fr.Run(context.Background(), "probe", "--json", "--no-prompt")

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(fr.Calls))
	}
	if fr.Calls[0].Name != "probe" {
		t.Fatalf("recorded name = %q, want probe", fr.Calls[0].Name)
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
	if !errors.Is(res.Err, context.DeadlineExceeded) {
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
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(result.Stdout)))
	if err != nil {
		t.Fatal(err)
	}
	// Compare canonical paths: on macOS TempDir lives under /var, a
	// symlink to /private/var, while the shell reports the physical path.
	want, err := filepath.EvalSymlinks(dir)
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
	input := "fetch https://alice:s3cr3t@example.com/x?token=querysecret&keep=yes&sig=signed&X-Amz-Credential=AKIA/thing&X-Goog-Signature=googsecret --token abc123 --client-secret cli-secret\nAuthorization: Bearer topsecret\nCookie: session=xyz"
	got := RedactSensitiveText(input)
	for _, secret := range []string{"s3cr3t", "querysecret", "signed", "AKIA/thing", "googsecret", "abc123", "cli-secret", "topsecret", "session=xyz"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text still contains %q: %q", secret, got)
		}
	}
	for _, marker := range []string{"token=***", "sig=***", "X-Amz-Credential=***", "X-Goog-Signature=***", "--token=***", "--client-secret=***", "Authorization: ***", "Cookie: ***"} {
		if !strings.Contains(got, marker) {
			t.Fatalf("redacted text missing %q: %q", marker, got)
		}
	}
}

func TestSensitiveQueryKeyClassification(t *testing.T) {
	for _, key := range []string{"token", "refresh_token", "client_secret", "X-Amz-Credential", "x-amz-security-token", "X-Goog-Signature"} {
		if !IsSensitiveQueryKey(key) {
			t.Fatalf("IsSensitiveQueryKey(%q) = false", key)
		}
	}
	for _, key := range []string{"page", "version", "channel", "arch"} {
		if IsSensitiveQueryKey(key) {
			t.Fatalf("IsSensitiveQueryKey(%q) = true", key)
		}
	}
}

func TestSensitiveFlagClassification(t *testing.T) {
	for _, flag := range []string{"--token", "--client-secret", "--refresh-token", "--API-KEY"} {
		if !IsSensitiveFlag(flag) {
			t.Fatalf("IsSensitiveFlag(%q) = false", flag)
		}
	}
	for _, flag := range []string{"--version", "--channel", "-v"} {
		if IsSensitiveFlag(flag) {
			t.Fatalf("IsSensitiveFlag(%q) = true", flag)
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

func TestExecutionAllowedFailsClosedThroughBlockedRunner(t *testing.T) {
	blocked := BlockedRunner{Reason: "dry-run"}
	if ExecutionAllowed(blocked) {
		t.Fatal("BlockedRunner unexpectedly allows execution")
	}
	if !ExecutionAllowed(&FakeRunner{}) {
		t.Fatal("runner without explicit execution policy should remain executable")
	}
	if ExecutionAllowed(nil) {
		t.Fatal("nil runner unexpectedly allows execution")
	}
}

func TestRedactSensitiveTextRedactsSecretURLFragments(t *testing.T) {
	got := RedactSensitiveText("https://example.test/tool#access_token=supersecret&section=install")
	if strings.Contains(got, "supersecret") {
		t.Fatalf("fragment secret leaked: %q", got)
	}
	if !strings.Contains(got, "section=install") {
		t.Fatalf("benign fragment content was lost: %q", got)
	}
}

func TestRedactSensitiveTextForms(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		leaked     []string
		preserved  []string
		redactMark []string
	}{
		{
			name:       "flag equals and space forms",
			input:      "install --password=hunter2 --api-key AKIAEXAMPLE --secret topsecret",
			leaked:     []string{"hunter2", "AKIAEXAMPLE", "topsecret"},
			redactMark: []string{"--password=***", "--api-key=***", "--secret=***"},
		},
		{
			name:       "proxy and cookie headers",
			input:      "Proxy-Authorization: Basic dXNlcjpwYXNz\nSet-Cookie: sess=abc123",
			leaked:     []string{"dXNlcjpwYXNz", "sess=abc123"},
			redactMark: []string{"Proxy-Authorization: ***", "Set-Cookie: ***"},
		},
		{
			name:       "userinfo with port",
			input:      "fetch https://deploy@example.test:8443/pkg.tgz",
			leaked:     []string{"deploy@"},
			preserved:  []string{"example.test:8443/pkg.tgz"},
			redactMark: []string{"https://***@"},
		},
		{
			name:       "sensitive query params",
			input:      "GET https://example.test/a?password=pw123&client_secret=cs456&refresh_token=rt789&page=2",
			leaked:     []string{"pw123", "cs456", "rt789"},
			preserved:  []string{"page=2"},
			redactMark: []string{"password=***", "client_secret=***", "refresh_token=***"},
		},
		{
			name:      "benign text untouched",
			input:     "install --version 1.2.3 from channel stable",
			leaked:    nil,
			preserved: []string{"install --version 1.2.3 from channel stable"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSensitiveText(tc.input)
			for _, secret := range tc.leaked {
				if strings.Contains(got, secret) {
					t.Fatalf("redacted text still contains %q: %q", secret, got)
				}
			}
			for _, want := range tc.preserved {
				if !strings.Contains(got, want) {
					t.Fatalf("redacted text lost %q: %q", want, got)
				}
			}
			for _, mark := range tc.redactMark {
				if !strings.Contains(got, mark) {
					t.Fatalf("redacted text missing %q: %q", mark, got)
				}
			}
		})
	}
}
