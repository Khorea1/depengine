package state

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func persistPreparationMutation(t *testing.T, ls *LockedState, key string, p plan.PreparationPlan, id string) plan.PreparationJournal {
	t.Helper()
	if _, err := ls.PlanPreparationApply(key, p, id); err != nil {
		t.Fatalf("PlanPreparationApply(%q): %v", id, err)
	}
	got, err := ls.RecordPreparationApplied(key, p, id)
	if err != nil {
		t.Fatalf("RecordPreparationApplied(%q): %v", id, err)
	}
	return got
}

func persistRollbackMutation(t *testing.T, ls *LockedState, key string, p plan.PreparationPlan, id string) plan.PreparationJournal {
	t.Helper()
	if _, _, err := ls.PlanPreparationRollbackApply(key, p, id); err != nil {
		t.Fatalf("PlanPreparationRollbackApply(%q): %v", id, err)
	}
	got, err := ls.RecordPreparationRollbackApplied(key, p, id)
	if err != nil {
		t.Fatalf("RecordPreparationRollbackApplied(%q): %v", id, err)
	}
	return got
}

func preparationTransactionPlan(resourceKey string) plan.PreparationPlan {
	rollback := plan.Operation{Kind: "remove-source", Effect: plan.EffectMutation}
	return plan.PreparationPlan{
		Prepare: []plan.PreparationMutation{{
			ID:        "source-add",
			Resource:  plan.ResourceIdentity{Kind: plan.ResourceSource, Key: resourceKey},
			Ownership: plan.OwnershipDepengine,
			Apply:     plan.Operation{Kind: "add-source", Effect: plan.EffectMutation},
			Rollback:  &rollback,
			Policy:    plan.RollbackSafe,
		}},
		Commit: []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}},
	}
}

func TestLockedStatePersistsExactPreparationPlanForRecovery(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:stable")
	p.Probe = []plan.Operation{{Kind: "check-source", Description: "repo:stable", Effect: plan.EffectReadOnly}}
	p.Prepare[0].Apply.Command = []string{"sourcectl", "add", "repo:stable"}
	p.Prepare[0].Apply.ArbitraryCode = true
	const key = "tool-a/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.BeginPreparation(key, p); err != nil {
		ls.Close()
		t.Fatal(err)
	}

	// Mutating the caller's plan after BeginPreparation must not alias the
	// durable transaction snapshot.
	p.Prepare[0].Resource.Key = "repo:changed"
	p.Prepare[0].Apply.Command[2] = "repo:changed"
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	ls, err = LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	storedPlan, journal, err := ls.PreparationTransaction(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Status != plan.PreparationPending {
		t.Fatalf("journal status = %q, want pending", journal.Status)
	}
	if got := storedPlan.Prepare[0].Resource.Key; got != "repo:stable" {
		t.Fatalf("stored resource key = %q, want repo:stable", got)
	}
	if got := storedPlan.Prepare[0].Apply.Command[2]; got != "repo:stable" {
		t.Fatalf("stored apply command = %q, want repo:stable", got)
	}

	// Lifecycle calls must be bound to that exact stored plan. A newly derived
	// plan from changed host state cannot silently replace the WAL dependency.
	if _, err := ls.PlanPreparationApply(key, p, "source-add"); err == nil || !strings.Contains(err.Error(), "changed since transaction began") {
		t.Fatalf("PlanPreparationApply changed plan error = %v", err)
	}
	if _, err := ls.PlanPreparationApply(key, storedPlan, "source-add"); err != nil {
		t.Fatalf("PlanPreparationApply stored plan: %v", err)
	}
}

