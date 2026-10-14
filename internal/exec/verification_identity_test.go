package exec

import (
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
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
			got := projectVerificationIdentity(&config.MethodCandidate{Kind: tt.kind}, identity)
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

func TestProjectVerificationIdentityIncludesRevisionOnlyForRetainedGitCheckout(t *testing.T) {
	identity := plan.ResolvedIdentity{Revision: "revision"}
	for _, tt := range []struct {
		name   string
		config map[string]any
		want   string
	}{
		{name: "retained root checkout", config: map[string]any{"extract_to": "/opt/tool"}, want: "revision"},
		{name: "managed paths take precedence", config: map[string]any{"extract_to": "/opt/tool", "managed_paths": []any{"/opt/tool/bin/tool"}}},
		{name: "non-root artifact", config: map[string]any{"extract_to": "/opt/tool", "artifact": "build/output"}},
		{name: "managed path slice representation", config: map[string]any{"extract_to": "/opt/tool", "managed_paths": []string{"/opt/tool/bin/tool"}}},
		{name: "no retained output", config: map[string]any{"binary": "tool"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			method := &config.MethodCandidate{Kind: "git", Config: tt.config}
			got := projectVerificationIdentity(method, identity)
			if got.Revision != tt.want {
				t.Fatalf("projected revision = %q, want %q", got.Revision, tt.want)
			}
		})
	}
}

func TestProjectedGitCheckoutRevisionDriftIsActionable(t *testing.T) {
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{"extract_to": "/opt/tool"}}
	desired := projectVerificationIdentity(method, plan.ResolvedIdentity{Revision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	observed := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		KnownFields: []plan.IdentityField{plan.FieldRevision},
	}
	verification := plan.Reconcile(desired, observed)
	if verification.State != plan.StateDrifted {
		t.Fatalf("Reconcile() state = %q, want %q", verification.State, plan.StateDrifted)
	}
	decision, err := plan.TransitionForVerification(verification)
	if err != nil || decision.Transition != plan.TransitionUpgrade || !decision.Required {
		t.Fatalf("TransitionForVerification() = %+v, %v; want required upgrade", decision, err)
	}
}
