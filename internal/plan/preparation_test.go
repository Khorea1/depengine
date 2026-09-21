package plan

import (
	"reflect"
	"strings"
	"testing"
)

func mut(kind string) Operation  { return Operation{Kind: kind, Effect: EffectMutation} }
func read(kind string) Operation { return Operation{Kind: kind, Effect: EffectReadOnly} }

func recordPreparationMutation(t *testing.T, j PreparationJournal, p PreparationPlan, id string) PreparationJournal {
	t.Helper()
	planned, err := j.PlanApply(p, id)
	if err != nil {
		t.Fatalf("PlanApply(%q): %v", id, err)
	}
	applied, err := planned.RecordApplied(p, id)
	if err != nil {
		t.Fatalf("RecordApplied(%q): %v", id, err)
	}
	return applied
}

func recordRollbackMutation(t *testing.T, j PreparationJournal, p PreparationPlan, owned []OwnedResourceState, id string) PreparationJournal {
	t.Helper()
	planned, _, err := j.PlanRollbackApply(p, owned, id)
	if err != nil {
		t.Fatalf("PlanRollbackApply(%q): %v", id, err)
	}
	applied, err := planned.RecordRollbackApplied(p, owned, id)
	if err != nil {
		t.Fatalf("RecordRollbackApplied(%q): %v", id, err)
	}
	return applied
}

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
	got, err := p.RollbackFor([]string{"source", "prereq"}, nil)
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

func TestPreparationRollbackRetainsSharedOwnedResource(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  resource,
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	owned := []OwnedResourceState{{
		Resource:   resource,
		Ownership:  OwnershipDepengine,
		Dependents: []string{"other-tool"},
	}}

	got, err := p.RollbackFor([]string{"source"}, owned)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Operations) != 0 {
		t.Fatalf("shared resource rollback operations = %#v, want none", got.Operations)
	}
	if want := []ResourceIdentity{resource}; !reflect.DeepEqual(got.Retained, want) {
		t.Fatalf("retained = %#v, want %#v", got.Retained, want)
	}
}

func TestPreparationRollbackAllowsUnclaimedOwnedResource(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  resource,
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	owned := []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine}}

	got, err := p.RollbackFor([]string{"source"}, owned)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Operation{mut("source-remove")}; !reflect.DeepEqual(got.Operations, want) {
		t.Fatalf("rollback operations = %#v, want %#v", got.Operations, want)
	}
	if len(got.Retained) != 0 {
		t.Fatalf("retained = %#v, want none", got.Retained)
	}
}

func TestPreparationRollbackRejectsOwnershipSnapshotMismatch(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  resource,
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	owned := []OwnedResourceState{{Resource: resource, Ownership: OwnershipExternal}}

	if _, err := p.RollbackFor([]string{"source"}, owned); err == nil || !strings.Contains(err.Error(), "ownership changed") {
		t.Fatalf("RollbackFor() error = %v, want ownership mismatch", err)
	}
}

