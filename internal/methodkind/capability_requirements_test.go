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

func TestHTTPSecretRequirementUsesSupportedSharedAuthTransport(t *testing.T) {
	for _, kind := range []string{"http", "appimage", "android", "msi"} {
		t.Run(kind, func(t *testing.T) {
			p := plan.New("private-tool", kind, true)
			p.Artifacts = []plan.Artifact{{URL: "https://example.test/private.tar.gz"}}
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
			contract, ok := Lookup(kind)
			if !ok {
				t.Fatalf("%s contract missing", kind)
			}
			if err := contract.CheckRequirements(p, CandidateRequirements{}); err != nil {
				t.Fatalf("CheckRequirements() = %v, want supported authenticated HTTP transport", err)
			}
		})
	}
}

func TestWrapperSecretRequirementSupportsDirectAndResolvedAssetDownloads(t *testing.T) {
	for _, kind := range []string{"appimage", "android", "msi"} {
		for _, source := range []string{"direct", "repo+asset"} {
			t.Run(kind+"/"+source, func(t *testing.T) {
				p := plan.New("private-tool", kind, true)
				if source == "direct" {
					p.Artifacts = []plan.Artifact{{URL: "https://example.test/private.pkg"}}
				} else {
					p.Operations = []plan.Operation{{Kind: "resolve-artifact", Effect: plan.EffectReadOnly}}
				}
				p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
				contract, _ := Lookup(kind)
				if err := contract.CheckRequirements(p, CandidateRequirements{}); err != nil {
					t.Fatalf("CheckRequirements() = %v", err)
				}
			})
		}
	}
}

func TestGitHubSecretRequirementUsesOnlyGitHubArtifactTransport(t *testing.T) {
	p := plan.New("private-tool", "github", true)
	p.Operations = []plan.Operation{{Kind: "resolve-artifact", Effect: plan.EffectReadOnly}}
	p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
	contract, ok := Lookup("github")
	if !ok {
		t.Fatal("github contract missing")
	}
	if err := contract.CheckRequirements(p, CandidateRequirements{}); err != nil {
		t.Fatalf("CheckRequirements() = %v, want GitHub artifact auth transport accepted", err)
	}
}
