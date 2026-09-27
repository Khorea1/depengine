package run

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestEnvironmentChild(t *testing.T) {
	if os.Getenv("DEPENGINE_TEST_ENV_CHILD") != "1" {
		return
	}
	if os.Getenv("DEPENGINE_TEST_ENV_OVERRIDE") != "child" || os.Getenv("DEPENGINE_TEST_ENV_SECRET") != "runtime-secret" {
		t.Fatal("child did not receive environment overrides")
	}
	_, _ = os.Stdout.WriteString("runtime-secret")
	_, _ = os.Stderr.WriteString("runtime-secret")
}

func TestValidatedEnvironmentChild(t *testing.T) {
	if os.Getenv("DEPENGINE_TEST_VALIDATED_ENV_CHILD") != "1" {
		return
	}
	if os.Getenv("DEPENGINE_TEST_ENV_SECRET") != "runtime-secret" {
		t.Fatal("child did not receive sensitive environment override")
	}
	_, _ = os.Stdout.WriteString("runtime-secret\nresolved-commit\n")
	_, _ = os.Stderr.WriteString("runtime-secret")
}

func TestOmittedEnvironmentChild(t *testing.T) {
	mode := os.Getenv("DEPENGINE_TEST_ENV_PROBE")
	if mode == "" {
		return
	}
	if got := os.Getenv("DEPENGINE_TEST_ENV_UNRELATED"); got != "preserved" {
		t.Fatalf("unrelated environment = %q, want preserved", got)
	}
	if got := os.Getenv("DEPENGINE_TRACE_ID"); got != "omission-trace" {
		t.Fatalf("trace environment = %q, want omission-trace", got)
	}
	switch mode {
	case "omitted":
		if got, ok := os.LookupEnv("DEPENGINE_TEST_ENV_SECRET"); ok {
			t.Fatalf("omitted secret reached child with value %q", got)
		}
	case "present":
		if got := os.Getenv("DEPENGINE_TEST_ENV_SECRET"); got != "parent-secret" {
			t.Fatalf("secret = %q, want parent-secret in unscoped child", got)
		}
	default:
		t.Fatalf("unexpected probe mode %q", mode)
	}
}

func TestWithOmittedEnvAppliesToAllOSExecRunnerModes(t *testing.T) {
	t.Setenv("DEPENGINE_TEST_ENV_SECRET", "parent-secret")
	t.Setenv("DEPENGINE_TEST_ENV_UNRELATED", "preserved")
	t.Setenv("DEPENGINE_TRACE_ID", "omission-trace")
	t.Setenv("DEPENGINE_TEST_ENV_PROBE", "omitted")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithOmittedEnv(context.Background(), "DEPENGINE_TEST_ENV_SECRET")
	childArgs := []string{"-test.run=^TestOmittedEnvironmentChild$"}
	dir := t.TempDir()
	tests := []struct {
		name string
		run  func() Result
	}{
		{name: "Run", run: func() Result { return (OSExecRunner{}).Run(ctx, exe, childArgs...) }},
		{name: "RunWithEnv", run: func() Result {
			return (OSExecRunner{}).RunWithEnv(ctx, map[string]string{
				"DEPENGINE_TEST_ENV_PROBE":  "omitted",
				"DEPENGINE_TEST_ENV_SECRET": "child-override-secret",
			}, nil, exe, childArgs...)
		}},
		{name: "RunInDir", run: func() Result {
			return (OSExecRunner{}).RunInDir(ctx, dir, exe, childArgs...)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if result := tc.run(); result.Err != nil || result.ExitCode != 0 {
				t.Fatalf("child failed: exit=%d err=%v stderr=%s", result.ExitCode, result.Err, result.Stderr)
			}
		})
	}
	if got := os.Getenv("DEPENGINE_TEST_ENV_SECRET"); got != "parent-secret" {
		t.Fatalf("parent secret environment changed to %q", got)
	}
}

func TestWithOmittedEnvIsPerChildContext(t *testing.T) {
	t.Setenv("DEPENGINE_TEST_ENV_SECRET", "parent-secret")
	t.Setenv("DEPENGINE_TEST_ENV_UNRELATED", "preserved")
	t.Setenv("DEPENGINE_TRACE_ID", "omission-trace")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestOmittedEnvironmentChild$"}
	base := context.Background()
	omitted := WithOmittedEnv(base, "DEPENGINE_TEST_ENV_SECRET")
	for _, tc := range []struct {
		name string
		ctx  context.Context
		mode string
	}{
		{name: "scoped", ctx: omitted, mode: "omitted"},
		{name: "unscoped", ctx: base, mode: "present"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := (OSExecRunner{}).RunWithEnv(tc.ctx, map[string]string{"DEPENGINE_TEST_ENV_PROBE": tc.mode}, nil, exe, args...)
			if result.Err != nil || result.ExitCode != 0 {
				t.Fatalf("child failed: exit=%d err=%v stderr=%s", result.ExitCode, result.Err, result.Stderr)
			}
		})
	}
}

