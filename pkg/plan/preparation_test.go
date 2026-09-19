package plan

import (
	"reflect"
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

func TestPreparationRollbackRejectsUnknownOrDuplicateAppliedIDs(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	if _, err := p.RollbackFor([]string{"missing"}); err == nil {
		t.Fatal("expected unknown applied id error")
	}
	if _, err := p.RollbackFor([]string{"source", "source"}); err == nil {
		t.Fatal("expected duplicate applied id error")
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
