package exec

// Tests for the distribution-specialization gate in the attempt pipeline
// (proposal E, 8.3): ineligible distributions fall through to the next
// method, ambiguity aborts the tool without silent fallback.

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
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