func TestPreparationRollbackRejectsDuplicateOwnershipSnapshot(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  resource,
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	owned := []OwnedResourceState{
		{Resource: resource, Ownership: OwnershipDepengine},
		{Resource: resource, Ownership: OwnershipDepengine},
	}

	if _, err := p.RollbackFor([]string{"source"}, owned); err == nil || !strings.Contains(err.Error(), "duplicate owned resource") {
		t.Fatalf("RollbackFor() error = %v, want duplicate snapshot rejection", err)
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
		if _, err := p.RollbackFor(applied, nil); err == nil {
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
	if _, err := p.RollbackFor([]string{"source-add"}, nil); err == nil {
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
	if _, err := j.PlanApply(p, "prereq"); err == nil {
		t.Fatal("expected out-of-order mutation to fail")
	}
	if _, err := j.RecordApplied(p, "source"); err == nil {
		t.Fatal("RecordApplied unexpectedly accepted a mutation without a persisted PlanApply boundary")
	}
	planned, err := j.PlanApply(p, "source")
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != PreparationApplying || planned.Applying != "source" || len(planned.Applied) != 0 {
		t.Fatalf("planned mutation journal = %#v", planned)
	}
	j, err = planned.RecordApplied(p, "source")
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationPreparing {
		t.Fatalf("status = %q", j.Status)
	}
	j = recordPreparationMutation(t, j, p, "prereq")
	if !j.ReadyForCommit(p) || j.Status != PreparationReady {
		t.Fatalf("journal not ready: %#v", j)
	}
	j, err = j.PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationCommitting {
		t.Fatalf("status = %q, want %q", j.Status, PreparationCommitting)
	}
	if _, _, err := j.PlanRollback(p, nil); err == nil {
		t.Fatal("commit-in-progress preparation must not use candidate rollback")
	}
	j, err = j.MarkCommitted(p)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationCommitted {
		t.Fatalf("status = %q", j.Status)
	}
	if _, _, err := j.PlanRollback(p, nil); err == nil {
		t.Fatal("committed preparation must not use candidate rollback")
	}
}

func TestPreparationJournalRollbackAfterPartialFailure(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	j := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	j, decision, err := j.PlanRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationRollingBack {
		t.Fatalf("status = %q", j.Status)
	}
	if got, want := decision.Operations, []Operation{mut("source-remove")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback = %#v, want %#v", got, want)
	}
	if got, want := decision.OperationIDs, []string{"source"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback ids = %#v, want %#v", got, want)
	}
	j = recordRollbackMutation(t, j, p, nil, "source")
	j, owned, err := j.FinalizeRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationRolledBack {
		t.Fatalf("final status = %q", j.Status)
	}
	if len(owned) != 0 {
		t.Fatalf("owned = %#v, want none after successful safe rollback", owned)
	}
}

func TestPreparationJournalFinalizeRollbackRecordsRetainedOrphans(t *testing.T) {
	source := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	prereq := ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: source, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: prereq, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	j := NewPreparationJournal()
	j = recordPreparationMutation(t, j, p, "source")
	j = recordPreparationMutation(t, j, p, "prereq")
	j, decision, err := j.PlanRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := decision.Retained, []ResourceIdentity{prereq}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retained = %#v, want %#v", got, want)
	}

	j = recordRollbackMutation(t, j, p, nil, "source")
	j, owned, err := j.FinalizeRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != PreparationRolledBack {
		t.Fatalf("status = %q, want %q", j.Status, PreparationRolledBack)
	}
	want := []OwnedResourceState{{Resource: prereq, Ownership: OwnershipDepengine}}
	if !reflect.DeepEqual(owned, want) {
		t.Fatalf("owned = %#v, want %#v", owned, want)
	}
}

func TestPreparationJournalFinalizeRollbackPreservesSharedRetainedState(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:shared"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID: "source", Resource: resource, Ownership: OwnershipDepengine,
		Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe,
	}}}
	j := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	owned := []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine, Dependents: []string{"tool-b"}}}
	j, decision, err := j.PlanRollback(p, owned)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Operations) != 0 || !reflect.DeepEqual(decision.Retained, []ResourceIdentity{resource}) {
		t.Fatalf("decision = %#v, want retained shared resource", decision)
	}
	final, got, err := j.FinalizeRollback(p, owned)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != PreparationRolledBack {
		t.Fatalf("status = %q", final.Status)
	}
	if !reflect.DeepEqual(got, owned) {
		t.Fatalf("owned = %#v, want %#v", got, owned)
	}
	got[0].Dependents[0] = "mutated"
	if owned[0].Dependents[0] != "tool-b" {
		t.Fatal("FinalizeRollback output aliases ownership input")
	}
}

func TestPreparationJournalRollbackWALResumesOnlyRemainingOperations(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Rollback: ptrOp(mut("prereq-remove")), Policy: RollbackSafe},
	}}
	j := NewPreparationJournal()
	j = recordPreparationMutation(t, j, p, "source")
	j = recordPreparationMutation(t, j, p, "prereq")
	j, _, err := j.PlanRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}

	planned, op, err := j.PlanRollbackApply(p, nil, "prereq")
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != "prereq-remove" || planned.RollbackApplying != "prereq" {
		t.Fatalf("planned rollback = %#v op=%#v", planned, op)
	}
	blocked, err := planned.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Action != RecoveryBlocked || !strings.Contains(blocked.Detail, "prereq") {
		t.Fatalf("ambiguous rollback recovery = %#v", blocked)
	}

	j, err = planned.RecordRollbackApplied(p, nil, "prereq")
	if err != nil {
		t.Fatal(err)
	}
	resume, err := j.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resume.Action != RecoveryResumeRollback || !reflect.DeepEqual(resume.Rollback.OperationIDs, []string{"source"}) || !reflect.DeepEqual(resume.Rollback.Operations, []Operation{mut("source-remove")}) {
		t.Fatalf("resume = %#v, want only source rollback", resume)
	}
	if _, _, err := j.FinalizeRollback(p, nil); err == nil || !strings.Contains(err.Error(), "unconfirmed") {
		t.Fatalf("FinalizeRollback error = %v, want incomplete rollback rejection", err)
	}

	j = recordRollbackMutation(t, j, p, nil, "source")
	final, owned, err := j.FinalizeRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != PreparationRolledBack || len(owned) != 0 {
		t.Fatalf("final = %#v owned=%#v", final, owned)
	}
}

