package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

func batchProbeExecutor(adapter AdapterV2) *Executor {
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithDefaultMethodOrder([]string{"native"})(ex)
	return ex
}
func batchProbeRunContext(ex *Executor, s *config.Schema, report *ExecReport) *runContext {
	rc := ex.newRunContext(context.Background(), s, "arch")
	if report != nil {
		rc.report = report
	}
	return rc
}

func batchProbeTool() *config.Tool {
	return &config.Tool{
		Name:    "demo",
		Methods: []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "demo"}}},
	}
}

type batchIdentityAdapter struct {
	*executorAdapterV2Double
	observedPackage     string
	observedEnvironment string
}

func (a *batchIdentityAdapter) Observe(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	a.observedEnvironment, _ = method.Config["environment"].(string)
	observation, err := a.executorAdapterV2Double.Observe(ctx, runner, tool, method)
	if err == nil && a.observedPackage != "" {
		observation.Identity.Package = a.observedPackage
	}
	return observation, err
}

func TestBatchPresenceObservesResolvedEnvironment(t *testing.T) {
	adapter := &batchIdentityAdapter{executorAdapterV2Double: &executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "conda"}, presence: plan.PresencePresent}}
	ex := batchProbeExecutor(adapter)
	method := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "demo", "environment": "old"}}
	tool := &config.Tool{Name: "demo"}
	resolved := plan.New("demo", "conda", true)
	resolved.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}

	if _, ok := ex.batchPresence(context.Background(), tool, method, &resolved); !ok {
		t.Fatal("batchPresence() rejected a valid observation")
	}
	if adapter.observedEnvironment != "tools" {
		t.Fatalf("observed environment = %q, want resolved target %q", adapter.observedEnvironment, "tools")
	}
	if got := method.Config["environment"]; got != "old" {
		t.Fatalf("source method environment changed to %v", got)
	}
}

func TestBatchV2ProbeStatesPreserveSerialFallback(t *testing.T) {
	tests := []struct {
		name           string
		presence       plan.PresenceState
		observeErr     error
		wantCandidates int
		wantRemaining  int
		wantReport     bool
		observedPkg    string
	}{
		{name: "present", presence: plan.PresencePresent, wantReport: true},
		{name: "present but drifted", presence: plan.PresencePresent, observedPkg: "other", wantCandidates: 0, wantRemaining: 1},
		{name: "absent", presence: plan.PresenceAbsent, wantCandidates: 1},
		{name: "unknown", presence: plan.PresenceUnknown, wantRemaining: 1},
		{name: "broken", presence: plan.PresenceBroken, wantRemaining: 1},
		{name: "error", observeErr: errors.New("probe failed"), wantRemaining: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &batchIdentityAdapter{
				executorAdapterV2Double: &executorAdapterV2Double{
					testMockAdapter: testMockAdapter{kindValue: "native"},
					presence:        tt.presence,
					observeErr:      tt.observeErr,
				},
				observedPackage: tt.observedPkg,
			}
			ex := batchProbeExecutor(adapter)
			report := &ExecReport{}
			schema := &config.Schema{Tools: map[string]*config.Tool{"demo": batchProbeTool()}}
			rc := batchProbeRunContext(ex, schema, report)
			candidates, remaining, _ := ex.identifyBatchCandidates(context.Background(), rc, []string{"demo"})
			if len(candidates) != tt.wantCandidates || len(remaining) != tt.wantRemaining {
				t.Fatalf("identifyBatchCandidates() = candidates %d, remaining %d; want %d, %d", len(candidates), len(remaining), tt.wantCandidates, tt.wantRemaining)
			}
			if tt.wantReport {
				if len(report.Tools) != 1 || report.Tools[0].Status != StatusAlready {
					t.Fatalf("report = %+v, want one already result", report.Tools)
				}
			}
			if adapter.checkCalls != 0 {
				t.Fatalf("Check() calls = %d, want 0 for V2", adapter.checkCalls)
			}
		})
	}
}

