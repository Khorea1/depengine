package exec

import (
	"context"
	"errors"
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"io"
	"strings"
	"sync"
	"testing"
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

func (*sessionReuseAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
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
func TestExecuteUsesSchemaMethodOrderThenConfiguredFallback(t *testing.T) {
	var installed []string
	cargo := &testMockAdapter{kindValue: "cargo", installFunc: func(string) error {
		installed = append(installed, "cargo")
		return nil
	}}
	http := &testMockAdapter{kindValue: "http", installFunc: func(string) error {
		installed = append(installed, "http")
		return nil
	}}
	executor := New()
	WithRunner(&run.FakeRunner{})(executor)
	WithOutput(io.Discard)(executor)
	WithDefaultMethodOrder([]string{"cargo", "http"})(executor)
	WithAdapters(cargo, http)(executor)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{
		{Kind: "cargo", Config: map[string]any{"pkg": "demo"}},
		{Kind: "http", Config: map[string]any{"url": "https://example.com/demo"}},
	}}

	firstSchema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"http", "cargo"}},
		Tools:    map[string]*config.Tool{"demo": tool},
	}
	if _, err := executor.Execute(context.Background(), firstSchema, ""); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	secondSchema := &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}
	if _, err := executor.Execute(context.Background(), secondSchema, ""); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if got := strings.Join(installed, ","); got != "http,cargo" {
		t.Fatalf("installed methods = %q, want schema override then configured fallback", got)
	}
}

func TestSelectedMethodsHostInterleavingDoesNotAffectExecute(t *testing.T) {
	var installed []string
	nativeAdapter := &testMockAdapter{kindValue: "native", installFunc: func(string) error {
		installed = append(installed, "native")
		return nil
	}}
	cargoAdapter := &testMockAdapter{kindValue: "cargo", installFunc: func(string) error {
		installed = append(installed, "cargo")
		return nil
	}}
	executor := New()
	WithRunner(&run.FakeRunner{})(executor)
	WithOutput(io.Discard)(executor)
	WithDefaultMethodOrder([]string{"apt", "pacman", "cargo"})(executor)
	WithAdapters(nativeAdapter, cargoAdapter)(executor)
	tool := &config.Tool{
		Name:         "demo",
		MethodPrefer: []string{"apt"},
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "demo"}},
			{Kind: "cargo", Config: map[string]any{"pkg": "demo"}},
		},
	}

	debianFirst := executor.SelectedMethods(tool, "debian")
	arch := executor.SelectedMethods(tool, "arch")
	debianAgain := executor.SelectedMethods(tool, "debian")
	if len(debianFirst) != 2 || debianFirst[0].Kind != "native" {
		t.Fatalf("Debian selection = %v, want native first", methodKinds(debianFirst))
	}
	if len(arch) != 2 || arch[0].Kind != "native" {
		t.Fatalf("Arch selection = %v, want pacman-backed native method first", methodKinds(arch))
	}
	if len(debianAgain) != 2 || debianAgain[0] != debianFirst[0] || debianAgain[1] != debianFirst[1] {
		t.Fatalf("second Debian selection = %v, want same candidates as first selection %v", methodKinds(debianAgain), methodKinds(debianFirst))
	}
	unknown := executor.SelectedMethods(tool, "unknown")
	if len(unknown) != 2 || unknown[0].Kind != "cargo" {
		t.Fatalf("unknown-clan selection = %v, want configured cargo fallback before native", methodKinds(unknown))
	}
	if _, err := executor.Execute(context.Background(), &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}, "debian"); err != nil {
		t.Fatalf("Execute() after interleaved selection queries error = %v", err)
	}
	if got := strings.Join(installed, ","); got != "native" {
		t.Fatalf("Execute() installed methods = %q, want Debian native method regardless of prior queries", got)
	}
}

func TestWithDefaultMethodOrderCopiesInput(t *testing.T) {
	order := []string{"cargo", "http"}
	executor := New()
	WithDefaultMethodOrder(order)(executor)
	order[0] = "http"
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{
		{Kind: "cargo"},
		{Kind: "http"},
	}}
	selected := executor.SelectedMethods(tool, "unknown")
	if len(selected) != 2 || selected[0].Kind != "cargo" {
		t.Fatalf("selection after caller mutates option input = %v, want cargo first", methodKinds(selected))
	}
}

func methodKinds(methods []*config.MethodCandidate) []string {
	kinds := make([]string, len(methods))
	for i, method := range methods {
		kinds[i] = method.Kind
	}
	return kinds
}

type sessionReusePlanAdapter struct {
	*executorAdapterV2Double
	packages []string
}

func (a *sessionReusePlanAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.packages = append(a.packages, resolved.Identity.Package)
	return a.executorAdapterV2Double.InstallResolved(ctx, rn, tool, method, resolved)
}

func TestExecuteUsesCurrentClanPackageOverrideForEachRun(t *testing.T) {
	adapter := &sessionReusePlanAdapter{executorAdapterV2Double: &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "native"},
		presence:        plan.PresenceAbsent,
	}}
	executor := New()
	WithRunner(&run.FakeRunner{})(executor)
	WithOutput(io.Discard)(executor)
	WithAdapters(adapter)(executor)
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{"native"},
		Methods: []*config.MethodCandidate{{
			Kind: "native",
			Config: map[string]any{
				"pkg": "base-pkg",
				"pkg_overrides": map[string]any{
					"apt":    "debian-pkg",
					"pacman": "arch-pkg",
				},
			},
		}},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}

	for _, clan := range []string{"debian", "arch", "unknown"} {
		if _, err := executor.Execute(context.Background(), schema, clan); err != nil {
			t.Fatalf("Execute(%q) error = %v", clan, err)
		}
	}
	want := []string{"debian-pkg", "arch-pkg", "base-pkg"}
	if len(adapter.packages) != len(want) {
		t.Fatalf("installed packages = %v, want %v", adapter.packages, want)
	}
	for i := range want {
		if adapter.packages[i] != want[i] {
			t.Fatalf("installed packages = %v, want Debian apt, Arch pacman, then the unoverridden base package %v", adapter.packages, want)
		}
	}
}
