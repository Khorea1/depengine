package plan

import "testing"

func TestReconcileIncludesPackageIdentity(t *testing.T) {
	desired := ResolvedIdentity{Package: "ripgrep"}
	got := Reconcile(desired, Observation{
		Presence: PresencePresent, Identity: ObservedIdentity{Package: "rg"}, KnownFields: []IdentityField{FieldPackage},
	})
	if got.State != StateDrifted || len(got.Drift) != 1 || got.Drift[0].Field != FieldPackage {
		t.Fatalf("Reconcile() = %+v, want package drift", got)
	}
}