func TestVerifyBatchV2OnlyPresentCommitsBatchResult(t *testing.T) {
	for _, tt := range []struct {
		name          string
		presence      plan.PresenceState
		observeErr    error
		wantRemaining int
		wantTools     int
		observedPkg   string
	}{
		{name: "present", presence: plan.PresencePresent, wantTools: 1},
		{name: "present but drifted", presence: plan.PresencePresent, observedPkg: "other", wantRemaining: 1},
		{name: "absent", presence: plan.PresenceAbsent, wantRemaining: 1},
		{name: "unknown", presence: plan.PresenceUnknown, wantRemaining: 1},
		{name: "broken", presence: plan.PresenceBroken, wantRemaining: 1},
		{name: "error", observeErr: errors.New("probe failed"), wantRemaining: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &batchIdentityAdapter{
				executorAdapterV2Double: &executorAdapterV2Double{
					testMockAdapter: testMockAdapter{kindValue: "native"},
					presence:        tt.presence,
					observeErr:      tt.observeErr,
				},
				observedPackage: tt.observedPkg,
			}
			ex := batchProbeExecutor(adapter)
			tool := batchProbeTool()
			resolved, err := ex.ResolveCandidatePlan(context.Background(), tool, tool.Methods[0], "arch")
			if err != nil {
				t.Fatalf("ResolveCandidatePlan() error = %v", err)
			}
			candidate := batchCandidate{toolName: tool.Name, tool: tool, method: tool.Methods[0], resolvedPlan: resolved}
			report := &ExecReport{}
			rc := batchProbeRunContext(ex, nil, report)
			remaining := ex.verifyBatchInstall(rc, []batchCandidate{candidate}, nil, nil)
			if len(remaining) != tt.wantRemaining || len(report.Tools) != tt.wantTools {
				t.Fatalf("verifyBatchInstall() = remaining %d, report tools %d; want %d, %d", len(remaining), len(report.Tools), tt.wantRemaining, tt.wantTools)
			}
			if tt.wantTools == 1 && report.Tools[0].Status != StatusInstalled {
				t.Fatalf("status = %v, want installed", report.Tools[0].Status)
			}
			if adapter.checkCalls != 0 {
				t.Fatalf("Check() calls = %d, want 0 for V2", adapter.checkCalls)
			}
		})
	}
}

func TestBatchLegacyProbeStillUsesCheck(t *testing.T) {
	checks := 0
	adapter := &testMockAdapter{
		kindValue: "native",
		checkFunc: func(string) bool {
			checks++
			return true
		},
	}
	ex := batchProbeExecutor(adapter)
	report := &ExecReport{}
	schema := &config.Schema{Tools: map[string]*config.Tool{"demo": batchProbeTool()}}
	rc := batchProbeRunContext(ex, schema, report)
	candidates, remaining, _ := ex.identifyBatchCandidates(context.Background(), rc, []string{"demo"})
	if len(candidates) != 0 || len(remaining) != 0 || len(report.Tools) != 1 || report.Tools[0].Status != StatusAlready {
		t.Fatalf("legacy batch result = candidates %d, remaining %d, report %+v; want already", len(candidates), len(remaining), report.Tools)
	}
	if checks != 1 {
		t.Fatalf("Check() calls = %d, want 1", checks)
	}
}

func TestBatchDryRunReportsResolvedPlanAndResolvesOnce(t *testing.T) {
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "native"},
		presence:        plan.PresenceAbsent,
	}
	ex := batchProbeExecutor(adapter)
	report := &ExecReport{}
	tool := batchProbeTool()
	schema := &config.Schema{Tools: map[string]*config.Tool{tool.Name: tool}}
	rc := batchProbeRunContext(ex, schema, report)
	candidates, remaining, _ := ex.identifyBatchCandidates(context.Background(), rc, []string{tool.Name})
	if len(candidates) != 1 || len(remaining) != 0 {
		t.Fatalf("identifyBatchCandidates() = candidates %d, remaining %d; want 1, 0", len(candidates), len(remaining))
	}
	if adapter.resolveCall != 1 {
		t.Fatalf("ResolvePlan() calls = %d, want 1", adapter.resolveCall)
	}
	if candidates[0].resolvedPlan == nil {
		t.Fatal("batch candidate has nil resolved plan")
	}

	ex.reportBatchDryRun(rc, candidates)
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusWouldInstall {
		t.Fatalf("report = %+v, want one would-install result", report.Tools)
	}
	if report.Tools[0].PlanIntent != candidates[0].resolvedPlan {
		t.Fatalf("dry-run plan = %p, candidate resolved plan = %p; want exact resolved plan", report.Tools[0].PlanIntent, candidates[0].resolvedPlan)
	}
	if adapter.resolveCall != 1 {
		t.Fatalf("ResolvePlan() calls after dry-run report = %d, want 1", adapter.resolveCall)
	}
}

