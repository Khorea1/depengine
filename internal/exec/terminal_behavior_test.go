package exec

// Serial/lock/check/why agreement on terminal vs recoverable failures
// (dep-1n0.10, plan section 4).
//
// These tests use only pre-existing error types (config ambiguity, plain
// availability errors) so they compile — and fail — before the fix.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

// terminalResolveAdapter injects a ResolvePlan failure while counting
// installs, so tests can prove whether fallback ran after the failure.
type terminalResolveAdapter struct {
	testMockAdapter
	resolveErr error
	installs   int
}

func (a *terminalResolveAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if a.resolveErr != nil {
		return nil, a.resolveErr
	}
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *terminalResolveAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	a.installs++
	return a.testMockAdapter.Install(ctx, rn, tool, mc)
}

func terminalTwoMethodSchema(firstKind, secondKind string) *config.Schema {
	firstConfig := map[string]any{"pkg": "tool-first"}
	if firstKind == "github" {
		firstConfig = map[string]any{"repo": "owner/repo", "asset": "tool.tar.gz"}
	}
	return &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{firstKind, secondKind}},
		Tools: map[string]*config.Tool{"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{
				{Kind: firstKind, Config: firstConfig},
				{Kind: secondKind, Config: map[string]any{"pkg": "tool-second"}},
			},
		}},
	}
}

func TestSerialExecutorAbortsOnTerminalResolveFailure(t *testing.T) {
	github := &terminalResolveAdapter{
		testMockAdapter: testMockAdapter{kindValue: "github"},
		// A typed terminal failure surfacing at resolution time (e.g. an
		// asset mismatch), wrapped exactly like the production chain.
		resolveErr: &config.AmbiguousDistributionsError{Kind: "github", Conditions: []string{"distributions[0]", "distributions[1]"}},
	}
	fallback := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "fallback"}}
	ex := New()
	WithAdapters(github, fallback)(ex)
	WithDryRun()(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	report, err := ex.Execute(context.Background(), terminalTwoMethodSchema("github", "fallback"), "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Status != StatusFailed {
		t.Fatalf("result = %+v, want terminal failure, never silent fallback", got)
	}
	if len(got.Methods) != 1 {
		t.Fatalf("attempts = %+v, want no fallback after a terminal resolve failure", got.Methods)
	}
	if fallback.installs != 0 {
		t.Fatalf("fallback installs = %d, want 0", fallback.installs)
	}
}

func TestSerialExecutorKeepsFallbackOnAvailabilityResolveFailure(t *testing.T) {
	github := &terminalResolveAdapter{
		testMockAdapter: testMockAdapter{kindValue: "github"},
		resolveErr:      errors.New("connection reset by peer"),
	}
	fallback := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "fallback"}}
	ex := New()
	WithAdapters(github, fallback)(ex)
	WithDryRun()(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	report, err := ex.Execute(context.Background(), terminalTwoMethodSchema("github", "fallback"), "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Method != "fallback" {
		t.Fatalf("result = %+v, want availability fallback to keep working", got)
	}
	if len(got.Methods) != 2 {
		t.Fatalf("attempts = %+v, want failed github then fallback", got.Methods)
	}
}

func TestCheckDesiredStateTerminalBeatsLaterSatisfied(t *testing.T) {
	ambiguous := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"linux"}}},
			{When: &config.Condition{Arch: []string{"x86_64"}}},
		},
	}
	satisfied := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{
		Name:    "tool",
		Methods: []*config.MethodCandidate{ambiguous, satisfied},
	}
	github := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "github"}}
	cargo := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresencePresent,
		observed:        plan.ObservedIdentity{Package: "demo"},
		knownFields:     []plan.IdentityField{plan.FieldPackage},
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(github, cargo)(ex)
	WithDefaultMethodOrder([]string{"github", "cargo"})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	checked, err := ex.CheckDesiredState(context.Background(), tool, "", true)
	if err != nil {
		t.Fatalf("CheckDesiredState error = %v", err)
	}
	if checked.Verification.State != plan.StateBroken || !strings.Contains(checked.Verification.Detail, "distributions match") {
		t.Fatalf("checked = %+v, want the earlier terminal ambiguity, not the later satisfied method", checked)
	}
}

func TestResolveLockCandidateAbortsOnTerminalResolveFailure(t *testing.T) {
	terminal := &config.MethodCandidate{
		Kind:   "github",
		Label:  "terminal",
		Config: map[string]any{"repo": "owner/repo", "asset": "tool.tar.gz"},
	}
	selected := lockCandidate("cargo", "selected", "selected-pkg")
	adapter := &lockCandidateAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "github"}},
		resolveError: map[*config.MethodCandidate]error{
			terminal: &config.AmbiguousDistributionsError{Kind: "github", Conditions: []string{"distributions[0]", "distributions[1]"}},
		},
	}
	fallbackAdapter := &lockCandidateAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
	}
	ex := newLockCandidateExecutor(&run.FakeRunner{}, adapter, fallbackAdapter)
	tool := &config.Tool{Name: "demo", MethodPrefer: []string{"terminal", "selected"}, Methods: []*config.MethodCandidate{terminal, selected}}

	if _, err := ex.ResolveLockCandidate(context.Background(), tool, ""); err == nil {
		t.Fatal("expected terminal resolve failure to abort, got fallback success")
	} else if !strings.Contains(err.Error(), "distributions match") {
		t.Fatalf("err = %v, want the terminal failure, not a fallback result", err)
	}
	if fallbackAdapter.resolveCall != 0 {
		t.Fatalf("fallback ResolvePlan calls = %d, want 0 after a terminal failure", fallbackAdapter.resolveCall)
	}
}

func TestExplainToolBlocksFallbackAfterTerminal(t *testing.T) {
	ambiguous := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"linux"}}},
			{When: &config.Condition{Arch: []string{"x86_64"}}},
		},
	}
	ready := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{ambiguous, ready}}
	github := &terminalResolveAdapter{testMockAdapter: testMockAdapter{kindValue: "github"}}
	cargo := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresenceAbsent,
	}
	ex := New()
	WithAdapters(github, cargo)(ex)
	WithDefaultMethodOrder([]string{"github", "cargo"})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)

	attempts := ex.ExplainTool(context.Background(), tool, "")
	if len(attempts) != 2 {
		t.Fatalf("attempts = %+v, want both candidates diagnosed", attempts)
	}
	if attempts[0].Status != "failed" || !strings.Contains(attempts[0].Error, "distributions match") {
		t.Fatalf("attempts[0] = %+v, want the terminal diagnosis", attempts[0])
	}
	if attempts[1].Status != "failed" || !strings.Contains(attempts[1].Error, "blocked by terminal error") {
		t.Fatalf("attempts[1] = %+v, want blocked fallback, never would_install", attempts[1])
	}
}
