package plan_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
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

func TestPlanJSONRedactsSeparatedSensitiveFlagValues(t *testing.T) {
	p := plan.New("private", "git", true)
	p.Operations = []plan.Operation{{
		Kind:          "build",
		Effect:        plan.EffectMutation,
		ArbitraryCode: true,
		Command:       []string{"tool", "--client-secret", "client-value", "--refresh-token", "refresh-value"},
	}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"client-value", "refresh-value"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("serialized plan leaked %q: %s", secret, b)
		}
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

func TestPlanRejectsRemovalPathOutsideDeclaredOwnership(t *testing.T) {
	p := plan.New("demo", "test", true)
	p.OwnedPaths = []string{"/opt/depengine/demo"}
	p.Removal = plan.RemovalMetadata{
		Supported:  true,
		OwnedPaths: []string{"/opt/depengine/other"},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "not declared as owned") {
		t.Fatalf("Validate() error = %v, want undeclared ownership rejection", err)
	}
}

func TestPlanAcceptsRemovalPathsThatAreDeclaredOwned(t *testing.T) {
	p := plan.New("demo", "test", true)
	p.OwnedPaths = []string{"/opt/depengine/demo", "/usr/local/bin/demo"}
	p.Removal = plan.RemovalMetadata{
		Supported:  true,
		OwnedPaths: []string{"/usr/local/bin/demo", "/opt/depengine/demo"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestPlanRejectsDuplicateOwnershipPaths(t *testing.T) {
	p := plan.New("demo", "test", true)
	p.OwnedPaths = []string{"/opt/depengine/demo", "/opt/depengine/demo"}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate owned path") {
		t.Fatalf("Validate() error = %v, want duplicate ownership rejection", err)
	}

	p.OwnedPaths = []string{"/opt/depengine/demo"}
	p.Removal.Supported = true
	p.Removal.OwnedPaths = []string{"/opt/depengine/demo", "/opt/depengine/demo"}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate removal owned path") {
		t.Fatalf("Validate() error = %v, want duplicate removal ownership rejection", err)
	}
}

func TestPlanRejectsOwnershipPathAliases(t *testing.T) {
	tests := []struct {
		name  string
		owned []string
	}{
		{name: "unix non canonical", owned: []string{"/opt/depengine/../demo"}},
		{name: "unix duplicate alias", owned: []string{"/opt/demo", "/opt//demo"}},
		{name: "relative", owned: []string{"opt/demo"}},
		{name: "windows case separator alias", owned: []string{`C:\Tools\Demo`, `c:/tools/demo`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.New("demo", "test", true)
			p.OwnedPaths = tt.owned
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate() accepted ownership paths %#v", tt.owned)
			}
		})
	}
}

func TestPlanMatchesRemovalOwnershipAcrossWindowsPathSpelling(t *testing.T) {
	p := plan.New("demo", "test", true)
	p.OwnedPaths = []string{`C:\Tools\Demo`}
	p.Removal = plan.RemovalMetadata{Supported: true, OwnedPaths: []string{`c:/tools/demo`}}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() rejected equivalent Windows ownership spelling: %v", err)
	}
}

func TestPlanValidateRejectsCommandWithoutArbitraryCodeClassification(t *testing.T) {
	p := plan.New("demo", "git", true)
	p.Operations = []plan.Operation{{Kind: "build", Effect: plan.EffectMutation, Command: []string{"make"}}}
	if err := p.Validate(); err == nil {
		t.Fatal("command-bearing operation without arbitrary-code classification unexpectedly accepted")
	}

	p.Operations[0].ArbitraryCode = true
	if err := p.Validate(); err != nil {
		t.Fatalf("classified command-bearing operation rejected: %v", err)
	}
}