func TestLockedStatePersistsPreparationCommitBoundaryAndOwnership(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:stable")
	const key = "tool-a/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()

	if got, err := ls.BeginPreparation(key, p); err != nil || got.Status != plan.PreparationPending {
		t.Fatalf("BeginPreparation() = %#v, %v", got, err)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationPending)

	if got, err := ls.PlanPreparationApply(key, p, "source-add"); err != nil || got.Status != plan.PreparationApplying || got.Applying != "source-add" {
		t.Fatalf("PlanPreparationApply() = %#v, %v", got, err)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationApplying)
	if got, err := ls.RecordPreparationApplied(key, p, "source-add"); err != nil || got.Status != plan.PreparationReady || got.Applying != "" {
		t.Fatalf("RecordPreparationApplied() = %#v, %v", got, err)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationReady)

	committing, err := ls.PlanPreparationCommit(key, p)
	if err != nil {
		t.Fatal(err)
	}
	if committing.Status != plan.PreparationCommitting {
		t.Fatalf("commit status = %q, want %q", committing.Status, plan.PreparationCommitting)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationCommitting)
	if _, _, err := ls.PlanPreparationRollback(key, p); err == nil || !strings.Contains(err.Error(), "commit is in progress") {
		t.Fatalf("PlanPreparationRollback() error = %v, want commit-in-progress rejection", err)
	}

	committed, err := ls.FinalizePreparationCommit(key, p, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	if committed.Status != plan.PreparationCommitted {
		t.Fatalf("final status = %q, want %q", committed.Status, plan.PreparationCommitted)
	}
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.PreparationJournals[key]; ok {
		t.Fatalf("completed preparation journal %q was retained", key)
	}
	if _, ok := loaded.PreparationPlans[key]; ok {
		t.Fatalf("completed preparation plan %q was retained", key)
	}
	wantOwned := []plan.OwnedResourceState{{
		Resource:   plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"},
		Ownership:  plan.OwnershipDepengine,
		Dependents: []string{"tool-a"},
	}}
	if !reflect.DeepEqual(loaded.OwnedResources, wantOwned) {
		t.Fatalf("owned resources = %#v, want %#v", loaded.OwnedResources, wantOwned)
	}
}

func TestLockedStatePersistsRollbackBoundaryBeforeCompensation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:temporary")
	const key = "tool-b/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	persistPreparationMutation(t, ls, key, p, "source-add")

	rollingBack, decision, err := ls.PlanPreparationRollback(key, p)
	if err != nil {
		t.Fatal(err)
	}
	if rollingBack.Status != plan.PreparationRollingBack {
		t.Fatalf("rollback status = %q, want %q", rollingBack.Status, plan.PreparationRollingBack)
	}
	if got, want := len(decision.Operations), 1; got != want {
		t.Fatalf("rollback operations = %d, want %d", got, want)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationRollingBack)
	persistRollbackMutation(t, ls, key, p, "source-add")

	rolledBack, err := ls.FinalizePreparationRollback(key, p)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Status != plan.PreparationRolledBack {
		t.Fatalf("final status = %q, want %q", rolledBack.Status, plan.PreparationRolledBack)
	}
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.PreparationJournals[key]; ok {
		t.Fatalf("completed rollback journal %q was retained", key)
	}
	if _, ok := loaded.PreparationPlans[key]; ok {
		t.Fatalf("completed rollback plan %q was retained", key)
	}
	if len(loaded.OwnedResources) != 0 {
		t.Fatalf("owned resources = %#v, want none", loaded.OwnedResources)
	}
}

