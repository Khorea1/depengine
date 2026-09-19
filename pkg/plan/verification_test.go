package plan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReconcileSatisfiedExactIdentity(t *testing.T) {
	desired := ResolvedIdentity{Version: "1.4.3", Source: "registry.example/stable", Scope: "user"}
	got := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.4.3", Source: "registry.example/stable", Scope: "user"},
		KnownFields: []IdentityField{FieldScope, FieldVersion, FieldSource},
	})
	if got.State != StateSatisfied {
		t.Fatalf("state = %q, want %q: %#v", got.State, StateSatisfied, got)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	wantFields := []IdentityField{FieldVersion, FieldSource, FieldScope}
	if !reflect.DeepEqual(got.KnownFields, wantFields) {
		t.Fatalf("known fields = %#v, want %#v", got.KnownFields, wantFields)
	}
}

func TestReconcileVersionDriftIsNotSatisfied(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "2.0.0"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.9.0"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if got.State != StateDrifted {
		t.Fatalf("state = %q, want %q", got.State, StateDrifted)
	}
	want := []IdentityDrift{{Field: FieldVersion, Desired: "2.0.0", Observed: "1.9.0"}}
	if !reflect.DeepEqual(got.Drift, want) {
		t.Fatalf("drift = %#v, want %#v", got.Drift, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestReconcileUnknownWhenManagerCannotReportDesiredIdentity(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "2.0.0", Source: "stable"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "2.0.0"},
		KnownFields: []IdentityField{FieldVersion},
		Detail:      "manager does not report package source",
	})
	if got.State != StateUnknown {
		t.Fatalf("state = %q, want %q", got.State, StateUnknown)
	}
	if !reflect.DeepEqual(got.Unverifiable, []IdentityField{FieldSource}) {
		t.Fatalf("unverifiable = %#v", got.Unverifiable)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestReconcileDriftTakesPrecedenceOverUnknownDimensions(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "2", Source: "stable"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if got.State != StateDrifted {
		t.Fatalf("state = %q, want %q", got.State, StateDrifted)
	}
	if len(got.Drift) != 1 || !reflect.DeepEqual(got.Unverifiable, []IdentityField{FieldSource}) {
		t.Fatalf("result = %#v", got)
	}
}

func TestReconcilePresenceStates(t *testing.T) {
	tests := []struct {
		name        string
		observation Observation
		want        VerificationState
	}{
		{name: "absent", observation: Observation{Presence: PresenceAbsent}, want: StateAbsent},
		{name: "unknown", observation: Observation{Presence: PresenceUnknown, Detail: "query unsupported"}, want: StateUnknown},
		{name: "broken", observation: Observation{Presence: PresenceBroken, Detail: "manager query failed"}, want: StateBroken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Reconcile(ResolvedIdentity{Version: "1.0.0"}, tc.observation)
			if got.State != tc.want {
				t.Fatalf("state = %q, want %q", got.State, tc.want)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestReconcileInvalidPresenceFailsClosed(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "1"}, Observation{Presence: "maybe"})
	if got.State != StateBroken {
		t.Fatalf("state = %q, want %q", got.State, StateBroken)
	}
	if got.Detail == "" {
		t.Fatal("expected diagnostic detail")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVerificationValidateRejectsContradictoryResults(t *testing.T) {
	tests := []VerificationResult{
		{State: StateSatisfied, Drift: []IdentityDrift{{Field: FieldVersion, Desired: "2", Observed: "1"}}},
		{State: StateDrifted},
		{State: StateUnknown},
		{State: StateBroken},
		{State: StateDrifted, Drift: []IdentityDrift{{Field: FieldVersion, Desired: "1", Observed: "1"}}},
		{State: StateSatisfied, KnownFields: []IdentityField{FieldVersion, FieldVersion}},
	}
	for i, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error for %#v", i, result)
		}
	}
}

func TestReconcileIgnoresRequestedConstraintAfterConcreteResolution(t *testing.T) {
	desired := ResolvedIdentity{
		RequestedVersion: &VersionIntent{Mode: VersionConstraint, Value: ">=1,<2"},
		Version:          "1.8.4",
	}
	got := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.8.4"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if got.State != StateSatisfied {
		t.Fatalf("state = %q, want %q", got.State, StateSatisfied)
	}
}

func TestReconcileInvalidKnownFieldFailsClosed(t *testing.T) {
	for _, fields := range [][]IdentityField{{"mystery"}, {FieldVersion, FieldVersion}} {
		got := Reconcile(ResolvedIdentity{Version: "1"}, Observation{
			Presence:    PresencePresent,
			KnownFields: fields,
		})
		if got.State != StateBroken {
			t.Fatalf("fields %#v: state = %q, want %q", fields, got.State, StateBroken)
		}
		if got.Detail == "" {
			t.Fatalf("fields %#v: expected detail", fields)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("fields %#v: Validate: %v", fields, err)
		}
	}
}

func TestVerificationJSONRedactsSensitiveObservedState(t *testing.T) {
	result := VerificationResult{
		State: StateDrifted,
		Observed: ObservedIdentity{
			Source: "https://user:secret@example.test/pkg?token=topsecret",
		},
		KnownFields: []IdentityField{FieldSource},
		Drift: []IdentityDrift{{
			Field:    FieldSource,
			Desired:  "https://example.test/pkg?api_key=wantedsecret",
			Observed: "https://example.test/pkg?sig=observedsecret",
		}},
		Detail: "probe https://example.test/?access_token=detailsecret failed",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"secret", "topsecret", "wantedsecret", "observedsecret", "detailsecret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("serialized verification leaked %q: %s", secret, text)
		}
	}
}
