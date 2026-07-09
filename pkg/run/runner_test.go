package run

import (
	"context"
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
