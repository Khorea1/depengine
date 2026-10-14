package exec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type dpkgSequenceRunner struct {
	results []run.Result
	calls   []run.FakeCall
}

func (r *dpkgSequenceRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: args})
	if len(r.results) == 0 {
		return run.Result{Err: errors.New("unexpected extra process call")}
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result
}

func dpkgQueryResult(name, status string) run.Result {
	return run.Result{Stdout: []byte(name + "\t" + status + "\n")}
}

type dpkgObservationExecutorAdapter struct {
	*executorAdapterV2Double
	native *NativeAdapter
}

func (a *dpkgObservationExecutorAdapter) Observe(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	return a.native.Observe(ctx, runner, tool, method)
}

func TestNativeDpkgUnknownObservationBlocksOrdinaryInstall(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []run.Result
	}{
		{
			name: "empty successful target output",
			results: []run.Result{
				{ExitCode: 0}, // Inconsistent query output, not absence.
				dpkgQueryResult("dpkg", "install ok installed"),
			},
		},
		{
			name: "missing status database",
			results: []run.Result{
				{ExitCode: 1},
				{ExitCode: 1},
			},
		},
		{
			name: "malformed status database",
			results: []run.Result{
				{ExitCode: 2},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &dpkgSequenceRunner{results: tc.results}
			adapter := &dpkgObservationExecutorAdapter{
				executorAdapterV2Double: &executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "native"}},
				native:                  NewNativeAdapter("debian"),
			}
			ex := New()
			WithRunner(runner)(ex)
			WithAdapters(adapter)(ex)
			tool := &config.Tool{Name: "demo", MethodOnly: []string{"native"}, Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "demo"}}}}
			result := ToolResult{Tool: tool.Name}
			ctx := context.Background()
			ex.tryMethods(ctx, ex.newRunContext(ctx, nil, ""), tool, &result, time.Now())
			if result.Status != StatusFailed {
				t.Fatalf("ordinary install status = %v, want failed for unknown native inventory", result.Status)
			}
			if adapter.installCall != 0 {
				t.Fatalf("InstallResolved calls = %d, want none", adapter.installCall)
			}
			if len(runner.results) != 0 {
				t.Fatalf("unconsumed observation results = %d", len(runner.results))
			}
		})
	}
}

func TestNativeDpkgUnknownObservationExcludedFromBatch(t *testing.T) {
	runner := &dpkgSequenceRunner{results: []run.Result{
		{ExitCode: 1}, {ExitCode: 1}, // Missing target and missing dpkg sentinel.
		{ExitCode: 2}, // Malformed target database.
	}}
	adapter := &dpkgObservationExecutorAdapter{
		executorAdapterV2Double: &executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "native"}},
		native:                  NewNativeAdapter("debian"),
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	schema := &config.Schema{Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"native"}}, Tools: map[string]*config.Tool{
		"one": {Name: "one", MethodOnly: []string{"native"}, Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "one-pkg"}}}},
		"two": {Name: "two", MethodOnly: []string{"native"}, Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "two-pkg"}}}},
	}}
	ctx := context.Background()
	rc := ex.newRunContext(ctx, schema, "debian")
	candidates, remaining, _ := ex.identifyBatchCandidates(ctx, rc, []string{"one", "two"})
	if len(candidates) != 0 || len(remaining) != 2 {
		t.Fatalf("batch candidates = %d, remaining = %v; unknown inventory must stay serial", len(candidates), remaining)
	}
	if len(runner.results) != 0 {
		t.Fatalf("unconsumed observation results = %d", len(runner.results))
	}
}
