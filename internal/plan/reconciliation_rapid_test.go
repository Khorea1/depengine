package plan

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"pgregory.net/rapid"
)

// TestRapidReconciliationSequences exercises observations from multiple logical
// schemas and adapters in an arbitrary order. A new observation must never
// inherit authority from an earlier, successful one.
func TestRapidReconciliationSequences(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		steps := rapid.SliceOfN(rapid.IntRange(0, 65535), 1, 64).Draw(t, "observations")
		for i, step := range steps {
			schema := step % 4
			adapter := (step / 4) % 5
			packageName := fmt.Sprintf("package-%d", (step/20)%3)
			version := fmt.Sprintf("%d.0", 1+(step/60)%4)
			desired := ResolvedIdentity{Package: packageName, Version: version}
			observed := Observation{
				Presence:    PresencePresent,
				Identity:    ObservedIdentity{Package: packageName, Version: version},
				KnownFields: []IdentityField{FieldPackage, FieldVersion},
				Detail:      fmt.Sprintf("schema=%d adapter=%d", schema, adapter),
			}
			switch (step / 240) % 9 {
			case 0: // exact match
			case 1:
				observed.Identity.Version = version + ".stale"
			case 2:
				observed.Presence = PresenceAbsent
				observed.KnownFields = nil
				observed.Identity = ObservedIdentity{}
			case 3:
				observed.Presence = PresenceUnknown
			case 4:
				observed.Presence = PresenceBroken
			case 5: // incomplete, possibly misleading identity
				observed.KnownFields = []IdentityField{FieldPackage}
			case 6: // a different physical package is not a version match
				observed.Identity.Package = "different-package"
			case 7: // invalid adapter output cannot be trusted
				observed.KnownFields = []IdentityField{FieldPackage, FieldPackage}
			case 8:
				observed.Presence = PresenceState("inconsistent")
			}

			verification := Reconcile(desired, observed)
			if err := verification.Validate(); err != nil {
				t.Fatalf("step %d: reconciliation yielded invalid result: %v (%#v)", i, err, verification)
			}
			decision, err := TransitionForVerification(verification)
			switch verification.State {
			case StateUnknown, StateBroken:
				if err == nil || decision.Required {
					t.Fatalf("step %d: inconclusive observation authorized a mutation: %+v, %v", i, decision, err)
				}
			case StateSatisfied:
				if err != nil || decision.Required {
					t.Fatalf("step %d: satisfied observation selected a mutation: %+v, %v", i, decision, err)
				}
			case StateAbsent:
				if err != nil || !decision.Required || decision.Transition != TransitionInstall {
					t.Fatalf("step %d: validated absence did not select install: %+v, %v", i, decision, err)
				}
			case StateDrifted:
				if err != nil || !decision.Required || decision.Transition != TransitionUpgrade {
					t.Fatalf("step %d: proven drift did not select upgrade: %+v, %v", i, decision, err)
				}
			default:
				t.Fatalf("step %d: unexpected state %q", i, verification.State)
			}
		}
	})
}

// TestRapidInterruptedReplacement never permits a removal retry based on an
// ambiguous identity, even after a previous step proved the old version.
func TestRapidInterruptedReplacement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		steps := rapid.SliceOfN(rapid.IntRange(0, 65535), 1, 64).Draw(t, "recovery steps")
		phases := []ReplacementPhase{ReplacementPlanned, ReplacementRemoving, ReplacementRemoved, ReplacementInstalling, ReplacementInstalled, ReplacementPostHookRunning}
		for i, step := range steps {
			pkg := fmt.Sprintf("schema-%d/adapter-%d/tool", step%3, (step/3)%4)
			oldVersion := fmt.Sprintf("%d.0", (step/12)%4+1)
			newVersion := oldVersion + ".next"
			oldIdentity := ResolvedIdentity{Package: pkg, Version: oldVersion}
			targetIdentity := ResolvedIdentity{Package: pkg, Version: newVersion}
			observation := Observation{Presence: PresencePresent, Identity: ObservedIdentity{Package: pkg, Version: oldVersion}, KnownFields: []IdentityField{FieldPackage, FieldVersion}}
			oldObs, desiredObs := observation, observation
			switch (step / 48) % 9 {
			case 0: // same version, two matching snapshots
			case 1:
				desiredObs.Identity.Version = "changed-between-probes"
			case 2:
				desiredObs.Identity.Package = "other-physical-package"
			case 3:
				desiredObs.KnownFields = []IdentityField{FieldVersion}
			case 4:
				desiredObs.Presence = PresenceUnknown
			case 5:
				desiredObs.Presence = PresenceBroken
			case 6:
				oldObs.Presence = PresenceUnknown
			case 7:
				oldObs.Presence = PresenceAbsent
				oldObs.KnownFields = nil
				oldObs.Identity = ObservedIdentity{}
				desiredObs = oldObs
			case 8:
				desiredObs.Identity.Version = newVersion
			}
			phase := phases[(step/432)%len(phases)]
			old := Reconcile(oldIdentity, oldObs)
			desired := Reconcile(targetIdentity, desiredObs)
			action, err := ReplacementRecoveryFor(phase, old, desired)
			if err != nil {
				t.Fatalf("step %d: valid recovery inputs rejected: %v", i, err)
			}
			if old.State == StateUnknown || old.State == StateBroken || desired.State == StateUnknown || desired.State == StateBroken {
				if action != ReplacementBlocked {
					t.Fatalf("step %d: uncertain recovery authorized %q", i, action)
				}
			}
			if action == ReplacementRetryRemoval {
				if phase != ReplacementPlanned && phase != ReplacementRemoving {
					t.Fatalf("step %d: retry removal outside WAL removal phase", i)
				}
				if old.State != StateSatisfied {
					t.Fatalf("step %d: removal retry without verified old version", i)
				}
				if desired.State != StateAbsent && (desired.State != StateDrifted ||
					!slices.Contains(desired.KnownFields, FieldPackage) ||
					!slices.Contains(desired.KnownFields, FieldVersion) ||
					desired.Observed.Package != old.Observed.Package ||
					desired.Observed.Version != old.Observed.Version) {
					t.Fatalf("step %d: changed or incomplete identity permitted removal: old=%+v target=%+v", i, old, desired)
				}
			}
		}
	})
}

