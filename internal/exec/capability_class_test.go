package exec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

// Production planning must consume the typed adapter-neutral capability
// boundary (Contract.CheckRequirements), so capability mismatches — including
// downloader auth requirements once secret references are schema-wired — fail
// with a stable error class instead of a plain string.
func TestCandidatePlanIntentReturnsTypedCapabilityError(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}
	_, err := CandidatePlanIntent(tool, method)
	if err == nil {
		t.Fatal("CandidatePlanIntent() = nil, want capability mismatch")
	}
	var plannerErr *plan.PlannerError
	if !errors.As(err, &plannerErr) {
		t.Fatalf("CandidatePlanIntent() error = %T %v, want *plan.PlannerError", err, err)
	}
	if !plan.IsClass(err, plan.ErrorUnsupportedCapability) {
		t.Fatalf("error class = %v, want unsupported_capability", err)
	}
	if !strings.Contains(err.Error(), "exact-version") {
		t.Fatalf("error = %q, want stable missing-capability name", err)
	}
}

func TestCandidatePlanIntentUsesSharedPlanningError(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}
	_, want := planner.BuildValidatedCandidateIntent(tool, method, methodkind.CandidateRequirements{})
	_, got := CandidatePlanIntent(tool, method)
	if want == nil || got == nil || got.Error() != want.Error() {
		t.Fatalf("CandidatePlanIntent() error = %v, want shared planning error %v", got, want)
	}
}

func TestExplainToolCapabilitySkipCarriesErrorClass(t *testing.T) {
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(&testMockAdapter{kindValue: "native", availableFunc: func() bool { return true }})(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "native", Config: map[string]any{"pkg": "demo", "version": "1.2.3"},
	}}}
	attempts := ex.ExplainTool(context.Background(), tool, "unknown")
	if len(attempts) != 1 || attempts[0].Status != "skip_capability" {
		t.Fatalf("attempts = %+v, want one skip_capability", attempts)
	}
	if !strings.Contains(attempts[0].Error, string(plan.ErrorUnsupportedCapability)) {
		t.Fatalf("reason = %q, want stable error class", attempts[0].Error)
	}
}