func TestLockedStateRollbackRecoverySkipsDurablyCompletedCompensation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	removeSource := plan.Operation{Kind: "remove-source", Effect: plan.EffectMutation}
	removePrereq := plan.Operation{Kind: "remove-prereq", Effect: plan.EffectMutation}
	p := plan.PreparationPlan{Prepare: []plan.PreparationMutation{
		{ID: "source-add", Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:partial"}, Ownership: plan.OwnershipDepengine, Apply: plan.Operation{Kind: "add-source", Effect: plan.EffectMutation}, Rollback: &removeSource, Policy: plan.RollbackSafe},
		{ID: "prereq-add", Resource: plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "pkg:partial"}, Ownership: plan.OwnershipDepengine, Apply: plan.Operation{Kind: "add-prereq", Effect: plan.EffectMutation}, Rollback: &removePrereq, Policy: plan.RollbackSafe},
	}}
	const key = "tool-partial/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	persistPreparationMutation(t, ls, key, p, "source-add")
	persistPreparationMutation(t, ls, key, p, "prereq-add")
	if _, _, err := ls.PlanPreparationRollback(key, p); err != nil {
		t.Fatal(err)
	}
	persistRollbackMutation(t, ls, key, p, "prereq-add")
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	ls, err = LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	decision, err := ls.PreparationRecovery(key, p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != plan.RecoveryResumeRollback || !reflect.DeepEqual(decision.Rollback.OperationIDs, []string{"source-add"}) || len(decision.Rollback.Operations) != 1 || decision.Rollback.Operations[0].Kind != "remove-source" {
		t.Fatalf("recovery = %#v, want only remaining source compensation", decision)
	}
	persistRollbackMutation(t, ls, key, p, "source-add")
	if _, err := ls.FinalizePreparationRollback(key, p); err != nil {
		t.Fatal(err)
	}
}