func TestPreparationJournalResolveAmbiguousRollbackOutcome(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine,
		Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe,
	}}}
	j := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	j, _, err := j.PlanRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	j, _, err = j.PlanRollbackApply(p, nil, "source")
	if err != nil {
		t.Fatal(err)
	}

	notApplied, err := j.ResolveRollbackApplying(p, nil, "source", PreparationMutationNotApplied)
	if err != nil {
		t.Fatal(err)
	}
	if notApplied.RollbackApplying != "" || len(notApplied.RollbackApplied) != 0 {
		t.Fatalf("not-applied resolution = %#v", notApplied)
	}
	if _, _, err := notApplied.PlanRollbackApply(p, nil, "source"); err != nil {
		t.Fatalf("retry after not_applied: %v", err)
	}

	applied, err := j.ResolveRollbackApplying(p, nil, "source", PreparationMutationApplied)
	if err != nil {
		t.Fatal(err)
	}
	if applied.RollbackApplying != "" || !reflect.DeepEqual(applied.RollbackApplied, []string{"source"}) {
		t.Fatalf("applied resolution = %#v", applied)
	}
	if _, _, err := applied.PlanRollbackApply(p, nil, "source"); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("PlanRollbackApply after completion error = %v", err)
	}
}

func TestPreparationJournalFinalizeRollbackRequiresPlannedRollback(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe,
	}}}
	j := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	if _, _, err := j.FinalizeRollback(p, nil); err == nil {
		t.Fatal("FinalizeRollback unexpectedly accepted unplanned rollback")
	}
	j, _, err := j.PlanRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.PlanRollback(p, nil); err == nil {
		t.Fatal("PlanRollback unexpectedly accepted rollback already in progress")
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
		{Status: PreparationCommitting, Applied: []string{"source"}},
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

func TestPreparationJournalValidateShapeRejectsPersistedCorruptionWithoutPlan(t *testing.T) {
	tests := []PreparationJournal{
		{Status: "mystery"},
		{Status: PreparationPending, Applied: []string{"source"}},
		{Status: PreparationPending, Applying: "source"},
		{Status: PreparationApplying},
		{Status: PreparationApplying, Applied: []string{"source"}, Applying: "source"},
		{Status: PreparationPreparing},
		{Status: PreparationPreparing, Applied: []string{"source"}, Applying: "prereq"},
		{Status: PreparationPreparing, Applied: []string{"source", "source"}},
		{Status: PreparationRollingBack, Applied: []string{" source"}},
		{Status: PreparationRolledBack, Applied: []string{"source\x00remove"}},
	}
	for i, journal := range tests {
		if err := journal.ValidateShape(); err == nil {
			t.Fatalf("case %d: ValidateShape() accepted %#v", i, journal)
		}
	}

	for _, journal := range []PreparationJournal{
		{},
		{Status: PreparationApplying, Applying: "source"},
		{Status: PreparationApplying, Applied: []string{"source"}, Applying: "prereq"},
		{Status: PreparationPreparing, Applied: []string{"source"}},
		{Status: PreparationReady},
		{Status: PreparationCommitting, Applied: []string{"source"}},
		{Status: PreparationCommitted, Applied: []string{"source"}},
		{Status: PreparationRollingBack, Applied: []string{"source"}},
		{Status: PreparationRolledBack, Applied: []string{"source"}},
	} {
		if err := journal.ValidateShape(); err != nil {
			t.Fatalf("ValidateShape(%#v): %v", journal, err)
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
	if _, _, err := stale.PlanRollback(p, nil); err == nil {
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
	decision, err := p.RollbackFor([]string{"source"}, nil)
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

func TestPreparationJournalFinalizeCommitClaimsPreparedResources(t *testing.T) {
	source := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	prereq := ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: source, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: prereq, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	j := NewPreparationJournal()
	j = recordPreparationMutation(t, j, p, "source")
	j = recordPreparationMutation(t, j, p, "prereq")

	j, err := j.PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}
	owned := []OwnedResourceState{{Resource: source, Ownership: OwnershipDepengine, Dependents: []string{"tool-b"}}}
	before := append([]string(nil), owned[0].Dependents...)
	committed, got, err := j.FinalizeCommit(p, owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	if committed.Status != PreparationCommitted {
		t.Fatalf("status = %q, want %q", committed.Status, PreparationCommitted)
	}
	want := []OwnedResourceState{
		{Resource: prereq, Ownership: OwnershipDepengine, Dependents: []string{"tool-a"}},
		{Resource: source, Ownership: OwnershipDepengine, Dependents: []string{"tool-a", "tool-b"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ownership = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(owned[0].Dependents, before) {
		t.Fatalf("FinalizeCommit mutated input dependents: got %v, want %v", owned[0].Dependents, before)
	}
	got[1].Dependents[0] = "mutated"
	if owned[0].Dependents[0] != "tool-b" {
		t.Fatal("FinalizeCommit output aliases input ownership state")
	}
}

func TestPreparationJournalFinalizeCommitRejectsOwnershipMismatch(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID: "source", Resource: resource, Ownership: OwnershipDepengine,
		Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe,
	}}}
	j := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	j, err := j.PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}
	owned := []OwnedResourceState{{Resource: resource, Ownership: OwnershipExternal}}
	if _, _, err := j.FinalizeCommit(p, owned, "tool-a"); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("FinalizeCommit() error = %v, want ownership mismatch", err)
	}
}

func TestPreparationJournalFinalizeCommitRequiresPersistableCommittingState(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe,
	}}}
	if _, _, err := NewPreparationJournal().FinalizeCommit(p, nil, "tool-a"); err == nil {
		t.Fatal("FinalizeCommit unexpectedly accepted unprepared journal")
	}
	ready := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	if _, _, err := ready.FinalizeCommit(p, nil, "tool-a"); err == nil {
		t.Fatal("FinalizeCommit unexpectedly accepted ready journal before PlanCommit")
	}
	if _, err := ready.MarkCommitted(p); err == nil {
		t.Fatal("MarkCommitted unexpectedly skipped committing state")
	}
}

