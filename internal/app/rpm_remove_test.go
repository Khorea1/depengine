package app

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type rpmRemovalRunner struct {
	results []run.Result
	calls   int
	mutated bool
}

func (r *rpmRemovalRunner) Run(_ context.Context, name string, args ...string) run.Result {
	if name != "rpm" {
		r.mutated = true
		return run.Result{ExitCode: 0}
	}
	if r.calls >= len(r.results) {
		return run.Result{Err: context.Canceled}
	}
	result := r.results[r.calls]
	r.calls++
	return result
}

func (r *rpmRemovalRunner) RunWithEnv(ctx context.Context, env map[string]string, _ []string, name string, args ...string) run.Result {
	if env["LC_ALL"] != "C" {
		return run.Result{Err: context.Canceled}
	}
	return r.Run(ctx, name, args...)
}

func TestRemoveUnknownRpmObservationPreservesStateAndSkipsMutation(t *testing.T) {
	runner := &rpmRemovalRunner{results: []run.Result{
		{Stdout: []byte("bash\tx86_64\n")},
		{ExitCode: 1, Stdout: []byte("package filesystem is not installed\n"), Stderr: []byte("rpmdb damaged")},
	}}
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "native", MethodKind: "native", Config: map[string]any{"pkg": "bash"}},
	}}
	sess := testRemoveSession(st, "tool")
	sess.runner = runner
	exec.WithAdapters(exec.NewNativeAdapter("fedora"))(sess.executor)
	exec.WithRunner(runner)(sess.executor)

	if sess.removeTrackedTool(context.Background(), "tool", false) {
		t.Fatal("remove succeeded with an unknown RPM observation")
	}
	if _, exists := st.Tools["tool"]; !exists {
		t.Fatal("tracked state was deleted with an unknown RPM observation")
	}
	if runner.mutated {
		t.Fatal("package-manager mutation ran with an unknown RPM observation")
	}
	if runner.calls != 2 {
		t.Fatalf("RPM query calls = %d, want target and sentinel", runner.calls)
	}
}