func TestLockedStateRollbackWALSurvivesRestartAndRequiresExplicitResolution(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:rollback-ambiguous")
	const key = "tool-rollback/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	persistPreparationMutation(t, ls, key, p, "source-add")
	if _, _, err := ls.PlanPreparationRollback(key, p); err != nil {
		t.Fatal(err)
	}
	planned, op, err := ls.PlanPreparationRollbackApply(key, p, "source-add")
	if err != nil {
		t.Fatal(err)
	}
	if planned.RollbackApplying != "source-add" || op.Kind != "remove-source" {
		t.Fatalf("planned rollback = %#v op=%#v", planned, op)
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	ls, err = LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	decision, err := ls.PreparationRecovery(key, p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != plan.RecoveryBlocked || !strings.Contains(decision.Detail, "source-add") {
		t.Fatalf("recovery = %#v, want blocked ambiguous rollback", decision)
	}
	if _, err := ls.FinalizePreparationRollback(key, p); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("FinalizePreparationRollback error = %v, want ambiguity rejection", err)
	}

	resolved, err := ls.ResolvePreparationRollbackApplying(key, p, "source-add", plan.PreparationMutationNotApplied)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RollbackApplying != "" || len(resolved.RollbackApplied) != 0 {
		t.Fatalf("not_applied resolution = %#v", resolved)
	}
	persistRollbackMutation(t, ls, key, p, "source-add")
	if _, err := ls.FinalizePreparationRollback(key, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := loaded.PreparationJournals[key]; exists {
		t.Fatalf("completed rollback journal %q remains active", key)
	}
}

func TestLockedStateRestoresInMemoryJournalWhenPersistenceFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:stable")
	const key = "tool-a/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}

	// Make unrelated persisted state invalid so the next Save fails after the
	// journal transition has been staged in memory.
	ls.state.OwnedResources = []plan.OwnedResourceState{{
		Resource:   plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:broken"},
		Ownership:  plan.OwnershipDepengine,
		Dependents: []string{"tool-b", "tool-a"},
	}}
	if _, err := ls.PlanPreparationApply(key, p, "source-add"); err == nil {
		t.Fatal("PlanPreparationApply unexpectedly succeeded with invalid state")
	}
	if got := ls.state.PreparationJournals[key].Status; got != plan.PreparationPending {
		t.Fatalf("in-memory status after failed save = %q, want %q", got, plan.PreparationPending)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationPending)
}

func assertPersistedPreparationStatus(t *testing.T, key string, want plan.PreparationStatus) {
	t.Helper()
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	journal, ok := loaded.PreparationJournals[key]
	if !ok {
		t.Fatalf("persisted preparation journal %q missing", key)
	}
	if journal.Status != want {
		t.Fatalf("persisted status = %q, want %q", journal.Status, want)
	}
}

func TestLockedStateRecoveryBlocksPersistedInFlightPreparationMutation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:ambiguous")
	const key = "tool-ambiguous/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	planned, err := ls.PlanPreparationApply(key, p, "source-add")
	if err != nil {
		t.Fatal(err)
	}
	if planned.Status != plan.PreparationApplying || planned.Applying != "source-add" {
		t.Fatalf("planned journal = %#v", planned)
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash after the source mutation may have started, but before
	// its success was durably confirmed. Recovery must not infer either side.
	ls, err = LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	decision, err := ls.PreparationRecovery(key, p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != plan.RecoveryBlocked {
		t.Fatalf("recovery action = %q, want %q", decision.Action, plan.RecoveryBlocked)
	}
	if !strings.Contains(decision.Detail, "ambiguous") || !strings.Contains(decision.Detail, "source-add") {
		t.Fatalf("recovery detail = %q", decision.Detail)
	}
	if _, _, err := ls.PlanPreparationRollback(key, p); err == nil || !strings.Contains(err.Error(), "mutation is in progress") {
		t.Fatalf("PlanPreparationRollback() error = %v, want in-flight rejection", err)
	}
	if got := ls.state.PreparationJournals[key]; got.Status != plan.PreparationApplying || got.Applying != "source-add" {
		t.Fatalf("recovery mutated journal = %#v", got)
	}
}

func TestLockedStateResolvesPersistedApplyingOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome plan.PreparationMutationOutcome
		status  plan.PreparationStatus
		applied []string
	}{
		{name: "applied", outcome: plan.PreparationMutationApplied, status: plan.PreparationReady, applied: []string{"source-add"}},
		{name: "not-applied", outcome: plan.PreparationMutationNotApplied, status: plan.PreparationPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			p := preparationTransactionPlan("repo:ambiguous")
			const key = "tool-ambiguous/native#0"

			ls, err := LoadLocked()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ls.BeginPreparation(key, p); err != nil {
				t.Fatal(err)
			}
			if _, err := ls.PlanPreparationApply(key, p, "source-add"); err != nil {
				t.Fatal(err)
			}
			if err := ls.Close(); err != nil {
				t.Fatal(err)
			}

			// Simulate restart before the host-side result was confirmed. The
			// explicit outcome is external evidence; state must never infer it.
			ls, err = LoadLocked()
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := ls.ResolvePreparationApplying(key, p, "source-add", tc.outcome)
			if err != nil {
				ls.Close()
				t.Fatal(err)
			}
			if resolved.Status != tc.status || resolved.Applying != "" || !reflect.DeepEqual(resolved.Applied, tc.applied) {
				ls.Close()
				t.Fatalf("resolved journal = %#v, want status=%q applied=%v", resolved, tc.status, tc.applied)
			}
			if err := ls.Close(); err != nil {
				t.Fatal(err)
			}

			loaded, err := LoadFrom(DefaultPath())
			if err != nil {
				t.Fatal(err)
			}
			got := loaded.PreparationJournals[key]
			if got.Status != tc.status || got.Applying != "" || !reflect.DeepEqual(got.Applied, tc.applied) {
				t.Fatalf("persisted journal = %#v, want status=%q applied=%v", got, tc.status, tc.applied)
			}
		})
	}
}

func TestLockedStateResolveApplyingRejectsUnprovenOutcome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:ambiguous")
	const key = "tool-ambiguous/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.PlanPreparationApply(key, p, "source-add"); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.ResolvePreparationApplying(key, p, "source-add", plan.PreparationMutationOutcome("unknown")); err == nil {
		t.Fatal("ResolvePreparationApplying unexpectedly accepted unknown outcome")
	}
	got := ls.state.PreparationJournals[key]
	if got.Status != plan.PreparationApplying || got.Applying != "source-add" {
		t.Fatalf("failed resolution mutated journal = %#v", got)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationApplying)
}