func TestReleaseDependentResourcesPlansOnlyLastOwnedClaimsForRemoval(t *testing.T) {
	shared := ResourceIdentity{Kind: ResourceSource, Key: "repo:shared"}
	last := ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:last"}
	external := ResourceIdentity{Kind: ResourceSource, Key: "repo:external"}
	unrelated := ResourceIdentity{Kind: ResourceOther, Key: "cache:shared"}
	owned := []OwnedResourceState{
		{Resource: shared, Ownership: OwnershipDepengine, Dependents: []string{"tool-a", "tool-b"}},
		{Resource: last, Ownership: OwnershipDepengine, Dependents: []string{"tool-a"}},
		{Resource: external, Ownership: OwnershipExternal, Dependents: []string{"tool-a"}},
		{Resource: unrelated, Ownership: OwnershipDepengine, Dependents: []string{"tool-c"}},
	}
	original := make([]OwnedResourceState, len(owned))
	copy(original, owned)
	for i := range owned {
		original[i].Dependents = append([]string(nil), owned[i].Dependents...)
	}

	got, err := ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	wantUpdated := []OwnedResourceState{
		{Resource: unrelated, Ownership: OwnershipDepengine, Dependents: []string{"tool-c"}},
		{Resource: last, Ownership: OwnershipDepengine},
		{Resource: external, Ownership: OwnershipExternal},
		{Resource: shared, Ownership: OwnershipDepengine, Dependents: []string{"tool-b"}},
	}
	if !reflect.DeepEqual(got.Updated, wantUpdated) {
		t.Fatalf("updated = %#v, want %#v", got.Updated, wantUpdated)
	}
	if want := []ResourceIdentity{last}; !reflect.DeepEqual(got.Removable, want) {
		t.Fatalf("removable = %#v, want %#v", got.Removable, want)
	}
	if !reflect.DeepEqual(owned, original) {
		t.Fatalf("ReleaseDependentResources mutated input: got %#v, want %#v", owned, original)
	}
}

func TestReleaseDependentResourcesIsIdempotentForUnclaimedTool(t *testing.T) {
	owned := []OwnedResourceState{{
		Resource:   ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership:  OwnershipDepengine,
		Dependents: []string{"tool-b"},
	}}
	got, err := ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Updated, owned) {
		t.Fatalf("updated = %#v, want %#v", got.Updated, owned)
	}
	if len(got.Removable) != 0 {
		t.Fatalf("removable = %#v, want none", got.Removable)
	}
}

func TestCleanupReleasedResourcesPlansSafeCleanupAndRetainsIrreversibleState(t *testing.T) {
	source := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	prereq := ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "source", Resource: source, Ownership: OwnershipDepengine, Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe},
		{ID: "prereq", Resource: prereq, Ownership: OwnershipDepengine, Apply: mut("prereq-install"), Policy: RollbackRetain},
	}}
	owned := []OwnedResourceState{
		{Resource: source, Ownership: OwnershipDepengine, Dependents: []string{"tool-a"}},
		{Resource: prereq, Ownership: OwnershipDepengine, Dependents: []string{"tool-a"}},
	}
	release, err := ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := p.CleanupReleasedResources(release)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cleanup.Operations, []Operation{mut("source-remove")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %#v, want %#v", got, want)
	}
	if got, want := cleanup.Cleaned, []ResourceIdentity{source}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned = %#v, want %#v", got, want)
	}
	if got, want := cleanup.Retained, []ResourceIdentity{prereq}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retained = %#v, want %#v", got, want)
	}

	final, err := p.FinalizeReleasedResourceCleanup(release, cleanup)
	if err != nil {
		t.Fatal(err)
	}
	want := []OwnedResourceState{{Resource: prereq, Ownership: OwnershipDepengine}}
	if !reflect.DeepEqual(final, want) {
		t.Fatalf("final ownership = %#v, want %#v", final, want)
	}
	if len(release.Updated) != 2 {
		t.Fatal("cleanup mutated release ownership snapshot")
	}
}

