package exec

import (
	"context"
	"errors"
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

func TestVerifyResolvedCandidateUsesResolvedTargetAndReturnsObserveError(t *testing.T) {
	adapter := &verificationAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "conda"}, observeErr: errors.New("probe failed")}}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "demo", "environment": "old"}}
	resolved := plan.New("demo", "conda", true)
	resolved.Identity.Package = "demo"
	resolved.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "new"}
	if _, err := ex.VerifyResolvedCandidate(context.Background(), tool, method, &resolved); err == nil {
		t.Fatal("expected Observe error")
	}
	if got := adapter.seen.Config["environment"]; got != "new" {
		t.Fatalf("observed environment=%v, want new", got)
	}
}
