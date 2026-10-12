package exec

// Tests for the distribution-specialization gate in the attempt pipeline
// (proposal E, 8.3): ineligible distributions fall through to the next
// method, ambiguity aborts the tool without silent fallback.

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

func TestExecutorFallsThroughWhenNoDistributionMatches(t *testing.T) {
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"github", "fallback"}},
		Tools: map[string]*config.Tool{"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{
				{
					Kind:   "github",
					Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
					Distributions: []config.Distribution{
						{When: &config.Condition{OS: []string{"linux"}}},
					},
				},
				{Kind: "fallback", Config: map[string]any{}},
			},
		}},
	}
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"}, &testMockAdapter{kindValue: "fallback"})(ex)
	WithDryRun()(ex)
	WithFacts(&platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"})(ex)
	report, err := ex.Execute(context.Background(), schema, "windows")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Method != "fallback" {
		t.Fatalf("result = %+v, want fallback after ineligible distributions", got)
	}
	if len(got.Methods) != 2 || got.Methods[0].Status != "skip_no_distribution" {
		t.Fatalf("attempts = %+v, want skip_no_distribution then fallback", got.Methods)
	}
}

func TestExecutorAbortsOnAmbiguousDistributions(t *testing.T) {
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"github", "fallback"}},
		Tools: map[string]*config.Tool{"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{
				{
					Kind:   "github",
					Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
					Distributions: []config.Distribution{
						{When: &config.Condition{OS: []string{"linux"}}},
						{When: &config.Condition{Arch: []string{"x86_64"}}},
					},
				},
				{Kind: "fallback", Config: map[string]any{}},
			},
		}},
	}
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"}, &testMockAdapter{kindValue: "fallback"})(ex)
	WithDryRun()(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	report, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Status != StatusFailed {
		t.Fatalf("result = %+v, want terminal failure on ambiguity", got)
	}
	if !strings.Contains(got.Error, "distributions match") {
		t.Fatalf("error = %q, want ambiguity detail", got.Error)
	}
	if len(got.Methods) != 1 {
		t.Fatalf("attempts = %+v, want no silent fallback after ambiguity", got.Methods)
	}
}

func githubDistributionsTool() *config.Tool {
	return &config.Tool{
		Name: "tool",
		Methods: []*config.MethodCandidate{
			{
				Kind:   "github",
				Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
				Distributions: []config.Distribution{
					{When: &config.Condition{OS: []string{"linux"}}, Asset: config.AssetSelector{Template: "linux-only.tar.gz"}, HasAsset: true},
				},
			},
			{Kind: "fallback", Config: map[string]any{}},
		},
	}
}

func TestSelectionUsesEffectiveCandidate(t *testing.T) {
	tool := githubDistributionsTool()
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"}, &testMockAdapter{kindValue: "fallback"})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	selection := ex.resolveCandidateForSelection(context.Background(), tool, tool.Methods[0], "")
	if selection.err != nil {
		t.Fatalf("selection err = %v", selection.err)
	}
	if selection.method == tool.Methods[0] {
		t.Fatal("selection must use the specialized copy, not the declared candidate")
	}
	if selection.method.Config["asset"] != "linux-only.tar.gz" {
		t.Fatalf("effective asset = %v", selection.method.Config["asset"])
	}
	if tool.Methods[0].Config["asset"] != "base.tar.gz" {
		t.Fatal("declared candidate must stay untouched")
	}
}

func TestSelectionReportsIneligibleAndAmbiguous(t *testing.T) {
	tool := githubDistributionsTool()
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"}, &testMockAdapter{kindValue: "fallback"})(ex)
	WithFacts(&platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"})(ex)
	selection := ex.resolveCandidateForSelection(context.Background(), tool, tool.Methods[0], "")
	if selection.err == nil || selection.status != "skip_no_distribution" {
		t.Fatalf("selection = %+v, want skip_no_distribution", selection)
	}
	ambiguous := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"windows"}}},
			{When: &config.Condition{Arch: []string{"x86_64"}}},
		},
	}
	selection = ex.resolveCandidateForSelection(context.Background(), &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{ambiguous}}, ambiguous, "")
	if selection.err == nil || selection.status != "failed" {
		t.Fatalf("selection = %+v, want terminal failure", selection)
	}
	var ambiguousErr *config.AmbiguousDistributionsError
	if !errors.As(selection.err, &ambiguousErr) {
		t.Fatalf("err = %v, want typed ambiguity", selection.err)
	}
}