func TestCleanupReleasedResourcesUsesReversePreparationOrder(t *testing.T) {
	first := ResourceIdentity{Kind: ResourceSource, Key: "repo:first"}
	second := ResourceIdentity{Kind: ResourceSource, Key: "repo:second"}
	p := PreparationPlan{Prepare: []PreparationMutation{
		{ID: "first", Resource: first, Ownership: OwnershipDepengine, Apply: mut("add-first"), Rollback: ptrOp(mut("remove-first")), Policy: RollbackSafe},
		{ID: "second", Resource: second, Ownership: OwnershipDepengine, Apply: mut("add-second"), Rollback: ptrOp(mut("remove-second")), Policy: RollbackSafe},
	}}
	release := ResourceReleaseDecision{
		Updated: []OwnedResourceState{
			{Resource: first, Ownership: OwnershipDepengine},
			{Resource: second, Ownership: OwnershipDepengine},
		},
		Removable: []ResourceIdentity{first, second},
	}
	cleanup, err := p.CleanupReleasedResources(release)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cleanup.Operations, []Operation{mut("remove-second"), mut("remove-first")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %#v, want %#v", got, want)
	}
	if got, want := cleanup.Cleaned, []ResourceIdentity{second, first}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned = %#v, want %#v", got, want)
	}
}

func TestCleanupReleasedResourcesFailsClosedForUnrepresentedResource(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:unknown"}
	release := ResourceReleaseDecision{
		Updated:   []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine}},
		Removable: []ResourceIdentity{resource},
	}
	if _, err := (PreparationPlan{}).CleanupReleasedResources(release); err == nil || !strings.Contains(err.Error(), "not represented") {
		t.Fatalf("CleanupReleasedResources() error = %v, want unrepresented resource rejection", err)
	}
}

func TestFinalizeReleasedResourceCleanupRejectsForgedDecision(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID: "prereq", Resource: resource, Ownership: OwnershipDepengine,
		Apply: mut("install-prereq"), Policy: RollbackRetain,
	}}}
	release := ResourceReleaseDecision{
		Updated:   []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine}},
		Removable: []ResourceIdentity{resource},
	}
	forged := ResourceCleanupDecision{Cleaned: []ResourceIdentity{resource}}
	if _, err := p.FinalizeReleasedResourceCleanup(release, forged); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("FinalizeReleasedResourceCleanup() error = %v, want forged decision rejection", err)
	}
}

func TestFinalizeReleasedResourceCleanupRejectsForgedOperations(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID: "source", Resource: resource, Ownership: OwnershipDepengine,
		Apply: mut("source-add"), Rollback: ptrOp(mut("source-remove")), Policy: RollbackSafe,
	}}}
	release := ResourceReleaseDecision{
		Updated:   []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine}},
		Removable: []ResourceIdentity{resource},
	}
	want, err := p.CleanupReleasedResources(release)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		cleanup ResourceCleanupDecision
	}{
		{name: "omitted operation", cleanup: ResourceCleanupDecision{Cleaned: append([]ResourceIdentity(nil), want.Cleaned...)}},
		{name: "different operation", cleanup: ResourceCleanupDecision{Operations: []Operation{mut("different-remove")}, Cleaned: append([]ResourceIdentity(nil), want.Cleaned...)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.FinalizeReleasedResourceCleanup(release, tc.cleanup); err == nil || !strings.Contains(err.Error(), "does not match") {
				t.Fatalf("FinalizeReleasedResourceCleanup() error = %v, want forged operation rejection", err)
			}
		})
	}
}

func TestValidateOwnedResourceSnapshotRequiresCanonicalOrderAndUniqueness(t *testing.T) {
	prereq := OwnedResourceState{
		Resource:   ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"},
		Ownership:  OwnershipDepengine,
		Dependents: []string{"tool-a"},
	}
	source := OwnedResourceState{
		Resource:   ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership:  OwnershipDepengine,
		Dependents: []string{"tool-a", "tool-b"},
	}
	if err := ValidateOwnedResourceSnapshot([]OwnedResourceState{prereq, source}); err != nil {
		t.Fatalf("canonical snapshot rejected: %v", err)
	}
	if err := ValidateOwnedResourceSnapshot([]OwnedResourceState{source, prereq}); err == nil {
		t.Fatal("out-of-order ownership snapshot unexpectedly accepted")
	}
	if err := ValidateOwnedResourceSnapshot([]OwnedResourceState{prereq, prereq}); err == nil {
		t.Fatal("duplicate ownership snapshot unexpectedly accepted")
	}
}

func TestPreparationRecoveryRollsBackUncommittedPrefix(t *testing.T) {
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
	journal := recordPreparationMutation(t, NewPreparationJournal(), p, "source")

	got, err := journal.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != RecoveryRollbackPreparation {
		t.Fatalf("action = %q, want %q", got.Action, RecoveryRollbackPreparation)
	}
	if want := []Operation{mut("source-remove")}; !reflect.DeepEqual(got.Rollback.Operations, want) {
		t.Fatalf("rollback operations = %#v, want %#v", got.Rollback.Operations, want)
	}
}

