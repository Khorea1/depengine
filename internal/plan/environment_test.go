package plan

import "testing"

func TestEnvironmentTargetValidate(t *testing.T) {
	for _, target := range []EnvironmentTarget{
		{Kind: EnvironmentNamed, Value: "build"},
		{Kind: EnvironmentPrefix, Value: "/opt/conda/envs/build"},
		{Kind: EnvironmentPrefix, Value: `C:\envs\build`},
		{Kind: EnvironmentProfile, Value: "dev"},
		{Kind: EnvironmentProject, Value: "./workspace"},
	} {
		if err := target.Validate(); err != nil {
			t.Fatalf("Validate(%+v): %v", target, err)
		}
	}
}

func TestEnvironmentTargetRejectsAmbiguousValues(t *testing.T) {
	for _, target := range []EnvironmentTarget{
		{},
		{Kind: "unknown", Value: "x"},
		{Kind: EnvironmentNamed},
		{Kind: EnvironmentNamed, Value: " dev"},
		{Kind: EnvironmentNamed, Value: "dev\x00prod"},
	} {
		if err := target.Validate(); err == nil {
			t.Fatalf("Validate(%+v) unexpectedly succeeded", target)
		}
	}
}

func TestEnvironmentTargetCanonicalKeySeparatesKinds(t *testing.T) {
	name := EnvironmentTarget{Kind: EnvironmentNamed, Value: "dev"}
	profile := EnvironmentTarget{Kind: EnvironmentProfile, Value: "dev"}
	if name.CanonicalKey() == profile.CanonicalKey() {
		t.Fatalf("different environment kinds collapsed to %q", name.CanonicalKey())
	}
}

func TestResolvedPlanEnvironmentIsValidatedAndLocked(t *testing.T) {
	target := &EnvironmentTarget{Kind: EnvironmentNamed, Value: "build"}
	p := New("cmake", "conda", true)
	p.Identity.Version = "3.30.0"
	p.Identity.Environment = target
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	locked, err := ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	if locked.Identity.Environment == nil || locked.Identity.Environment.CanonicalKey() != "named:build" {
		t.Fatalf("lock environment = %#v", locked.Identity.Environment)
	}

	p.Identity.Environment = &EnvironmentTarget{Kind: EnvironmentNamed, Value: " build"}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted invalid environment target")
	}
}

func TestReconcileEnvironmentTargetByKindAndValue(t *testing.T) {
	desired := ResolvedIdentity{Environment: &EnvironmentTarget{Kind: EnvironmentNamed, Value: "dev"}}
	got := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Environment: &EnvironmentTarget{Kind: EnvironmentProfile, Value: "dev"}},
		KnownFields: []IdentityField{FieldEnvironment},
	})
	if got.State != StateDrifted {
		t.Fatalf("state = %q, want %q", got.State, StateDrifted)
	}
	if len(got.Drift) != 1 || got.Drift[0].Desired != "named:dev" || got.Drift[0].Observed != "profile:dev" {
		t.Fatalf("environment drift = %#v", got.Drift)
	}
}