func TestResolveLockCandidateAbortsOnAmbiguity(t *testing.T) {
	ambiguous := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"linux"}}},
			{When: &config.Condition{Arch: []string{"x86_64"}}},
		},
	}
	fallback := &config.MethodCandidate{Kind: "cargo", Label: "fallback", Config: map[string]any{"pkg": "fallback-pkg"}}
	adapter := &lockCandidateAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "github"}}}
	fallbackAdapter := &lockCandidateAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}}}
	ex := newLockCandidateExecutor(&run.FakeRunner{}, adapter, fallbackAdapter)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	tool := &config.Tool{Name: "demo", MethodPrefer: []string{"github"}, Methods: []*config.MethodCandidate{ambiguous, fallback}}
	if _, err := ex.ResolveLockCandidate(context.Background(), tool, ""); err == nil {
		t.Fatal("expected immediate ambiguity error, got fallback success")
	} else if !strings.Contains(err.Error(), "distributions match") {
		t.Fatalf("err = %v", err)
	}
}

func TestExplainToolShowsEffectiveCandidate(t *testing.T) {
	tool := githubDistributionsTool()
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"}, &testMockAdapter{kindValue: "fallback"})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	attempts := ex.ExplainTool(context.Background(), tool, "")
	var github *MethodAttempt
	for i := range attempts {
		if attempts[i].Kind == "github" {
			github = &attempts[i]
		}
	}
	if github == nil {
		t.Fatalf("attempts = %+v, want github attempt", attempts)
	}
	if github.PlanIntent == nil {
		t.Fatalf("github PlanIntent = %+v, want specialized intent", github.PlanIntent)
	}
	found := false
	for _, op := range github.PlanIntent.Operations {
		if op.Kind == "resolve-artifact" && strings.Contains(op.Description, "linux-only.tar.gz") {
			found = true
		}
	}
	if !found {
		t.Fatalf("github PlanIntent operations = %+v, want specialized asset", github.PlanIntent.Operations)
	}
}

func TestCheckDesiredStateRecordsAmbiguityBroken(t *testing.T) {
	ambiguous := &config.MethodCandidate{
		Kind:   "github",
		Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
		Distributions: []config.Distribution{
			{When: &config.Condition{OS: []string{"linux"}}},
			{When: &config.Condition{Arch: []string{"x86_64"}}},
		},
	}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{ambiguous}}
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	checked, err := ex.CheckDesiredState(context.Background(), tool, "", true)
	if err != nil {
		t.Fatalf("CheckDesiredState error = %v", err)
	}
	if checked.Verification.State != plan.StateBroken || !strings.Contains(checked.Verification.Detail, "distributions match") {
		t.Fatalf("checked = %+v, want broken ambiguity", checked)
	}
}

type assetCapturingAdapter struct {
	testMockAdapter
	installed []map[string]any
}

func (a *assetCapturingAdapter) InstallResolved(_ context.Context, _ run.Runner, _ *config.Tool, mc *config.MethodCandidate, _ *plan.ResolvedInstallPlan) error {
	a.installed = append(a.installed, mc.Config)
	return nil
}

func TestExecutorInstallReceivesSpecializedCandidate(t *testing.T) {
	github := &assetCapturingAdapter{testMockAdapter: testMockAdapter{kindValue: "github"}}
	ex := New()
	WithAdapters(github, &testMockAdapter{kindValue: "fallback"})(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	tool := githubDistributionsTool()
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"github", "fallback"}},
		Tools:    map[string]*config.Tool{"tool": tool},
	}
	report, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.Tools[0].Method != "github" {
		t.Fatalf("result = %+v, want github install", report.Tools[0])
	}
	if len(github.installed) != 1 || github.installed[0]["asset"] != "linux-only.tar.gz" {
		t.Fatalf("installed Config = %v, want specialized asset", github.installed)
	}
	if tool.Methods[0].Config["asset"] != "base.tar.gz" {
		t.Fatalf("declared candidate mutated: %v", tool.Methods[0].Config["asset"])
	}
}

func TestExecutorInstallsSpecializedCandidate(t *testing.T) {
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"github", "fallback"}},
		Tools: map[string]*config.Tool{"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{
				{
					Kind:   "github",
					Config: map[string]any{"repo": "owner/repo", "asset": "base.tar.gz"},
					Distributions: []config.Distribution{
						{When: &config.Condition{OS: []string{"linux"}}, Asset: config.AssetSelector{Template: "linux-only.tar.gz"}, HasAsset: true},
					},
				},
				{Kind: "fallback", Config: map[string]any{}},
			},
		}},
	}
	ex := New()
	WithAdapters(&testMockAdapter{kindValue: "github"}, &testMockAdapter{kindValue: "fallback"})(ex)
	WithDryRun()(ex)
	WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(ex)
	report, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := report.Tools[0]
	if got.Method != "github" {
		t.Fatalf("result = %+v, want specialized github to win", got)
	}
}
