package plan

import (
	"reflect"
	"strings"
	"testing"
)

func mut(kind string) Operation  { return Operation{Kind: kind, Effect: EffectMutation} }
func read(kind string) Operation { return Operation{Kind: kind, Effect: EffectReadOnly} }

func TestPreparationPlanSeparatesProbeFromMutation(t *testing.T) {
	p := PreparationPlan{
		Probe: []Operation{read("check-source")},
		Prepare: []PreparationMutation{{
			ID:        "add-source",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "https://packages.example.test/stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-add"),
			Rollback:  ptrOp(mut("source-remove")),
			Policy:    RollbackSafe,
		}},
		Commit: []Operation{mut("install-package")},
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}

	p.Probe[0].Effect = EffectMutation
	if err := p.Validate(); err == nil {
		t.Fatal("expected mutating probe to be rejected")
	}
}

func TestPreparationRollbackReverseOrderAndRetainedReporting(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{
			ID:        "source",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-add"),
			Rollback:  ptrOp(mut("source-remove")),
			Policy:    RollbackSafe,
		},
		{
			ID:        "prereq",
			Resource:  ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"},
			Ownership: OwnershipDepengine,
			Apply:     mut("prereq-install"),
			Policy:    RollbackRetain,
		},
	}}
	got, err := p.RollbackFor([]string{"source", "prereq"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Operation{mut("source-remove")}; !reflect.DeepEqual(got.Operations, want) {
		t.Fatalf("rollback operations = %#v, want %#v", got.Operations, want)
	}
	if want := []ResourceIdentity{{Kind: ResourcePrerequisite, Key: "pkg:compiler"}}; !reflect.DeepEqual(got.Retained, want) {
		t.Fatalf("retained = %#v, want %#v", got.Retained, want)
	}
}

func TestPreparationRollbackRejectsNonPrefixAppliedIDs(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{
			ID:        "source",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-add"),
			Rollback:  ptrOp(mut("source-remove")),
			Policy:    RollbackSafe,
		},
		{
			ID:        "prereq",
			Resource:  ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"},
			Ownership: OwnershipDepengine,
			Apply:     mut("prereq-install"),
			Policy:    RollbackRetain,
		},
	}}
	for _, applied := range [][]string{
		{"missing"},
		{"prereq"},
		{"prereq", "source"},
		{"source", "source"},
		{"source", "prereq", "extra"},
	} {
		if _, err := p.RollbackFor(applied); err == nil {
			t.Fatalf("RollbackFor(%v) unexpectedly accepted non-prefix applied state", applied)
		}
	}
}

func TestPreparationMutationPolicyValidation(t *testing.T) {
	base := PreparationMutation{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
	}
	m := base
	m.Policy = RollbackSafe
	if err := m.Validate(); err == nil {
		t.Fatal("safe policy without rollback must fail")
	}
	m = base
	m.Policy = RollbackRetain
	m.Rollback = ptrOp(mut("unexpected"))
	if err := m.Validate(); err == nil {
		t.Fatal("retain policy with automatic rollback must fail")
	}
}

func TestPreparationPlanRejectsDuplicateResourceIdentity(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{
			ID:        "source-add",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-add"),
			Rollback:  ptrOp(mut("source-remove")),
			Policy:    RollbackSafe,
		},
		{
			ID:        "source-refresh",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-refresh"),
			Policy:    RollbackRetain,
		},
	}}
	if err := p.Validate(); err == nil {
		t.Fatal("duplicate preparation resource identity unexpectedly accepted")
	}
	if _, err := p.RollbackFor([]string{"source-add"}); err == nil {
		t.Fatal("rollback unexpectedly accepted plan with duplicate resource identity")
	}
}

