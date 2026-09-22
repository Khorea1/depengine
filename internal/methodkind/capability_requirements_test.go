package methodkind

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestCheckRequirementsClassifiesAuthMismatch(t *testing.T) {
	p := plan.New("private-tool", "http", true)
	p.Sources = []plan.SourceReference{{
		Role:      plan.SourceSelection,
		Name:      "private",
		SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"},
	}}
	contract := Contract{Kind: "http", Capabilities: CapabilitySourceSelection}
	err := contract.CheckRequirements(p, CandidateRequirements{})
	if err == nil {
		t.Fatal("expected auth capability mismatch")
	}
	if !plan.IsClass(err, plan.ErrorAuthRequirement) {
		t.Fatalf("error class = %v, want auth_requirement", err)
	}
	if !strings.Contains(err.Error(), "auth") || !strings.Contains(err.Error(), "http") {
		t.Fatalf("error = %q, want method and auth capability", err)
	}
}

func TestCheckRequirementsClassifiesNonAuthMismatch(t *testing.T) {
	p := plan.New("scoped-tool", "native", true)
	p.Identity.Scope = string(plan.ScopeUser)
	contract := Contract{Kind: "native"}
	err := contract.CheckRequirements(p, CandidateRequirements{})
	if err == nil {
		t.Fatal("expected capability mismatch")
	}
	if !plan.IsClass(err, plan.ErrorUnsupportedCapability) {
		t.Fatalf("error class = %v, want unsupported_capability", err)
	}
	if !strings.Contains(err.Error(), "scope") {
		t.Fatalf("error = %q, want missing scope capability", err)
	}
}
