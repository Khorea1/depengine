package validate

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
)

func TestValidatePlanIntentsRejectsUnsupportedVersionSemantics(t *testing.T) {
	s := schemaWithMethod("native", map[string]any{"pkg": "demo", "version": "1.2.3"})
	r := validatePlanIntents(s)
	if !r.HasErrors() || !strings.Contains(r.Errors[0].Message, "exact-version") {
		t.Fatalf("result = %+v, want exact-version capability error", r)
	}
}

func TestValidatePlanIntentsExplainsCapabilityClass(t *testing.T) {
	s := schemaWithMethod("native", map[string]any{"pkg": "demo", "version": "1.2.3"})
	r := validatePlanIntents(s)
	if !r.HasErrors() {
		t.Fatalf("result = %+v, want capability error", r)
	}
	if !strings.Contains(r.Errors[0].Message, string(plan.ErrorUnsupportedCapability)) {
		t.Fatalf("message = %q, want stable error class", r.Errors[0].Message)
	}
	if !strings.Contains(r.Errors[0].Message, "exact-version") {
		t.Fatalf("message = %q, want stable missing-capability name", r.Errors[0].Message)
	}
}

func TestValidatePlanIntentsUsesSharedPlanningError(t *testing.T) {
	s := schemaWithMethod("native", map[string]any{"pkg": "demo", "version": "1.2.3"})
	method := s.Tools["demo"].Methods[0]
	_, err := planner.BuildValidatedCandidateIntent(s.Tools["demo"], method, methodkind.CandidateRequirements{})
	if err == nil {
		t.Fatal("BuildValidatedCandidateIntent() = nil, want capability error")
	}
	r := validatePlanIntents(s)
	if !r.HasErrors() || r.Errors[0].Message != err.Error() {
		t.Fatalf("validation error = %+v, want shared planning error %q", r.Errors, err.Error())
	}
}

func TestValidateSchemaRedactsMalformedCargoGitCredentials(t *testing.T) {
	const secret = "redaction-sentinel"
	s := schemaWithMethod("cargo", map[string]any{
		"pkg": "demo",
		"git": "https://user:" + secret + "@%zz.example/repo.git",
	})
	r := ValidateSchema(s, nil)
	if !r.HasErrors() {
		t.Fatal("ValidateSchema() accepted malformed credential-bearing cargo.git source")
	}
	for _, finding := range r.Errors {
		if strings.Contains(finding.Message, secret) {
			t.Fatalf("ValidateSchema() leaked credential in finding: %+v", finding)
		}
	}
}

func TestValidatePlanIntentsAcceptsOverloadedSelectorsByContract(t *testing.T) {
	for name, method := range map[string]*config.MethodCandidate{
		"container-tag": {Kind: "container", Config: map[string]any{"manager": "podman", "source": "org/demo", "tag": "edge"}},
		"github-branch": {Kind: "github", Config: map[string]any{"repo": "org/demo", "asset": "demo.tar.gz", "branch": "edge"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := &config.Schema{Tools: map[string]*config.Tool{"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}}}}
			if r := validatePlanIntents(s); r.HasErrors() {
				t.Fatalf("validatePlanIntents() = %+v", r.Errors)
			}
		})
	}
}

func schemaWithMethod(kind string, cfg map[string]any) *config.Schema {
	method := &config.MethodCandidate{Kind: kind, Config: cfg}
	return &config.Schema{Tools: map[string]*config.Tool{"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}}}}
}

func TestValidateSchemaRunsPlanIntentChecks(t *testing.T) {
	s := schemaWithMethod("native", map[string]any{"pkg": "demo", "version": "1.2.3"})
	r := ValidateSchema(s, nil)
	for _, e := range r.Errors {
		if strings.Contains(e.Message, "exact-version") {
			return
		}
	}
	t.Fatalf("ValidateSchema() = %+v, want exact-version capability error", r.Errors)
}

func TestValidateSchemaOrdersFindingsDeterministically(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{}}
	for _, name := range []string{"delta", "alpha", "charlie", "bravo", "echo", "foxtrot"} {
		s.Tools[name] = &config.Tool{Name: name, Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": name, "version": "1.0"}},
		}}
	}
	first := ValidateSchema(s, nil).Errors
	for i := 0; i < 25; i++ {
		got := ValidateSchema(s, nil).Errors
		if len(got) != len(first) {
			t.Fatalf("finding count changed: %d vs %d", len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d finding %d = %v, want %v", i, j, got[j], first[j])
			}
		}
	}
	for j := 1; j < len(first); j++ {
		if first[j-1].Field > first[j].Field {
			t.Fatalf("findings not sorted by field: %q before %q", first[j-1].Field, first[j].Field)
		}
	}
}
