package exec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type verificationAdapter struct {
	executorAdapterV2Double
	seen        *config.MethodCandidate
	observation plan.Observation
}

func (a *verificationAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	a.seen = method
	return a.observation, a.observeErr
}

func TestVerifyResolvedCandidateReconcilesResolvedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation plan.Observation
		want        plan.VerificationState
	}{
		{"satisfied", plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "demo"}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, plan.StateSatisfied},
		{"drifted", plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "other"}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, plan.StateDrifted},
		{"absent", plan.Observation{Presence: plan.PresenceAbsent}, plan.StateAbsent},
		{"unknown", plan.Observation{Presence: plan.PresenceUnknown}, plan.StateUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &verificationAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}}}
			adapter.observation = tc.observation
			ex := New()
			WithAdapters(adapter)(ex)
			tool := &config.Tool{Name: "demo"}
			method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}}
			resolved := plan.New("demo", "cargo", true)
			resolved.Identity.Package = "demo"
			got, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
			if err != nil || got.State != tc.want {
				t.Fatalf("verification=%+v err=%v, want %s", got, err, tc.want)
			}
		})
	}
}

func TestVerifyResolvedCandidateIgnoresExecuteOnlyIdentityDimensions(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		observation: plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "demo"},
			KnownFields: []plan.IdentityField{plan.FieldPackage},
		},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo", "git": "https://example.test/demo.git"}}
	resolved := plan.New("demo", "cargo", true)
	resolved.Identity.Package = "demo"
	resolved.Identity.Source = "https://example.test/demo.git"

	verification, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
	if err != nil {
		t.Fatalf("VerifyResolvedCandidate: %v", err)
	}
	if verification.State != plan.StateSatisfied {
		t.Fatalf("verification = %+v, want satisfied without unverifiable cargo.git identity", verification)
	}
}

func TestVerifyResolvedCandidateKeepsDeclaredVerificationDimensions(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "winget"}},
		observation: plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "Demo.Tool"},
			KnownFields: []plan.IdentityField{plan.FieldPackage},
		},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "winget", Config: map[string]any{"pkg": "Demo.Tool", "source": "winget"}}
	resolved := plan.New("demo", "winget", true)
	resolved.Identity.Package = "Demo.Tool"
	resolved.Identity.Source = "winget"

	verification, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
	if err != nil {
		t.Fatalf("VerifyResolvedCandidate: %v", err)
	}
	if verification.State != plan.StateUnknown || len(verification.Unverifiable) != 1 || verification.Unverifiable[0] != plan.FieldSource {
		t.Fatalf("verification = %+v, want unverifiable declared source identity", verification)
	}
}

func TestResolveAndVerifyCandidateAtVersionProjectsDesiredIdentity(t *testing.T) {
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{
			testMockAdapter: testMockAdapter{kindValue: "cargo"},
		},
		observation: plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "demo", Version: "v2.0.0"},
			KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
		},
	}
	ex := New()
	WithAdapters(adapter)(ex)
	WithRunner(&run.FakeRunner{})(ex)
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "demo"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}

	resolved, verification, err := ex.ResolveAndVerifyCandidateAtVersion(context.Background(), tool, method, "v2.0.0")
	if err != nil {
		t.Fatalf("ResolveAndVerifyCandidateAtVersion: %v", err)
	}
	if resolved == nil || resolved.Identity.Version != "v2.0.0" {
		t.Fatalf("resolved=%+v, want projected version v2.0.0", resolved)
	}
	if verification.State != plan.StateSatisfied {
		t.Fatalf("verification=%+v, want satisfied", verification)
	}
}

func TestVerifyResolvedCandidateUsesResolvedTargetAndCanonicalizesObserveError(t *testing.T) {
	adapter := &verificationAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "conda"}, observeErr: errors.New("probe failed at https://user:pass@example.test/?token=secret")}}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "demo", "environment": "old"}}
	resolved := plan.New("demo", "conda", true)
	resolved.Identity.Package = "demo"
	resolved.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "new"}
	got, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved)
	if err != nil {
		t.Fatalf("VerifyResolvedCandidate: %v", err)
	}
	if got.State != plan.StateBroken || !strings.Contains(got.Detail, "probe failed") {
		t.Fatalf("verification=%+v, want redacted broken observation", got)
	}
	if strings.Contains(got.Detail, "pass") || strings.Contains(got.Detail, "secret") {
		t.Fatalf("verification detail leaked probe credentials: %q", got.Detail)
	}
	if got := adapter.seen.Config["environment"]; got != "new" {
		t.Fatalf("observed environment=%v, want new", got)
	}
}
