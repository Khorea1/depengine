package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestExplainToolCarriesStaticPlanIntent(t *testing.T) {
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(&testMockAdapter{kindValue: "container", availableFunc: func() bool { return true }})(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "container", Config: map[string]any{"manager": "podman", "source": "org/demo", "tag": "edge"},
	}}}
	attempts := ex.ExplainTool(context.Background(), tool, "unknown")
	if len(attempts) != 1 || attempts[0].PlanIntent == nil {
		t.Fatalf("attempts = %+v, want plan intent", attempts)
	}
	got := attempts[0].PlanIntent.Identity.RequestedVersion
	if got == nil || got.Mode != plan.VersionContainerTag || got.Value != "edge" {
		t.Fatalf("requested version = %+v", got)
	}
}

// A candidate the planner rejects must still trip the arbitrary-code gate
// when it carries a command field.
func TestArbitraryCodeGateFailsClosedWhenPlanningRejectsCandidate(t *testing.T) {
	ex := New()
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "git", Config: map[string]any{
			"url": "https://example.test/demo.git", "build": []any{"make"}, "no_such_field": "x",
		},
	}}}
	if !ex.hasDangerousMethod(tool) {
		t.Fatal("candidate with a command field and an unplannable extra field bypassed the gate")
	}
}

func TestExplainToolSkipsCandidateWithUnsupportedSelector(t *testing.T) {
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(&testMockAdapter{kindValue: "go", availableFunc: func() bool { return true }})(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "go", Config: map[string]any{"pkg": "example.test/demo", "tag": "v1"},
	}}}
	attempts := ex.ExplainTool(context.Background(), tool, "unknown")
	if len(attempts) == 0 || attempts[0].Status != "skip_capability" {
		t.Fatalf("attempts = %+v, want skip_capability for unsupported tag", attempts)
	}
}
