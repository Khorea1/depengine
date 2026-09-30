package exec

import (
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestProjectVerificationIdentityFollowsContractEffects(t *testing.T) {
	exact := &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}
	identity := plan.ResolvedIdentity{
		Package:          "demo",
		RequestedVersion: exact,
		Version:          "1.2.3",
		Revision:         "rev-1",
		Digest:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Source:           "https://example.test/source",
		Registry:         "https://registry.example.test",
		Scope:            string(plan.ScopeSystem),
		Environment:      &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: "/opt/demo"},
		Architecture:     "amd64",
		Platform:         "linux/amd64",
	}

	tests := []struct {
		kind string
		want plan.ResolvedIdentity
	}{
		{
			kind: "cargo",
			want: plan.ResolvedIdentity{
				Package:          "demo",
				RequestedVersion: &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"},
				Version:          "1.2.3",
				Environment:      &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: "/opt/demo"},
			},
		},
		{
			kind: "npm",
			want: plan.ResolvedIdentity{
				Package:          "demo",
				RequestedVersion: &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"},
				Version:          "1.2.3",
			},
		},
		{
			kind: "winget",
			want: plan.ResolvedIdentity{
				Package:          "demo",
				RequestedVersion: &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"},
				Version:          "1.2.3",
				Source:           "https://example.test/source",
			},
		},
		{
			kind: "conda",
			want: plan.ResolvedIdentity{
				Package:          "demo",
				RequestedVersion: &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"},
				Version:          "1.2.3",
				Revision:         "rev-1",
				Environment:      &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: "/opt/demo"},
			},
		},
		{
			kind: "container",
			want: plan.ResolvedIdentity{
				Digest:   identity.Digest,
				Source:   identity.Source,
				Platform: identity.Platform,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := projectVerificationIdentity(tt.kind, identity)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("projectVerificationIdentity(%q) = %#v, want %#v", tt.kind, got, tt.want)
			}
			if got.RequestedVersion != nil && got.RequestedVersion == identity.RequestedVersion {
				t.Fatal("projection aliases RequestedVersion")
			}
			if got.Environment != nil && got.Environment == identity.Environment {
				t.Fatal("projection aliases Environment")
			}
		})
	}
}