func TestPreparationJournalResolveApplyingRequiresExplicitOutcome(t *testing.T) {
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

	applying, err := NewPreparationJournal().PlanApply(p, "source")
	if err != nil {
		t.Fatal(err)
	}

	notApplied, err := applying.ResolveApplying(p, "source", PreparationMutationNotApplied)
	if err != nil {
		t.Fatal(err)
	}
	if notApplied.Status != PreparationPending || notApplied.Applying != "" || len(notApplied.Applied) != 0 {
		t.Fatalf("not-applied resolution = %#v, want pending journal with no progress", notApplied)
	}
	retry, err := notApplied.PlanApply(p, "source")
	if err != nil {
		t.Fatalf("retry PlanApply: %v", err)
	}
	applied, err := retry.ResolveApplying(p, "source", PreparationMutationApplied)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Status != PreparationPreparing || applied.Applying != "" || !reflect.DeepEqual(applied.Applied, []string{"source"}) {
		t.Fatalf("applied resolution = %#v, want confirmed source prefix", applied)
	}

	second, err := applied.PlanApply(p, "prereq")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := second.ResolveApplying(p, "prereq", PreparationMutationApplied)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != PreparationReady || !reflect.DeepEqual(ready.Applied, []string{"source", "prereq"}) {
		t.Fatalf("final resolution = %#v, want ready journal", ready)
	}

	if _, err := applying.ResolveApplying(p, "prereq", PreparationMutationApplied); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong-id resolution error = %v, want mismatch", err)
	}
	if _, err := applying.ResolveApplying(p, "source", PreparationMutationOutcome("unknown")); err == nil || !strings.Contains(err.Error(), "invalid preparation mutation outcome") {
		t.Fatalf("invalid-outcome resolution error = %v", err)
	}
	if _, err := notApplied.ResolveApplying(p, "source", PreparationMutationApplied); err == nil || !strings.Contains(err.Error(), "not in flight") {
		t.Fatalf("non-applying resolution error = %v", err)
	}
}

func TestPreparationJournalResolveApplyingNotAppliedPreservesConfirmedPrefix(t *testing.T) {
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

	j := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	applying, err := j.PlanApply(p, "prereq")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := applying.ResolveApplying(p, "prereq", PreparationMutationNotApplied)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != PreparationPreparing || resolved.Applying != "" || !reflect.DeepEqual(resolved.Applied, []string{"source"}) {
		t.Fatalf("resolution = %#v, want preparing with source prefix preserved", resolved)
	}
}

func TestPreparationRecoveryBlocksAmbiguousInFlightPrepareMutation(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	applying, err := NewPreparationJournal().PlanApply(p, "source")
	if err != nil {
		t.Fatal(err)
	}

	got, err := applying.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != RecoveryBlocked {
		t.Fatalf("action = %q, want %q", got.Action, RecoveryBlocked)
	}
	if !strings.Contains(got.Detail, "ambiguous") || !strings.Contains(got.Detail, "source") {
		t.Fatalf("detail = %q, want in-flight mutation explanation", got.Detail)
	}
	if _, _, err := applying.PlanRollback(p, nil); err == nil || !strings.Contains(err.Error(), "mutation is in progress") {
		t.Fatalf("PlanRollback() error = %v, want in-flight mutation rejection", err)
	}
}

func TestPreparationRecoveryResumesPersistedRollback(t *testing.T) {
	p := PreparationPlan{Prepare: []PreparationMutation{{
		ID:        "source",
		Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
		Ownership: OwnershipDepengine,
		Apply:     mut("source-add"),
		Rollback:  ptrOp(mut("source-remove")),
		Policy:    RollbackSafe,
	}}}
	ready := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	rolling, _, err := ready.PlanRollback(p, nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := rolling.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != RecoveryResumeRollback {
		t.Fatalf("action = %q, want %q", got.Action, RecoveryResumeRollback)
	}
	if want := []Operation{mut("source-remove")}; !reflect.DeepEqual(got.Rollback.Operations, want) {
		t.Fatalf("rollback operations = %#v, want %#v", got.Rollback.Operations, want)
	}
}

func TestPreparationRecoveryReconcilesAmbiguousCommit(t *testing.T) {
	p := PreparationPlan{
		Prepare: []PreparationMutation{{
			ID:        "source",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-add"),
			Rollback:  ptrOp(mut("source-remove")),
			Policy:    RollbackSafe,
		}},
		Commit: []Operation{mut("install-package")},
	}
	ready := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	committing, err := ready.PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}

	probe, err := committing.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Action != RecoveryReconcileCommit {
		t.Fatalf("action = %q, want %q", probe.Action, RecoveryReconcileCommit)
	}

	desired := ResolvedIdentity{Package: "demo", Version: "1.2.3"}
	observation := Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Package: "demo", Version: "1.2.3"},
		KnownFields: []IdentityField{FieldPackage, FieldVersion},
	}
	finalize, err := committing.RecoveryFor(p, nil, &desired, &observation)
	if err != nil {
		t.Fatal(err)
	}
	if finalize.Action != RecoveryFinalizeCommit {
		t.Fatalf("action = %q, want %q", finalize.Action, RecoveryFinalizeCommit)
	}
	if finalize.Verification == nil || finalize.Verification.State != StateSatisfied {
		t.Fatalf("verification = %#v, want satisfied", finalize.Verification)
	}
}

