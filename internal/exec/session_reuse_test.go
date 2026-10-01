package exec

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type sessionReuseAdapter struct {
	mu        sync.Mutex
	failTools map[string]bool
	installs  map[string]int
	packages  map[string][]string
}

func (a *sessionReuseAdapter) Kind() string { return "session-reuse" }

func (*sessionReuseAdapter) Available(context.Context, run.Runner) bool { return true }

func (*sessionReuseAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}

func (*sessionReuseAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}

func (a *sessionReuseAdapter) InstallResolved(_ context.Context, _ run.Runner, tool *config.Tool, method *config.MethodCandidate, _ *plan.ResolvedInstallPlan) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.installs[tool.Name]++
	if pkg, ok := method.Config["pkg"].(string); ok {
		a.packages[tool.Name] = append(a.packages[tool.Name], pkg)
	}
	if a.failTools[tool.Name] {
		return errors.New("configured test failure")
	}
	return nil
}

func (*sessionReuseAdapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func (*sessionReuseAdapter) CanRemove() bool { return true }

func (*sessionReuseAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

func (*sessionReuseAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

func (a *sessionReuseAdapter) installCounts() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	counts := make(map[string]int, len(a.installs))
	for name, count := range a.installs {
		counts[name] = count
	}
	return counts
}

func (a *sessionReuseAdapter) packageInstalls(tool string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.packages[tool]...)
}

func sessionReuseSchema(tools map[string]*config.Tool) *config.Schema {
	return &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"session-reuse"}},
		Tools:    tools,
	}
}

func sessionReuseTool(name, pkg string, requires ...string) *config.Tool {
	return &config.Tool{
		Name: name,
		Methods: []*config.MethodCandidate{{
			Kind:     "session-reuse",
			Config:   map[string]any{"pkg": pkg},
			Requires: requires,
		}},
	}
}

func newSessionReuseExecutor(adapter *sessionReuseAdapter) *Executor {
	executor := New()
	WithRunner(&run.FakeRunner{})(executor)
	WithAdapters(adapter)(executor)
	WithOutput(io.Discard)(executor)
	return executor
}

func TestExecuteSequentialSchemasDoNotShareRunState(t *testing.T) {
	adapter := &sessionReuseAdapter{
		failTools: make(map[string]bool),
		installs:  make(map[string]int),
		packages:  make(map[string][]string),
	}
	adapter.failTools["first-only"] = true
	executor := newSessionReuseExecutor(adapter)

	first, err := executor.Execute(context.Background(), sessionReuseSchema(map[string]*config.Tool{
		"first-only": sessionReuseTool("first-only", "first-only-pkg"),
	}), "")
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if first.Failed != 1 || first.Success != 0 || len(first.Tools) != 1 || first.Tools[0].Tool != "first-only" || first.Tools[0].Status != StatusFailed {
		t.Fatalf("first report = %+v, want only first-only failed", first)
	}

	second, err := executor.Execute(context.Background(), sessionReuseSchema(map[string]*config.Tool{
		"second-only": sessionReuseTool("second-only", "second-only-pkg"),
	}), "")
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if second.Failed != 0 || second.Success != 1 || len(second.Tools) != 1 || second.Tools[0].Tool != "second-only" || second.Tools[0].Status != StatusInstalled {
		t.Fatalf("second report = %+v, want only second-only installed", second)
	}
	if first.Failed != 1 || first.Success != 0 || len(first.Tools) != 1 || first.Tools[0].Tool != "first-only" {
		t.Fatalf("first report changed after second Execute(): %+v", first)
	}
	if got := adapter.installCounts(); got["first-only"] != 1 || got["second-only"] != 1 || len(got) != 2 {
		t.Fatalf("installs = %v, want each schema's tool installed once", got)
	}
}

func TestExecuteReinstallsNamedLazyDependencyForNewSchemaIdentity(t *testing.T) {
	adapter := &sessionReuseAdapter{
		failTools: make(map[string]bool),
		installs:  make(map[string]int),
		packages:  make(map[string][]string),
	}
	executor := newSessionReuseExecutor(adapter)

	for _, pkg := range []string{"dependency-from-schema-a", "dependency-from-schema-b"} {
		schema := sessionReuseSchema(map[string]*config.Tool{
			"shared-helper": {
				Name:           "shared-helper",
				DependencyOnly: true,
				Methods: []*config.MethodCandidate{{
					Kind:   "session-reuse",
					Config: map[string]any{"pkg": pkg},
				}},
			},
			"consumer": sessionReuseTool("consumer", "consumer-"+pkg, "shared-helper"),
		})
		report, err := executor.Execute(context.Background(), schema, "")
		if err != nil {
			t.Fatalf("Execute() for %q error = %v", pkg, err)
		}
		if report.Failed != 0 || report.Success != 2 {
			t.Fatalf("report for %q = %+v, want helper and consumer installed", pkg, report)
		}
	}

	if got := adapter.packageInstalls("shared-helper"); len(got) != 2 || got[0] != "dependency-from-schema-a" || got[1] != "dependency-from-schema-b" {
		t.Fatalf("shared-helper package installs = %v, want both schema identities in call order", got)
	}
	if got := adapter.installCounts(); got["shared-helper"] != 2 {
		t.Fatalf("shared-helper installs = %d, want one for each Execute()", got["shared-helper"])
	}
}
