package methodkind

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestPlanCapabilitiesFailClosedForEverySemanticDimension(t *testing.T) {
	versionCases := []struct {
		name string
		in   plan.VersionIntent
		want Capability
	}{
		{"exact", plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}, CapabilityExactVersion},
		{"constraint", plan.VersionIntent{Mode: plan.VersionConstraint, Value: ">=1.2"}, CapabilityVersionConstraint},
		{"channel", plan.VersionIntent{Mode: plan.VersionChannel, Channel: &plan.ChannelSelector{Name: "stable"}}, CapabilityChannel},
		{"git-tag", plan.VersionIntent{Mode: plan.VersionGitTag, Value: "v1.2.3"}, CapabilityRevision},
		{"git-branch", plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"}, CapabilityRevision},
		{"git-revision", plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "deadbeef"}, CapabilityRevision},
		{"container-tag", plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "latest"}, CapabilityMutableTag},
		{"digest", plan.VersionIntent{Mode: plan.VersionDigest, Value: "sha256:" + strings.Repeat("a", 64)}, CapabilityImmutableIdentity},
	}
	for _, tt := range versionCases {
		t.Run("version/"+tt.name, func(t *testing.T) {
			p := plan.New("demo", "test", true)
			p.Identity.RequestedVersion = &tt.in
			assertRequiredAndMissing(t, p, tt.want)
		})
	}

	cases := []struct {
		name   string
		mutate func(*plan.ResolvedInstallPlan)
		want   Capability
	}{
		{"scope", func(p *plan.ResolvedInstallPlan) { p.Identity.Scope = "user" }, CapabilityScope},
		{"environment", func(p *plan.ResolvedInstallPlan) {
			p.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}
		}, CapabilityEnvironmentTarget},
		{"architecture", func(p *plan.ResolvedInstallPlan) { p.Identity.Architecture = "amd64" }, CapabilityArchitecture},
		{"platform", func(p *plan.ResolvedInstallPlan) { p.Identity.Platform = "linux" }, CapabilityArchitecture},
		{"local-artifact", func(p *plan.ResolvedInstallPlan) {
			p.Artifacts = []plan.Artifact{{Kind: plan.ArtifactRaw, LocalPath: "vendor/tool"}}
		}, CapabilityLocalArtifact},
		{"source-selection", func(p *plan.ResolvedInstallPlan) {
			p.Sources = []plan.SourceReference{{Role: plan.SourceSelection, Name: "corp"}}
		}, CapabilitySourceSelection},
		{"source-mutation", func(p *plan.ResolvedInstallPlan) {
			p.Sources = []plan.SourceReference{{Role: plan.SourceHostConfiguration, Name: "corp"}}
		}, CapabilitySourceSelection | CapabilitySourceMutation},
		{"source-trust", func(p *plan.ResolvedInstallPlan) {
			p.Sources = []plan.SourceReference{{Role: plan.SourceSelection, Name: "corp", Trust: &plan.SourceTrust{Fingerprint: "ABCD"}}}
		}, CapabilitySourceSelection | CapabilitySourceTrust},
		{"source-auth", func(p *plan.ResolvedInstallPlan) {
			p.Sources = []plan.SourceReference{{Role: plan.SourceSelection, Name: "corp", SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"}}}
		}, CapabilitySourceSelection | CapabilityAuth},
		{"secret-requirement", func(p *plan.ResolvedInstallPlan) {
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
		}, CapabilityAuth},
		{"arbitrary-code", func(p *plan.ResolvedInstallPlan) {
			p.Operations = []plan.Operation{{Kind: "build", Effect: plan.EffectMutation, Command: []string{"make"}, ArbitraryCode: true}}
		}, CapabilityArbitraryCode},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.New("demo", "test", true)
			tt.mutate(&p)
			assertRequiredAndMissing(t, p, tt.want)
		})
	}
}