func TestPreparationCommitNotAppliedResolutionEnablesRollbackWithoutReplay(t *testing.T) {
	p := PreparationPlan{
		Prepare: []PreparationMutation{{
			ID:        "source",
			Resource:  ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"},
			Ownership: OwnershipDepengine,
			Apply:     mut("source-add"),
			Rollback:  ptrOp(mut("source-remove")),
			Policy:    RollbackSafe,
		}},
		Commit: []Operation{mut("install-package")},
	}
	ready := recordPreparationMutation(t, NewPreparationJournal(), p, "source")
	committing, err := ready.PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := committing.ResolveCommitNotApplied(p)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != PreparationReady {
		t.Fatalf("status = %q, want %q", resolved.Status, PreparationReady)
	}
	if !reflect.DeepEqual(resolved.Applied, []string{"source"}) {
		t.Fatalf("applied = %#v, want source prefix preserved", resolved.Applied)
	}

	recovery, err := resolved.RecoveryFor(p, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Action != RecoveryRollbackPreparation {
		t.Fatalf("action = %q, want %q", recovery.Action, RecoveryRollbackPreparation)
	}
	if !reflect.DeepEqual(recovery.Rollback.OperationIDs, []string{"source"}) || !reflect.DeepEqual(recovery.Rollback.Operations, []Operation{mut("source-remove")}) {
		t.Fatalf("rollback = %#v, want source compensation", recovery.Rollback)
	}

	if _, err := resolved.ResolveCommitNotApplied(p); err == nil || !strings.Contains(err.Error(), "not in progress") {
		t.Fatalf("ResolveCommitNotApplied from ready error = %v, want state rejection", err)
	}
}

func TestPreparationRecoveryBlocksNonSatisfiedCommitOutcome(t *testing.T) {
	p := PreparationPlan{Commit: []Operation{mut("install-package")}}
	committing, err := NewPreparationJournal().PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}
	desired := ResolvedIdentity{Package: "demo", Version: "1.2.3"}

	for name, observation := range map[string]Observation{
		"absent": {Presence: PresenceAbsent},
		"drifted": {
			Presence:    PresencePresent,
			Identity:    ObservedIdentity{Package: "demo", Version: "1.2.2"},
			KnownFields: []IdentityField{FieldPackage, FieldVersion},
		},
		"unknown": {Presence: PresenceUnknown, Detail: "probe unavailable"},
		"broken":  {Presence: PresenceBroken, Detail: "probe failed"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := committing.RecoveryFor(p, nil, &desired, &observation)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != RecoveryBlocked {
				t.Fatalf("action = %q, want %q", got.Action, RecoveryBlocked)
			}
			if got.Verification == nil {
				t.Fatal("verification is nil")
			}
		})
	}
}

func TestPreparationRecoveryRejectsTerminalJournalAndPartialReconciliation(t *testing.T) {
	p := PreparationPlan{}
	committing, err := NewPreparationJournal().PlanCommit(p)
	if err != nil {
		t.Fatal(err)
	}
	desired := ResolvedIdentity{Package: "demo"}
	if _, err := committing.RecoveryFor(p, nil, &desired, nil); err == nil {
		t.Fatal("RecoveryFor unexpectedly accepted desired identity without observation")
	}

	committed, err := committing.MarkCommitted(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := committed.RecoveryFor(p, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("RecoveryFor terminal error = %v, want terminal rejection", err)
	}
}

func TestClaimResourceUsesTracksCreatedAndSharedResources(t *testing.T) {
	source := ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}
	owned, err := ClaimResourceUses(nil, "tool-a", []ResourceUse{{Resource: source, Created: true}})
	if err != nil {
		t.Fatal(err)
	}
	want := []OwnedResourceState{{Resource: source, Ownership: OwnershipDepengine, Dependents: []string{"tool-a"}}}
	if !reflect.DeepEqual(owned, want) {
		t.Fatalf("first claim = %#v, want %#v", owned, want)
	}

	owned, err = ClaimResourceUses(owned, "tool-b", []ResourceUse{{Resource: source}})
	if err != nil {
		t.Fatal(err)
	}
	want[0].Dependents = []string{"tool-a", "tool-b"}
	if !reflect.DeepEqual(owned, want) {
		t.Fatalf("shared claim = %#v, want %#v", owned, want)
	}
}