func TestVerifiedBatchInstallReportsSameResolvedPlan(t *testing.T) {
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "native"},
		presence:        plan.PresenceAbsent,
	}
	ex := batchProbeExecutor(adapter)
	tool := batchProbeTool()
	report := &ExecReport{}
	schema := &config.Schema{Tools: map[string]*config.Tool{tool.Name: tool}}
	rc := batchProbeRunContext(ex, schema, report)
	candidates, remaining, _ := ex.identifyBatchCandidates(context.Background(), rc, []string{tool.Name})
	if len(candidates) != 1 || len(remaining) != 0 {
		t.Fatalf("identifyBatchCandidates() = candidates %d, remaining %d; want 1, 0", len(candidates), len(remaining))
	}

	adapter.presence = plan.PresencePresent
	remaining = ex.verifyBatchInstall(rc, candidates, remaining, make(map[string]*candidateResolutionSeed))
	if len(remaining) != 0 || len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("verified batch = remaining %d, report %+v; want installed", len(remaining), report.Tools)
	}
	if report.Tools[0].PlanIntent != candidates[0].resolvedPlan {
		t.Fatalf("installed plan = %p, candidate resolved plan = %p; want exact resolved plan", report.Tools[0].PlanIntent, candidates[0].resolvedPlan)
	}
	if adapter.resolveCall != 1 {
		t.Fatalf("ResolvePlan() calls = %d, want 1", adapter.resolveCall)
	}
}

func TestBatchFallbackReusesResolvedPlan(t *testing.T) {
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "native"},
		presence:        plan.PresenceAbsent,
	}
	ex := batchProbeExecutor(adapter)
	tool := batchProbeTool()
	report := &ExecReport{}
	schema := &config.Schema{Tools: map[string]*config.Tool{tool.Name: tool}}
	rc := batchProbeRunContext(ex, schema, report)
	candidates, remaining, _ := ex.identifyBatchCandidates(context.Background(), rc, []string{tool.Name})
	if len(candidates) != 1 || len(remaining) != 0 {
		t.Fatalf("identifyBatchCandidates() = candidates %d, remaining %d; want 1, 0", len(candidates), len(remaining))
	}

	seed := &candidateResolutionSeed{method: candidates[0].method, resolved: candidates[0].resolvedPlan}
	ctx := context.Background()
	result := ex.executeToolWithResolution(ctx, rc, tool, seed)
	if result.Status != StatusInstalled {
		t.Fatalf("fallback result = %+v, want installed", result)
	}
	if adapter.resolveCall != 1 {
		t.Fatalf("ResolvePlan() calls after serial fallback = %d, want 1", adapter.resolveCall)
	}
	if adapter.installed != candidates[0].resolvedPlan {
		t.Fatalf("InstallResolved plan = %p, batch resolved plan = %p; want same plan", adapter.installed, candidates[0].resolvedPlan)
	}
}