func TestLockedStateRecoveryCanResolveCommitAsNotAppliedAndRollback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:not-applied")
	const key = "tool-not-applied/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	persistPreparationMutation(t, ls, key, p, "source-add")
	if _, err := ls.PlanPreparationCommit(key, p); err != nil {
		t.Fatal(err)
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart with an ambiguous commit. A normal desired-state probe that says
	// the target is absent remains insufficient to infer that commit had no
	// side effects.
	ls, err = LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	desired := plan.ResolvedIdentity{Package: "demo", Version: "1.2.3"}
	absent := plan.Observation{Presence: plan.PresenceAbsent}
	decision, err := ls.PreparationRecovery(key, p, &desired, &absent)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != plan.RecoveryBlocked {
		t.Fatalf("recovery action = %q, want %q", decision.Action, plan.RecoveryBlocked)
	}
	if got := ls.state.PreparationJournals[key].Status; got != plan.PreparationCommitting {
		t.Fatalf("blocked recovery mutated journal status to %q", got)
	}

	resolved, err := ls.ResolvePreparationCommitNotApplied(key, p)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != plan.PreparationReady {
		t.Fatalf("resolved status = %q, want %q", resolved.Status, plan.PreparationReady)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationReady)

	rollback, err := ls.PreparationRecovery(key, p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Action != plan.RecoveryRollbackPreparation {
		t.Fatalf("recovery action after resolution = %q, want %q", rollback.Action, plan.RecoveryRollbackPreparation)
	}
	if !reflect.DeepEqual(rollback.Rollback.OperationIDs, []string{"source-add"}) {
		t.Fatalf("rollback operation ids = %#v, want source-add", rollback.Rollback.OperationIDs)
	}

	if _, _, err := ls.PlanPreparationRollback(key, p); err != nil {
		t.Fatalf("PlanPreparationRollback after not-applied resolution: %v", err)
	}
	assertPersistedPreparationStatus(t, key, plan.PreparationRollingBack)
}

func TestLockedStateRecoveryReconcilesPersistedCommitWithoutReplayingIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:stable")
	const key = "tool-recovery/native#0"

	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.BeginPreparation(key, p); err != nil {
		t.Fatal(err)
	}
	persistPreparationMutation(t, ls, key, p, "source-add")
	if _, err := ls.PlanPreparationCommit(key, p); err != nil {
		t.Fatal(err)
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a restart after commit began but before ownership was finalized.
	ls, err = LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()

	probe, err := ls.PreparationRecovery(key, p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Action != plan.RecoveryReconcileCommit {
		t.Fatalf("recovery action = %q, want %q", probe.Action, plan.RecoveryReconcileCommit)
	}
	if got := ls.state.PreparationJournals[key].Status; got != plan.PreparationCommitting {
		t.Fatalf("recovery probe mutated journal status to %q", got)
	}

	desired := plan.ResolvedIdentity{Package: "demo", Version: "1.2.3"}
	observation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "demo", Version: "1.2.3"},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}
	decision, err := ls.PreparationRecovery(key, p, &desired, &observation)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != plan.RecoveryFinalizeCommit {
		t.Fatalf("recovery action = %q, want %q", decision.Action, plan.RecoveryFinalizeCommit)
	}

	if _, err := ls.FinalizePreparationCommit(key, p, "tool-recovery"); err != nil {
		t.Fatal(err)
	}
	if _, exists := ls.state.PreparationJournals[key]; exists {
		t.Fatalf("finalized recovery retained active journal %q", key)
	}
	if got := ls.state.OwnedResources; len(got) != 1 || !reflect.DeepEqual(got[0].Dependents, []string{"tool-recovery"}) {
		t.Fatalf("owned resources after recovery = %#v", got)
	}
}