func TestSharedResourceRefcountAndOwnership(t *testing.T) {
	state := OwnedResourceState{
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
	}
	var err error
	state, err = ClaimResource(state, "tool-b")
	if err != nil {
		t.Fatal(err)
	}
	state, err = ClaimResource(state, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	state, err = ClaimResource(state, "tool-a") // idempotent claim
	if err != nil {
		t.Fatal(err)
	}
	if got, want := state.Dependents, []string{"tool-a", "tool-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependents = %v, want %v", got, want)
	}
	if state.RefCount() != 2 {
		t.Fatalf("refcount = %d, want 2", state.RefCount())
	}

	state, removable, err := ReleaseResource(state, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	if removable {
		t.Fatal("shared resource must not be removable while another dependent remains")
	}
	state, removable, err = ReleaseResource(state, "tool-b")
	if err != nil {
		t.Fatal(err)
	}
	if !removable {
		t.Fatal("depengine-owned unreferenced resource should be removable")
	}
}

func TestExternalResourceNeverBecomesRemovable(t *testing.T) {
	state := OwnedResourceState{
		Resource:   ResourceIdentity{Kind: ResourceSource, Key: "repo:system"},
		Ownership:  OwnershipExternal,
		Dependents: []string{"tool-a"},
	}
	_, removable, err := ReleaseResource(state, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	if removable {
		t.Fatal("external resource must never be removed by depengine")
	}
}

func TestResourceIdentityRejectsCredentialBearingKey(t *testing.T) {
	r := ResourceIdentity{Kind: ResourceSource, Key: "https://user:secret@example.test/repo"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected credential-bearing resource identity to fail")
	}
}

func ptrOp(op Operation) *Operation { return &op }

func TestSafeRollbackRequiresDepengineOwnership(t *testing.T) {
	m := PreparationMutation{
		ID:        "external-source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:external"},
		Ownership: OwnershipExternal,
		Apply:     mut("source-change"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected automatic rollback of external resource to be rejected")
	}
}

func TestPreparationJournalLifecycle(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}, Commit: []Operation{mut("install")}}

	j := NewPreparationJournal()
	if j.ReadyForCommit(p) {
		t.Fatal("journal must not be ready before required preparation")
	}
	if _, err := j.RecordApplied(p, "prereq"); err == nil {
		t.Fatal("expected out-of-order mutation to fail")
	}
	var err error
	j, err = j.RecordApplied(p, "source")
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationPreparing {
		t.Fatalf("status = %q", j.Status)
	}
	j, err = j.RecordApplied(p, "prereq")
	if err != nil {
		t.Fatal(err)
	}
	if !j.ReadyForCommit(p) || j.Status != PreparationReady {
		t.Fatalf("journal not ready: %#v", j)
	}
	j, err = j.MarkCommitted(p)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationCommitted {
		t.Fatalf("status = %q", j.Status)
	}
	if _, _, err := j.PlanRollback(p); err == nil {
		t.Fatal("committed preparation must not use candidate rollback")
	}
}

func TestPreparationJournalRollbackAfterPartialFailure(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	j, err := NewPreparationJournal().RecordApplied(p, "source")
	if err != nil {
		t.Fatal(err)
	}
	j, decision, err := j.PlanRollback(p)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationRolledBack {
		t.Fatalf("status = %q", j.Status)
	}
	if got, want := decision.Operations, []Operation{mut("source-remove")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback = %#v, want %#v", got, want)
	}
}

func FuzzSharedResourceRefcountProperties(f *testing.F) {
	f.Add("tool-a", "tool-b")
	f.Add("same", "same")
	f.Add("z", "a")
	f.Fuzz(func(t *testing.T, first, second string) {
		if len(first) > 128 || len(second) > 128 {
			return
		}
		valid := func(s string) bool {
			return s != "" && strings.TrimSpace(s) == s && !strings.ContainsRune(s, '\x00')
		}

		base := OwnedResourceState{
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:fuzz"},
			Ownership: OwnershipDepengine,
		}
		if !valid(first) {
			if _, err := ClaimResource(base, first); err == nil {
				t.Fatalf("ClaimResource accepted invalid dependent %q", first)
			}
			return
		}
		if !valid(second) {
			if _, err := ClaimResource(base, second); err == nil {
				t.Fatalf("ClaimResource accepted invalid dependent %q", second)
			}
			return
		}
		left, err := ClaimResource(base, first)
		if err != nil {
			t.Fatal(err)
		}
		left, err = ClaimResource(left, second)
		if err != nil {
			t.Fatal(err)
		}
		leftAgain, err := ClaimResource(left, first)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(left, leftAgain) {
			t.Fatalf("claim is not idempotent: %#v vs %#v", left, leftAgain)
		}

		right, err := ClaimResource(base, second)
		if err != nil {
			t.Fatal(err)
		}
		right, err = ClaimResource(right, first)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("claim order changed canonical state: %#v vs %#v", left, right)
		}

		next, removable, err := ReleaseResource(left, first)
		if err != nil {
			t.Fatal(err)
		}
		if first == second {
			if !removable || next.RefCount() != 0 {
				t.Fatalf("single unique dependent release = removable %v refcount %d", removable, next.RefCount())
			}
			return
		}
		if removable || next.RefCount() != 1 {
			t.Fatalf("first release = removable %v refcount %d, want false/1", removable, next.RefCount())
		}
		final, removable, err := ReleaseResource(next, second)
		if err != nil {
			t.Fatal(err)
		}
		if !removable || final.RefCount() != 0 {
			t.Fatalf("final release = removable %v refcount %d, want true/0", removable, final.RefCount())
		}
	})
}

