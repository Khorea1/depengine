package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestExplainToolRetainsExactDeclaredCandidateOrdinal(t *testing.T) {
	first := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "first"}}
	second := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "second"}}
	tool := &config.Tool{
		Name:    "demo",
		Methods: []*config.MethodCandidate{first, second},
	}

	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(&testMockAdapter{kindValue: "cargo"})(ex)
	WithDefaultMethodOrder([]string{"cargo"})(ex)

	attempts := ex.ExplainTool(context.Background(), tool, "unknown")
	if len(attempts) != 2 {
		t.Fatalf("attempts = %+v, want two declared candidates", attempts)
	}
	for i, attempt := range attempts {
		if !attempt.CandidateKnown || attempt.Candidate != i {
			t.Fatalf("attempt %d candidate identity = (%d, %v), want (%d, true): %+v", i, attempt.Candidate, attempt.CandidateKnown, i, attempt)
		}
	}
}

func TestDeclaredCandidateOrdinalRejectsSynthesizedCandidate(t *testing.T) {
	declared := &config.MethodCandidate{Kind: "cargo"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{declared}}
	synthesized := &config.MethodCandidate{Kind: "cargo"}

	if candidate, ok := declaredCandidateOrdinal(tool, synthesized); ok {
		t.Fatalf("synthesized candidate unexpectedly matched ordinal %d", candidate)
	}
}
