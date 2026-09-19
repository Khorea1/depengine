package plan_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestNewPlanAndMutationClassification(t *testing.T) {
	p := plan.New("ripgrep", "github", true)
	p.Operations = []plan.Operation{
		{Kind: "resolve-release", Effect: plan.EffectReadOnly},
		{Kind: "install-artifact", Effect: plan.EffectMutation},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if !p.HasMutations() {
		t.Fatal("HasMutations() = false, want true")
	}
}

func TestSourceMutationsMustBeMutating(t *testing.T) {
	p := plan.New("x", "native", true)
	p.SourceMutations = []plan.Operation{{Kind: "add-repo", Effect: plan.EffectReadOnly}}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() unexpectedly accepted read-only source mutation")
	}
}

func TestPlanJSONRedactsCredentials(t *testing.T) {
	p := plan.New("private", "github", true)
	p.Identity.Source = "https://user:password@example.test/repo"
	p.Artifacts = []plan.Artifact{{URL: "https://example.test/a?token=topsecret&x=1"}}
	p.Operations = []plan.Operation{{
		Kind:        "probe",
		Effect:      plan.EffectReadOnly,
		Description: "Authorization: Bearer secret-value",
		Command:     []string{"curl", "--api-key", "abc123"},
	}}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	got := string(data)
	for _, secret := range []string{"password", "topsecret", "secret-value", "abc123"} {
		if strings.Contains(got, secret) {
			t.Fatalf("serialized plan leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("serialized plan does not show redaction marker: %s", got)
	}
}

func TestPlannerErrorClassPreservesCause(t *testing.T) {
	cause := errors.New("network unavailable")
	err := &plan.PlannerError{Class: plan.ErrorResolutionFailure, Op: "resolve github release", Err: cause}
	if !plan.IsClass(err, plan.ErrorResolutionFailure) {
		t.Fatal("IsClass() = false")
	}
	if !errors.Is(err, cause) {
		t.Fatal("PlannerError does not unwrap cause")
	}
}

func TestValidateRejectsUnknownVersionAndEffect(t *testing.T) {
	p := plan.New("x", "http", true)
	p.Version++
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted unknown plan version")
	}

	p = plan.New("x", "http", true)
	p.Operations = []plan.Operation{{Kind: "mystery", Effect: "unknown"}}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted unknown operation effect")
	}
}

func TestPlanValidateRejectsNonCanonicalScope(t *testing.T) {
	for _, scope := range []string{"global", "machine", "default", " user"} {
		t.Run(scope, func(t *testing.T) {
			p := plan.New("tool", "method", true)
			p.Identity.Scope = scope
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate() accepted non-canonical scope %q", scope)
			}
		})
	}
}

func TestPlanValidateAcceptsPortableScope(t *testing.T) {
	for _, scope := range []string{"user", "system"} {
		t.Run(scope, func(t *testing.T) {
			p := plan.New("tool", "method", true)
			p.Identity.Scope = scope
			if err := p.Validate(); err != nil {
				t.Fatalf("Validate() rejected scope %q: %v", scope, err)
			}
		})
	}
}

func TestPlanMarshalRedactsPreparationOperations(t *testing.T) {
	p := plan.New("demo", "native", true)
	p.Preparation = &plan.PreparationPlan{
		Prepare: []plan.PreparationMutation{{
			ID:        "source",
			Resource:  plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"},
			Ownership: plan.OwnershipDepengine,
			Apply: plan.Operation{
				Kind:        "source-add",
				Effect:      plan.EffectMutation,
				Description: "https://example.test/repo?token=supersecret",
				Command:     []string{"tool", "--api-key", "supersecret"},
			},
			Rollback: &plan.Operation{Kind: "source-remove", Effect: plan.EffectMutation},
			Policy:   plan.RollbackSafe,
		}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "supersecret") {
		t.Fatalf("serialized plan leaked preparation secret: %s", b)
	}
}