func TestRunWithEnvValidatedKeepsRawOutputBehindBoundary(t *testing.T) {
	t.Setenv("DEPENGINE_TEST_ENV_SECRET", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	validatorSawSecret := false
	result := RunWithEnvValidated(context.Background(), OSExecRunner{}, map[string]string{
		"DEPENGINE_TEST_VALIDATED_ENV_CHILD": "1",
		"DEPENGINE_TEST_ENV_SECRET":          "runtime-secret",
	}, []string{"runtime-secret"}, exe, func(stdout []byte) ([]byte, error) {
		validatorSawSecret = bytes.Contains(stdout, []byte("runtime-secret"))
		if !validatorSawSecret || !bytes.Contains(stdout, []byte("resolved-commit")) {
			return nil, errors.New("unexpected child output")
		}
		return []byte("resolved-commit"), nil
	}, "-test.run=^TestValidatedEnvironmentChild$")
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("validated child failed: exit=%d err=%v", result.ExitCode, result.Err)
	}
	if !validatorSawSecret {
		t.Fatal("validator did not receive raw child output")
	}
	if got := string(result.Stdout); got != "resolved-commit" {
		t.Fatalf("validated stdout = %q, want resolved-commit", got)
	}
	if len(result.Stderr) != 0 || bytes.Contains(result.Stdout, []byte("runtime-secret")) {
		t.Fatalf("raw sensitive child output escaped: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if got := os.Getenv("DEPENGINE_TEST_ENV_SECRET"); got != "" {
		t.Fatalf("child environment mutated parent secret to %q", got)
	}
}

func TestRunWithEnvValidatedSuppressesValidationDetails(t *testing.T) {
	const secret = "runtime-secret"
	result := RunWithEnvValidated(context.Background(), &FakeRunner{Stdout: secret}, map[string]string{
		"TOKEN": secret,
	}, []string{secret}, "git", func([]byte) ([]byte, error) {
		return nil, errors.New("runtime-secret was rejected")
	}, "ls-remote")
	if result.Err == nil || !strings.Contains(result.Err.Error(), "output validation failed") {
		t.Fatalf("error = %v, want generic validation failure", result.Err)
	}
	if strings.Contains(result.Err.Error(), secret) || len(result.Stdout) != 0 || len(result.Stderr) != 0 {
		t.Fatalf("validation detail escaped: %+v", result)
	}
}

func TestRunWithEnvValidatedFailsClosedForUnsupportedRunner(t *testing.T) {
	type runOnly struct{ Runner }
	result := RunWithEnvValidated(context.Background(), runOnly{Runner: &FakeRunner{}}, nil, nil, "git", func(stdout []byte) ([]byte, error) {
		return stdout, nil
	}, "ls-remote")
	if result.Err == nil || !strings.Contains(result.Err.Error(), "validated per-call environment output") {
		t.Fatalf("error = %v, want unsupported validated environment", result.Err)
	}
}

func TestRunWithEnvIsPerChildAndSuppressesSecretOutput(t *testing.T) {
	t.Setenv("DEPENGINE_TEST_ENV_OVERRIDE", "parent")
	t.Setenv("DEPENGINE_TEST_ENV_SECRET", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := RunWithEnv(context.Background(), OSExecRunner{}, map[string]string{
		"DEPENGINE_TEST_ENV_CHILD":    "1",
		"DEPENGINE_TEST_ENV_OVERRIDE": "child",
		"DEPENGINE_TEST_ENV_SECRET":   "runtime-secret",
	}, []string{"runtime-secret"}, exe, "-test.run=^TestEnvironmentChild$")
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("child failed: exit=%d err=%v", result.ExitCode, result.Err)
	}
	if len(result.Stdout) != 0 || len(result.Stderr) != 0 {
		t.Fatalf("sensitive child output escaped: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if os.Getenv("DEPENGINE_TEST_ENV_OVERRIDE") != "parent" || os.Getenv("DEPENGINE_TEST_ENV_SECRET") != "" {
		t.Fatal("child environment mutated the parent")
	}
}

func TestLoggingRunnerValidatedOutputDoesNotLogRawSecret(t *testing.T) {
	const secret = "runtime-secret"
	var logged bytes.Buffer
	runner := NewLoggingRunner(&FakeRunner{Stdout: secret + "\nresolved-commit\n"}, slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	result := RunWithEnvValidated(context.Background(), runner, map[string]string{"TOKEN": secret}, []string{secret}, "git", func(stdout []byte) ([]byte, error) {
		if !bytes.Contains(stdout, []byte(secret)) {
			return nil, errors.New("validator did not see raw output")
		}
		return []byte("resolved-commit"), nil
	}, "ls-remote")
	if result.Err != nil || string(result.Stdout) != "resolved-commit" {
		t.Fatalf("result = %+v, want validated output", result)
	}
	if strings.Contains(logged.String(), secret) {
		t.Fatalf("raw secret appeared in structured command log: %s", logged.String())
	}
}

func TestLoggingRunnerOmitsSecretChildOutput(t *testing.T) {
	var logged bytes.Buffer
	runner := NewLoggingRunner(&FakeRunner{Stderr: "runtime-secret", ExitCode: 1}, slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	result := RunWithEnv(context.Background(), runner, map[string]string{"TOKEN": "runtime-secret"}, []string{"runtime-secret"}, "git", "clone")
	if result.ExitCode != 1 || len(result.Stderr) != 0 {
		t.Fatalf("result = %+v, want redacted nonzero exit", result)
	}
	if strings.Contains(logged.String(), "runtime-secret") {
		t.Fatal("secret appeared in structured command log")
	}
}
