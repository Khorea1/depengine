package run

import (
	"bytes"
	"context"
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
