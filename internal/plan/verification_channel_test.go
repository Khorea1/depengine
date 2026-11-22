package plan

import (
	"reflect"
	"testing"
)

func TestReconcileChannelIdentity(t *testing.T) {
	desired := ResolvedIdentity{Package: "demo", Channel: "latest/stable"}
	for _, tt := range []struct {
		name     string
		observed Observation
		state    VerificationState
		drift    []IdentityDrift
		unknown  []IdentityField
	}{
		{name: "matching", observed: Observation{Presence: PresencePresent, Identity: ObservedIdentity{Package: "demo", Channel: "latest/stable"}, KnownFields: []IdentityField{FieldPackage, FieldChannel}}, state: StateSatisfied},
		{name: "different channel", observed: Observation{Presence: PresencePresent, Identity: ObservedIdentity{Package: "demo", Channel: "latest/beta"}, KnownFields: []IdentityField{FieldPackage, FieldChannel}}, state: StateDrifted, drift: []IdentityDrift{{Field: FieldChannel, Desired: "latest/stable", Observed: "latest/beta"}}},
		{name: "channel not reported", observed: Observation{Presence: PresencePresent, Identity: ObservedIdentity{Package: "demo"}, KnownFields: []IdentityField{FieldPackage}}, state: StateUnknown, unknown: []IdentityField{FieldChannel}},
		{name: "not installed", observed: Observation{Presence: PresenceAbsent}, state: StateAbsent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Reconcile(desired, tt.observed)
			if got.State != tt.state || !reflect.DeepEqual(got.Drift, tt.drift) || !reflect.DeepEqual(got.Unverifiable, tt.unknown) {
				t.Fatalf("Reconcile() = %+v, want state=%s drift=%+v unverifiable=%v", got, tt.state, tt.drift, tt.unknown)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestReconcileRejectsMalformedObservedChannel(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Package: "demo", Channel: "latest/beta"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Package: "demo", Channel: " latest/beta"},
		KnownFields: []IdentityField{FieldPackage, FieldChannel},
	})
	if got.State != StateBroken {
		t.Fatalf("malformed channel verified as %s", got.State)
	}
}
