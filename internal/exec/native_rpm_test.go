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

func rpmQueryResult(name, arch string) run.Result {
	return run.Result{Stdout: []byte(name + "\t" + arch + "\n")}
}

type rpmSequenceRunner struct {
	results []run.Result
}

func (r *rpmSequenceRunner) Run(_ context.Context, name string, args ...string) run.Result {
	if len(r.results) == 0 {
		return run.Result{Err: errors.New("unexpected extra RPM query")}
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result
}

func (r *rpmSequenceRunner) RunWithEnv(ctx context.Context, env map[string]string, _ []string, name string, args ...string) run.Result {
	if env["LC_ALL"] != "C" {
		return run.Result{Err: errors.New("RPM query did not force the C locale")}
	}
	return r.Run(ctx, name, args...)
}

type rpmObservationExecutorAdapter struct {
	*executorAdapterV2Double
	native *NativeAdapter
}

func (a *rpmObservationExecutorAdapter) Observe(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	return a.native.Observe(ctx, runner, tool, method)
}

func TestNativeRpmUnknownObservationBlocksOrdinaryInstall(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []run.Result
	}{
		{
			name: "empty successful target output",
			results: []run.Result{
				{ExitCode: 0}, // Inconsistent query output, not absence.
				rpmQueryResult("filesystem", "x86_64"),
			},
		},
		{
			name: "missing database",
			results: []run.Result{
				{ExitCode: 1, Stdout: []byte("package demo is not installed\n")},
				{ExitCode: 1, Stdout: []byte("package filesystem is not installed\n")},
			},
		},
		{
			name: "malformed database",
			results: []run.Result{
				{ExitCode: 2},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &rpmSequenceRunner{results: tc.results}
			adapter := &rpmObservationExecutorAdapter{
				executorAdapterV2Double: &executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "native"}},
				native:                  NewNativeAdapter("fedora"),
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
		})
	}
}

func TestNativeRpmUnknownObservationExcludedFromBatch(t *testing.T) {
	runner := &rpmSequenceRunner{results: []run.Result{
		{ExitCode: 1}, {ExitCode: 1}, // Missing target and missing filesystem sentinel.
		{ExitCode: 2}, // Malformed target database query.
	}}
	adapter := &rpmObservationExecutorAdapter{
		executorAdapterV2Double: &executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "native"}},
		native:                  NewNativeAdapter("fedora"),
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	schema := &config.Schema{Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"native"}}, Tools: map[string]*config.Tool{
		"one": {Name: "one", MethodOnly: []string{"native"}, Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "one-pkg"}}}},
		"two": {Name: "two", MethodOnly: []string{"native"}, Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "two-pkg"}}}},
	}}
	ctx := context.Background()
	rc := ex.newRunContext(ctx, schema, "fedora")
	candidates, remaining, _ := ex.identifyBatchCandidates(ctx, rc, []string{"one", "two"})
	if len(candidates) != 0 || len(remaining) != 2 {
		t.Fatalf("batch candidates = %d, remaining = %v; unknown inventory must stay serial", len(candidates), remaining)
	}
}

func TestRpmProviderAliasesUseAuthoritativeObservation(t *testing.T) {
	tool := &config.Tool{Name: "git"}
	method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "git"}}
	adapters := []struct {
		name    string
		adapter AdapterV2
	}{
		{name: "suse native provider", adapter: NewNativeAdapter("suse")},
		{name: "explicit dnf manager alias", adapter: &NativeByManagerAdapter{managerName: "dnf"}},
		{name: "explicit zypper manager alias", adapter: &NativeByManagerAdapter{managerName: "zypper"}},
	}
	for _, tc := range adapters {
		t.Run(tc.name, func(t *testing.T) {
			runner := &rpmSequenceRunner{results: []run.Result{
				rpmQueryResult("git", "x86_64"),
				rpmQueryResult("filesystem", "x86_64"),
			}}
			observation, err := tc.adapter.Observe(context.Background(), runner, tool, method)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if observation.Presence != plan.PresencePresent || observation.Identity.Package != "git" {
				t.Fatalf("Observe() = %#v, want present git", observation)
			}
		})
	}
}
func TestRpmCheckUsesAuthoritativeObservationForAllProviders(t *testing.T) {
	for _, adapter := range []struct {
		name string
		a    interface {
			Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool
		}
	}{
		{name: "native Fedora", a: NewNativeAdapter("fedora")},
		{name: "native SUSE", a: NewNativeAdapter("suse")},
		{name: "explicit dnf", a: &NativeByManagerAdapter{managerName: "dnf"}},
		{name: "explicit zypper", a: &NativeByManagerAdapter{managerName: "zypper"}},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			tool := &config.Tool{Name: "bash"}
			method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "bash"}}
			for _, tc := range []struct {
				name    string
				results []run.Result
				want    bool
			}{
				{name: "present", results: []run.Result{rpmQueryResult("bash", "x86_64"), rpmQueryResult("filesystem", "x86_64")}, want: true},
				{name: "absent", results: []run.Result{{ExitCode: 1, Stdout: []byte("package bash is not installed\n")}, rpmQueryResult("filesystem", "x86_64")}},
				{name: "query error with sentinel success", results: []run.Result{{ExitCode: 1}, rpmQueryResult("filesystem", "x86_64")}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					runner := &rpmSequenceRunner{results: tc.results}
					if got := adapter.a.Check(context.Background(), runner, tool, method); got != tc.want {
						t.Fatalf("Check() = %v, want %v", got, tc.want)
					}
				})
			}
		})
	}
}

func TestNativeRpmUncleanExecutionBlocksObservation(t *testing.T) {
	runner := &rpmSequenceRunner{results: []run.Result{{Err: errors.New("probe failed")}}}
	adapter := NewNativeAdapter("fedora")
	tool := &config.Tool{Name: "git"}
	observation, err := adapter.Observe(context.Background(), runner, tool, &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "git"}})
	if err == nil {
		t.Fatal("Observe() should surface query execution failure")
	}
	if observation.Presence != plan.PresenceBroken {
		t.Fatalf("Observe() presence = %v, want broken", observation.Presence)
	}
}