func TestBatchDoesNotBypassCandidateCapabilityBoundary(t *testing.T) {
	nativeChecks := 0
	nativeAdapter := &testMockAdapter{
		kindValue: "native",
		checkFunc: func(string) bool {
			nativeChecks++
			return false
		},
	}
	gitAdapter := &testMockAdapter{
		kindValue: "git",
		checkFunc: func(string) bool { return false },
	}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"native", "git"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name: "demo",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}},
					{Kind: "git", Config: map[string]any{"url": "https://example.test/demo.git"}},
				},
			},
		},
	}
	ex := New()
	WithAdapters(nativeAdapter, gitAdapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	WithDryRun()(ex)

	report, err := ex.Execute(context.Background(), schema, "arch")
	if err != nil {
		t.Fatal(err)
	}
	if nativeChecks != 0 {
		t.Fatalf("native adapter was probed %d times; static capability rejection must happen first", nativeChecks)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusWouldInstall || report.Tools[0].MethodKind != "git" {
		t.Fatalf("report = %+v, want git fallback planned", report.Tools)
	}
	if len(report.Tools[0].Methods) != 2 || report.Tools[0].Methods[0].Status != "skip_capability" || report.Tools[0].Methods[1].Status != "success" {
		t.Fatalf("method attempts = %+v, want native capability rejection then git success", report.Tools[0].Methods)
	}
	if !strings.Contains(report.Tools[0].Methods[0].Error, "exact-version") {
		t.Fatalf("capability rejection = %q, want exact_version", report.Tools[0].Methods[0].Error)
	}
}

func TestBatchNativeInstallSkipsSingleton(t *testing.T) {
	runner := &run.FakeRunner{}
	ex := New()
	WithRunner(runner)(ex)
	rc := ex.newRunContext(context.Background(), nil, "arch")

	resolved := plan.New("demo", "native", true)
	resolved.Identity.Package = "demo"
	candidate := batchCandidate{
		toolName:     "demo",
		tool:         &config.Tool{Name: "demo"},
		method:       &config.MethodCandidate{Kind: "native"},
		resolvedPlan: &resolved,
	}

	if ex.batchNativeInstall(context.Background(), rc, []batchCandidate{candidate}) {
		t.Fatal("batchNativeInstall() accepted a singleton candidate")
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("runner calls = %d, want 0 for singleton batch", len(runner.Calls))
	}
}

func TestVerifiedBatchInstallPersistsCommitWhenPostInstallFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	checks := 0
	adapter := &testMockAdapter{
		kindValue: "native",
		checkFunc: func(string) bool {
			checks++
			return checks > 2
		},
	}
	runner := &sequenceRunner{results: []run.Result{
		{},            // batch install succeeds
		{ExitCode: 1}, // demo post-install hook fails
	}}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"native"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name:        "demo",
				PostInstall: []config.Hook{{Run: []string{"post-hook"}}},
				Methods: []*config.MethodCandidate{{
					Kind: "native", Config: map[string]any{"pkg": "demo"},
				}},
			},
			"helper": {
				Name: "helper",
				Methods: []*config.MethodCandidate{{
					Kind: "native", Config: map[string]any{"pkg": "helper"},
				}},
			},
		},
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithAllowArbitraryCode()(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), schema, "arch")
	if err != nil {
		t.Fatal(err)
	}
	if checks != 4 {
		t.Fatalf("native checks = %d, want two pre-batch and two verification probes", checks)
	}
	if len(report.Tools) != 2 {
		t.Fatalf("report = %+v, want two results", report.Tools)
	}
	var result *ToolResult
	for i := range report.Tools {
		if report.Tools[i].Tool == "demo" {
			result = &report.Tools[i]
			break
		}
	}
	if result == nil {
		t.Fatalf("report = %+v, missing demo result", report.Tools)
	}
	if result.Status != StatusFailed || !result.InstallCommitted {
		t.Fatalf("result = %+v, want failed post-install with committed install", *result)
	}
	if result.Method != "native" || result.MethodKind != "native" || result.PlanIntent == nil {
		t.Fatalf("batch result lost method/plan metadata: %+v", *result)
	}
	if !strings.Contains(result.Error, "post-install") {
		t.Fatalf("result error = %q, want post-install failure", result.Error)
	}

	persisted, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	toolState, ok := persisted.Tools["demo"]
	if !ok {
		t.Fatalf("committed batch install missing from state: %#v", persisted.Tools)
	}
	if toolState.MethodKind != "native" || toolState.Method != "native" {
		t.Fatalf("persisted batch method = %#v, want native metadata", toolState)
	}
}