func TestMissingPlanCapabilitiesPreservesCombinedRequirements(t *testing.T) {
	p := plan.New("demo", "test", true)
	exact := plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}
	p.Identity.RequestedVersion = &exact
	p.Identity.Scope = "user"
	p.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}
	p.Identity.Architecture = "amd64"
	p.Artifacts = []plan.Artifact{{Kind: plan.ArtifactArchive, LocalPath: "vendor/tool.tar.gz"}}
	p.Sources = []plan.SourceReference{{
		Role:      plan.SourceHostConfiguration,
		Name:      "corp",
		Trust:     &plan.SourceTrust{Fingerprint: "ABCD"},
		SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"},
	}}
	p.Operations = []plan.Operation{{Kind: "build", Effect: plan.EffectMutation, Command: []string{"make"}, ArbitraryCode: true}}

	want := CapabilityExactVersion |
		CapabilityScope |
		CapabilityEnvironmentTarget |
		CapabilityArchitecture |
		CapabilityLocalArtifact |
		CapabilitySourceSelection |
		CapabilitySourceMutation |
		CapabilitySourceTrust |
		CapabilityAuth |
		CapabilityArbitraryCode
	assertRequiredAndMissing(t, p, want)

	partial := Contract{Capabilities: CapabilityExactVersion | CapabilityScope | CapabilitySourceSelection}
	missing, err := partial.MissingPlanCapabilities(p)
	if err != nil {
		t.Fatalf("MissingPlanCapabilities() error: %v", err)
	}
	if got, wantMissing := missing, want&^partial.Capabilities; got != wantMissing {
		t.Fatalf("missing = %v, want %v", CapabilityNames(got), CapabilityNames(wantMissing))
	}
}

func TestSharedAuthRecognitionRemainsNarrow(t *testing.T) {
	t.Run("existing source transport", func(t *testing.T) {
		p := plan.New("demo", "native", true)
		p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
		p.Sources = []plan.SourceReference{{Role: plan.SourceHostConfiguration, Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"}}}
		missing, err := (Contract{Kind: "native"}).MissingPlanCapabilities(p)
		if err != nil || missing&CapabilityAuth != 0 {
			t.Fatalf("missing = %v, error = %v; want existing source auth accepted", CapabilityNames(missing), err)
		}
	})
	cases := []struct {
		name   string
		method string
		edit   func(*plan.ResolvedInstallPlan)
	}{
		{"orphan reference", "http", func(p *plan.ResolvedInstallPlan) {
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
		}},
		{"non-http artifact", "github", func(p *plan.ResolvedInstallPlan) {
			p.Artifacts = []plan.Artifact{{URL: "https://example.test/file.tar.gz"}}
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
		}},
		{"HTTP plus source auth", "http", func(p *plan.ResolvedInstallPlan) {
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
			p.Sources = []plan.SourceReference{{Role: plan.SourceHostConfiguration, Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"}}}
		}},
		{"unsupported source role", "native", func(p *plan.ResolvedInstallPlan) {
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
			p.Sources = []plan.SourceReference{{Role: plan.SourceRegistry, Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"}}}
		}},
		{"unsupported source kind", "native", func(p *plan.ResolvedInstallPlan) {
			p.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
			p.Sources = []plan.SourceReference{{Role: plan.SourceHostConfiguration, Kind: "apt-ppa", Name: "ppa:vendor/stable", SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"}}}
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.New("demo", tt.method, true)
			tt.edit(&p)
			contract := Contract{Kind: tt.method}
			missing, err := contract.MissingPlanCapabilities(p)
			if err != nil {
				t.Fatalf("MissingPlanCapabilities() error = %v", err)
			}
			if missing&CapabilityAuth == 0 {
				t.Fatalf("missing = %v, want auth", CapabilityNames(missing))
			}
		})
	}
}

func assertRequiredAndMissing(t *testing.T, p plan.ResolvedInstallPlan, want Capability) {
	t.Helper()
	got, err := PlanCapabilities(p)
	if err != nil {
		t.Fatalf("PlanCapabilities() error: %v", err)
	}
	if got != want {
		t.Fatalf("required = %v, want %v", CapabilityNames(got), CapabilityNames(want))
	}
	missing, err := (Contract{}).MissingPlanCapabilities(p)
	if err != nil {
		t.Fatalf("MissingPlanCapabilities() error: %v", err)
	}
	if missing != want {
		t.Fatalf("missing = %v, want %v", CapabilityNames(missing), CapabilityNames(want))
	}
}