func TestPlanValidateRejectsMalformedStaticExecutionIdentity(t *testing.T) {
	tests := []struct {
		name string
		edit func(*plan.ResolvedInstallPlan)
	}{
		{name: "tool whitespace", edit: func(p *plan.ResolvedInstallPlan) { p.Tool.Name = " demo" }},
		{name: "method whitespace", edit: func(p *plan.ResolvedInstallPlan) { p.Candidate.Method = "git " }},
		{name: "empty operation kind", edit: func(p *plan.ResolvedInstallPlan) { p.Operations = []plan.Operation{{Effect: plan.EffectReadOnly}} }},
		{name: "empty executable", edit: func(p *plan.ResolvedInstallPlan) {
			p.Operations = []plan.Operation{{Kind: "build", Effect: plan.EffectMutation, Command: []string{" "}, ArbitraryCode: true}}
		}},
		{name: "nul argv", edit: func(p *plan.ResolvedInstallPlan) {
			p.Operations = []plan.Operation{{Kind: "build", Effect: plan.EffectMutation, Command: []string{"make", "x\x00y"}, ArbitraryCode: true}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.New("demo", "git", true)
			tt.edit(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("malformed static execution identity unexpectedly accepted")
			}
		})
	}
}

func TestPlanValidateRejectsMalformedOrDuplicatePrerequisites(t *testing.T) {
	tests := []struct {
		name string
		in   []plan.Prerequisite
	}{
		{name: "empty name", in: []plan.Prerequisite{{}}},
		{name: "name whitespace", in: []plan.Prerequisite{{Name: " compiler"}}},
		{name: "method whitespace", in: []plan.Prerequisite{{Name: "compiler", Method: "native "}}},
		{name: "nul name", in: []plan.Prerequisite{{Name: "compiler\x00x"}}},
		{name: "duplicate", in: []plan.Prerequisite{{Name: "compiler", Method: "native"}, {Name: "compiler", Method: "native"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.New("demo", "native", true)
			p.Prerequisites = tt.in
			if err := p.Validate(); err == nil {
				t.Fatal("invalid prerequisite set unexpectedly accepted")
			}
		})
	}

	p := plan.New("demo", "native", true)
	p.Prerequisites = []plan.Prerequisite{{Name: "compiler"}, {Name: "headers", Method: "native"}}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid prerequisites rejected: %v", err)
	}
}

func TestPlanValidateRejectsInvalidEntrypointsAndNULPaths(t *testing.T) {
	tests := []struct {
		name string
		edit func(*plan.ResolvedInstallPlan)
	}{
		{name: "empty entrypoint name", edit: func(p *plan.ResolvedInstallPlan) { p.Entrypoints = map[string]string{"": "/bin/demo"} }},
		{name: "entrypoint name whitespace", edit: func(p *plan.ResolvedInstallPlan) { p.Entrypoints = map[string]string{" demo": "/bin/demo"} }},
		{name: "empty entrypoint target", edit: func(p *plan.ResolvedInstallPlan) { p.Entrypoints = map[string]string{"demo": ""} }},
		{name: "entrypoint target nul", edit: func(p *plan.ResolvedInstallPlan) { p.Entrypoints = map[string]string{"demo": "/bin/de\x00mo"} }},
		{name: "owned path nul", edit: func(p *plan.ResolvedInstallPlan) { p.OwnedPaths = []string{"/opt/de\x00mo"} }},
		{name: "removal path nul", edit: func(p *plan.ResolvedInstallPlan) {
			p.OwnedPaths = []string{"/opt/de\x00mo"}
			p.Removal.OwnedPaths = []string{"/opt/de\x00mo"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.New("demo", "local", true)
			tt.edit(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("invalid entrypoint/path unexpectedly accepted")
			}
		})
	}
}

func TestPlanValidateRejectsDuplicateArtifactLocation(t *testing.T) {
	p := plan.New("demo", "http", true)
	p.Artifacts = []plan.Artifact{
		{Kind: plan.ArtifactArchive, URL: "https://example.test/tool.tar.gz", Checksum: "sha256:a"},
		{Kind: plan.ArtifactArchive, URL: "https://example.test/tool.tar.gz", Checksum: "sha256:b"},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate artifact location with conflicting identity")
	}
}

func TestPlanValidateRejectsCanonicalArtifactURLAliases(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Artifacts = []plan.Artifact{
		{Kind: plan.ArtifactArchive, URL: "HTTPS://EXAMPLE.TEST/tool.tar.gz?z=2&a=1", Checksum: "sha256:" + strings.Repeat("a", 64)},
		{Kind: plan.ArtifactArchive, URL: "https://example.test/tool.tar.gz?a=1&z=2", Checksum: "sha256:" + strings.Repeat("b", 64)},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate artifact location") {
		t.Fatalf("Validate() error = %v, want canonical duplicate rejection", err)
	}
}

func TestPlanRejectsRemovalMetadataWhenRemovalUnsupported(t *testing.T) {
	for _, removal := range []plan.RemovalMetadata{
		{Identity: "pkg:demo"},
		{OwnedPaths: []string{"/opt/depengine/demo"}},
	} {
		p := plan.New("demo", "test", true)
		p.OwnedPaths = []string{"/opt/depengine/demo"}
		p.Removal = removal
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported removal") {
			t.Fatalf("Validate() error = %v, want unsupported-removal metadata rejection", err)
		}
	}
}

func TestPlannerErrorDisplayRedactsSecretsWithoutLosingCause(t *testing.T) {
	cause := fmt.Errorf("fetch https://tok_123@example.test/tool?access_token=qry_456 failed")
	err := &plan.PlannerError{Class: plan.ErrorResolutionFailure, Op: "resolve --client-secret argv_789", Err: cause}
	got := err.Error()
	for _, secret := range []string{"tok_123", "qry_456", "argv_789"} {
		if strings.Contains(got, secret) {
			t.Fatalf("PlannerError.Error() leaked %q: %s", secret, got)
		}
	}
	if !errors.Is(err, cause) {
		t.Fatal("PlannerError lost wrapped cause")
	}
}

func TestPlanRejectsDuplicateSecretReferences(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Secrets = []plan.SecretReference{
		{Provider: "env", Name: "TOKEN"},
		{Provider: "env", Name: "TOKEN"},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate secret reference") {
		t.Fatalf("Validate() error = %v, want duplicate secret reference", err)
	}
}
