package planner_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
)

func TestBuildCandidateIntentRejectsUnknownField(t *testing.T) {
	tool, method := candidate("demo", "native", map[string]any{"pkg": "demo", "mystery": true})
	_, err := planner.BuildCandidateIntent(tool, method)
	if err == nil || !plan.IsClass(err, plan.ErrorInvalidManifest) || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("error = %v, want invalid-manifest unknown field", err)
	}
}

func TestBuildCandidateIntentNormalizesPortableScope(t *testing.T) {
	tool, method := candidate("demo", "winget", map[string]any{"pkg": "Demo.Tool", "scope": "machine"})
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	if p.Identity.Package != "Demo.Tool" || p.Identity.Scope != string(plan.ScopeSystem) {
		t.Fatalf("identity = %+v", p.Identity)
	}
}

func TestBuildValidatedCandidateIntentPreservesTypedCapabilityError(t *testing.T) {
	tool, method := candidate("demo", "native", map[string]any{"pkg": "demo", "version": "1.2.3"})
	p, err := planner.BuildValidatedCandidateIntent(tool, method, methodkind.CandidateRequirements{})
	if err == nil || p == nil {
		t.Fatalf("BuildValidatedCandidateIntent() = (%v, %v), want populated intent and capability error", p, err)
	}
	var plannerErr *plan.PlannerError
	if !errors.As(err, &plannerErr) || !plan.IsClass(err, plan.ErrorUnsupportedCapability) {
		t.Fatalf("error = %T %v, want typed unsupported-capability planner error", err, err)
	}
	if !strings.Contains(err.Error(), "exact-version") {
		t.Fatalf("error = %q, want exact-version capability", err)
	}
}