// TestRapidSharedResourceRelease varies schema, adapter, version and physical
// resource identity. Persistent claims are per dependent, not per bare logical
// tool name, and only the final depengine-owned reference is removable.
func TestRapidSharedResourceRelease(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		claims := rapid.SliceOfN(rapid.IntRange(0, 4095), 1, 32).Draw(t, "claims")
		removals := rapid.SliceOfN(rapid.IntRange(0, 4095), 1, 48).Draw(t, "removals")
		ownedByEngine := rapid.Bool().Draw(t, "depengine owner")
		slot := rapid.IntRange(0, 5).Draw(t, "physical slot")
		resource := ResourceIdentity{Kind: ResourceSource, Key: fmt.Sprintf("repo:slot-%d", slot)}
		seen := make(map[string]bool)
		for _, claim := range claims {
			// Identical logical names in separate schemas must remain distinct claims.
			dependent := fmt.Sprintf("schema-%d/adapter-%d/tool-%d@v%d", claim%4, (claim/4)%4, (claim/16)%3, (claim/48)%3)
			seen[dependent] = true
		}
		dependents := make([]string, 0, len(seen))
		for dep := range seen {
			dependents = append(dependents, dep)
		}
		slices.Sort(dependents)
		ownership := OwnershipExternal
		if ownedByEngine {
			ownership = OwnershipDepengine
		}
		current := []OwnedResourceState{{Resource: resource, Ownership: ownership, Dependents: dependents}}
		for i, removal := range removals {
			name := dependents[removal%len(dependents)]
			before := slices.Clone(current)
			for j := range before {
				before[j].Dependents = slices.Clone(current[j].Dependents)
			}
			wasClaimed := slices.Contains(current[0].Dependents, name)
			release, err := ReleaseDependentResources(current, name)
			if err != nil {
				t.Fatalf("step %d: release failed: %v", i, err)
			}
			if !reflect.DeepEqual(before, current) {
				t.Fatalf("step %d: release mutated input snapshot", i)
			}
			if err := ValidateOwnedResourceSnapshot(release.Updated); err != nil {
				t.Fatalf("step %d: invalid persisted snapshot: %v", i, err)
			}
			if len(release.Updated) != 1 || release.Updated[0].Resource != resource {
				t.Fatalf("step %d: release lost or changed a physical resource", i)
			}
			remaining := release.Updated[0].Dependents
			wantRemovable := wasClaimed && ownedByEngine && len(remaining) == 0
			if got := slices.Contains(release.Removable, resource); got != wantRemovable {
				t.Fatalf("step %d: removable=%t, want %t (owned=%s, refs=%v)", i, got, wantRemovable, ownership, remaining)
			}
			if len(release.Removable) > 1 || (!wantRemovable && len(release.Removable) != 0) {
				t.Fatalf("step %d: unexpected cleanup targets: %+v", i, release.Removable)
			}
			current = release.Updated
		}
		other := ProspectiveTarget{Scope: "host", Kind: "source", Identity: resource.Key}
		alias := other
		if rapid.Bool().Draw(t, "other slot") {
			alias.Identity += "-another-name"
		}
		got := CompareProspectiveTargets(other, alias)
		if alias == other && got != OverlapSame || alias != other && got != OverlapUnknown {
			t.Fatalf("overlap=%q for %v and %v", got, other, alias)
		}
		if got == OverlapDisjoint {
			t.Fatal("unverified physical independence was treated as proven")
		}
	})
}