func TestClaimResourceUsesClassifiesPreexistingAsExternalAndCanAdoptRecreatedResource(t *testing.T) {
	source := ResourceIdentity{Kind: ResourceSource, Key: "repo:external"}
	owned, err := ClaimResourceUses(nil, "tool-a", []ResourceUse{{Resource: source}})
	if err != nil {
		t.Fatal(err)
	}
	if got := owned[0].Ownership; got != OwnershipExternal {
		t.Fatalf("preexisting ownership = %q, want external", got)
	}

	owned, err = ClaimResourceUses(owned, "tool-b", []ResourceUse{{Resource: source, Created: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got := owned[0].Ownership; got != OwnershipDepengine {
		t.Fatalf("recreated ownership = %q, want depengine", got)
	}
	if want := []string{"tool-a", "tool-b"}; !reflect.DeepEqual(owned[0].Dependents, want) {
		t.Fatalf("dependents = %#v, want %#v", owned[0].Dependents, want)
	}
}

func TestClaimResourceUsesRejectsDuplicateObservation(t *testing.T) {
	resource := ResourceIdentity{Kind: ResourceSource, Key: "repo:dup"}
	_, err := ClaimResourceUses(nil, "tool-a", []ResourceUse{{Resource: resource}, {Resource: resource, Created: true}})
	if err == nil || !strings.Contains(err.Error(), "duplicate resource use") {
		t.Fatalf("error = %v, want duplicate resource use rejection", err)
	}
}

func TestPrerequisiteResourceRoundTrip(t *testing.T) {
	resource, err := PrerequisiteResource("compiler")
	if err != nil {
		t.Fatal(err)
	}
	want := ResourceIdentity{Kind: ResourcePrerequisite, Key: "tool:compiler"}
	if resource != want {
		t.Fatalf("resource = %#v, want %#v", resource, want)
	}
	name, err := PrerequisiteToolName(resource)
	if err != nil {
		t.Fatal(err)
	}
	if name != "compiler" {
		t.Fatalf("name = %q, want compiler", name)
	}
}

func TestPrerequisiteResourceRejectsInvalidIdentity(t *testing.T) {
	for _, name := range []string{"", " compiler", "compiler ", "bad\x00name"} {
		if _, err := PrerequisiteResource(name); err == nil {
			t.Fatalf("PrerequisiteResource(%q) unexpectedly succeeded", name)
		}
	}
	if _, err := PrerequisiteToolName(ResourceIdentity{Kind: ResourceSource, Key: "tool:compiler"}); err == nil {
		t.Fatal("PrerequisiteToolName unexpectedly accepted source resource")
	}
	if _, err := PrerequisiteToolName(ResourceIdentity{Kind: ResourcePrerequisite, Key: "pkg:compiler"}); err == nil {
		t.Fatal("PrerequisiteToolName unexpectedly accepted non-tool prerequisite namespace")
	}
}

func TestFinalizeReleasedResourceDropsOnlyApprovedZeroRefOwnedResource(t *testing.T) {
	prerequisite, err := PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	other, err := PrerequisiteResource("other")
	if err != nil {
		t.Fatal(err)
	}
	release := ResourceReleaseDecision{
		Updated: []OwnedResourceState{
			{Resource: prerequisite, Ownership: OwnershipDepengine},
			{Resource: other, Ownership: OwnershipExternal},
		},
		Removable: []ResourceIdentity{prerequisite},
	}
	got, err := FinalizeReleasedResource(release, prerequisite)
	if err != nil {
		t.Fatal(err)
	}
	want := []OwnedResourceState{{Resource: other, Ownership: OwnershipExternal}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FinalizeReleasedResource() = %#v, want %#v", got, want)
	}
}

func TestFinalizeReleasedResourceRejectsUnapprovedOrReferencedResource(t *testing.T) {
	resource, err := PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		release ResourceReleaseDecision
	}{
		{
			name: "not removable",
			release: ResourceReleaseDecision{
				Updated: []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine}},
			},
		},
		{
			name: "still referenced",
			release: ResourceReleaseDecision{
				Updated:   []OwnedResourceState{{Resource: resource, Ownership: OwnershipDepengine, Dependents: []string{"owner"}}},
				Removable: []ResourceIdentity{resource},
			},
		},
		{
			name: "external",
			release: ResourceReleaseDecision{
				Updated:   []OwnedResourceState{{Resource: resource, Ownership: OwnershipExternal}},
				Removable: []ResourceIdentity{resource},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FinalizeReleasedResource(tc.release, resource); err == nil {
				t.Fatal("FinalizeReleasedResource() unexpectedly succeeded")
			}
		})
	}
}