func TestPreparationJournalRejectsCorruptedPersistedState(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	tests := []PreparationJournal{
		{Status: PreparationPreparing, Applied: []string{"prereq"}},
		{Status: PreparationPending, Applied: []string{"source"}},
		{Status: PreparationPreparing},
		{Status: PreparationReady, Applied: []string{"source"}},
		{Status: PreparationCommitted, Applied: []string{"source"}},
		{Status: "mystery"},
		{Status: PreparationPreparing, Applied: []string{"source", "prereq", "extra"}},
	}
	for i, journal := range tests {
		if err := journal.Validate(p); err == nil {
			t.Fatalf("case %d: Validate() accepted corrupted journal %#v", i, journal)
		}
		if journal.ReadyForCommit(p) {
			t.Fatalf("case %d: corrupted journal reported ready", i)
		}
	}
}

func TestPreparationJournalTransitionsRejectStalePrefix(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	stale := PreparationJournal{Status: PreparationPreparing, Applied: []string{"old-source"}}
	if _, err := stale.RecordApplied(p, "prereq"); err == nil {
		t.Fatal("RecordApplied accepted stale journal prefix")
	}
	if _, err := stale.MarkCommitted(p); err == nil {
		t.Fatal("MarkCommitted accepted stale journal prefix")
	}
	if _, _, err := stale.PlanRollback(p); err == nil {
		t.Fatal("PlanRollback accepted stale journal prefix")
	}
}

func TestOwnedResourceDependentRejectsNULAndWhitespace(t *testing.T) {
	base := OwnedResourceState{Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine}
	for _, dependent := range []string{" tool", "tool ", "tool\x00name"} {
		if _, err := ClaimResource(base, dependent); err == nil {
			t.Fatalf("ClaimResource(%q) unexpectedly succeeded", dependent)
		}
		claimed, err := ClaimResource(base, "tool")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReleaseResource(claimed, dependent); err == nil {
			t.Fatalf("ReleaseResource(%q) unexpectedly succeeded", dependent)
		}
	}
}

func TestPreparationMutationIDRejectsNUL(t *testing.T) {
	m := PreparationMutation{
		ID:        "source\x00add",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}
	if err := m.Validate(); err == nil {
		t.Fatal("preparation mutation ID containing NUL unexpectedly accepted")
	}
}

func TestRollbackDecisionDoesNotAliasPlanCommand(t *testing.T) {
	rollback := Operation{Kind: "source-remove", Effect: EffectMutation, Command: []string{"tool", "remove"}, ArbitraryCode: true}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     Operation{Kind: "source-add", Effect: EffectMutation},
		Rollback:  &rollback,
		Policy:    RollbackSafe,
	}}}
	decision, err := p.RollbackFor([]string{"source"})
	if err != nil {
		t.Fatal(err)
	}
	original := p.Prepare[0].Rollback.Command[0]
	decision.Operations[0].Command[0] = "mutated"
	if p.Prepare[0].Rollback.Command[0] != original {
		t.Fatal("RollbackFor output aliases plan rollback command")
	}
}

func TestResourceIdentityRequiresCanonicalURLKey(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "HTTPS://EXAMPLE.TEST/index?z=2&a=1"}
	if err := resource.Validate(); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("Validate() error = %v, want canonical resource rejection", err)
	}
	resource.Key = "https://example.test/index?a=1&z=2"
	if err := resource.Validate(); err != nil {
		t.Fatalf("canonical resource rejected: %v", err)
	}
}
