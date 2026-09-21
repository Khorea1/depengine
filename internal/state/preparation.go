package state

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
)

// BeginPreparation creates and durably records an empty preparation journal
// before any candidate-specific prepare mutation is executed.
func (ls *LockedState) BeginPreparation(key string, p plan.PreparationPlan) (plan.PreparationJournal, error) {
	if ls == nil || ls.state == nil {
		return plan.PreparationJournal{}, errors.New("locked state is nil")
	}
	if err := validatePreparationJournalKey(key); err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := p.Validate(); err != nil {
		return plan.PreparationJournal{}, fmt.Errorf("preparation plan: %w", err)
	}
	if ls.state.PreparationPlans == nil {
		ls.state.PreparationPlans = make(map[string]plan.PreparationPlan)
	}
	if ls.state.PreparationJournals == nil {
		ls.state.PreparationJournals = make(map[string]plan.PreparationJournal)
	}
	if _, exists := ls.state.PreparationJournals[key]; exists {
		return plan.PreparationJournal{}, fmt.Errorf("preparation journal %q already exists", key)
	}
	if _, exists := ls.state.PreparationPlans[key]; exists {
		return plan.PreparationJournal{}, fmt.Errorf("preparation plan %q already exists without an active journal", key)
	}
	journal := plan.NewPreparationJournal()
	if err := journal.Validate(p); err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationStart(key, p, journal); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(journal), nil
}

// PlanPreparationApply durably marks one prepare mutation as in flight before
// the host mutation begins. This is the write-ahead boundary that prevents a
// crash after host mutation from being mistaken for "mutation never started".
func (ls *LockedState) PlanPreparationApply(key string, p plan.PreparationPlan, id string) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.PlanApply(p, id)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// RecordPreparationApplied durably confirms one successful prepare mutation.
// PlanPreparationApply must have been persisted before the host mutation began;
// this transition closes that write-ahead record before the next mutation.
func (ls *LockedState) RecordPreparationApplied(key string, p plan.PreparationPlan, id string) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.RecordApplied(p, id)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// ResolvePreparationApplying durably closes an ambiguous write-ahead prepare
// mutation after a read-only probe or explicit recovery action establishes its
// host-side outcome. Unknown outcomes must remain in PreparationApplying and
// are never inferred by this state layer.
func (ls *LockedState) ResolvePreparationApplying(key string, p plan.PreparationPlan, id string, outcome plan.PreparationMutationOutcome) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.ResolveApplying(p, id, outcome)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// PlanPreparationCommit durably marks commit as in progress. Callers must use
// this transition before executing any candidate commit operation. Recovery of
// a persisted committing journal must reconcile the desired/observed install
// state instead of automatically rolling preparation back.
func (ls *LockedState) PlanPreparationCommit(key string, p plan.PreparationPlan) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.PlanCommit(p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// ResolvePreparationCommitNotApplied durably closes an ambiguous commit WAL
// boundary only after the caller has authoritative evidence that none of the
// commit operations took effect. The journal returns to ready so the same
// commit may be retried or the confirmed preparation prefix may be rolled back.
func (ls *LockedState) ResolvePreparationCommitNotApplied(key string, p plan.PreparationPlan) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.ResolveCommitNotApplied(p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// FinalizePreparationCommit atomically removes the active committing journal
// and persists the ownership/refcount claims created by the prepared resources.
// It must only be called after all candidate commit operations have completed successfully.
func (ls *LockedState) FinalizePreparationCommit(key string, p plan.PreparationPlan, dependent string) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, owned, err := journal.FinalizeCommit(p, ls.state.OwnedResources, dependent)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationCompletion(key, owned); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// PlanPreparationRollback durably marks rollback as in progress before any
// compensating operation is executed and returns the rollback decision for
// preview/reporting. Callers must journal each actual compensation through
// PlanPreparationRollbackApply before executing it.
func (ls *LockedState) PlanPreparationRollback(key string, p plan.PreparationPlan) (plan.PreparationJournal, plan.RollbackDecision, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, plan.RollbackDecision{}, err
	}
	next, decision, err := journal.PlanRollback(p, ls.state.OwnedResources)
	if err != nil {
		return plan.PreparationJournal{}, plan.RollbackDecision{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, plan.RollbackDecision{}, err
	}
	return clonePreparationJournal(next), decision, nil
}

// PlanPreparationRollbackApply durably marks the next compensating operation
// as in flight and returns that exact operation. The caller must persist this
// boundary before executing the host-side rollback mutation.
func (ls *LockedState) PlanPreparationRollbackApply(key string, p plan.PreparationPlan, id string) (plan.PreparationJournal, plan.Operation, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, plan.Operation{}, err
	}
	next, operation, err := journal.PlanRollbackApply(p, ls.state.OwnedResources, id)
	if err != nil {
		return plan.PreparationJournal{}, plan.Operation{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, plan.Operation{}, err
	}
	return clonePreparationJournal(next), operation, nil
}

// RecordPreparationRollbackApplied durably confirms one successful rollback
// mutation after PlanPreparationRollbackApply was persisted.
func (ls *LockedState) RecordPreparationRollbackApplied(key string, p plan.PreparationPlan, id string) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.RecordRollbackApplied(p, ls.state.OwnedResources, id)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// ResolvePreparationRollbackApplying closes an ambiguous rollback WAL record
// only after read-only evidence establishes whether the compensation applied.
func (ls *LockedState) ResolvePreparationRollbackApplying(key string, p plan.PreparationPlan, id string, outcome plan.PreparationMutationOutcome) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, err := journal.ResolveRollbackApplying(p, ls.state.OwnedResources, id, outcome)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationJournal(key, next); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// FinalizePreparationRollback atomically removes the active rollback journal
// and persists any retained resource ownership that could not be safely compensated.
func (ls *LockedState) FinalizePreparationRollback(key string, p plan.PreparationPlan) (plan.PreparationJournal, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	next, owned, err := journal.FinalizeRollback(p, ls.state.OwnedResources)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if err := ls.persistPreparationCompletion(key, owned); err != nil {
		return plan.PreparationJournal{}, err
	}
	return clonePreparationJournal(next), nil
}

// PreparationRecovery derives the safe crash-recovery action for one active
// journal while holding the state lock. It does not persist or execute any
// mutation. For a committing journal, callers first invoke this method without
// desired/observation to obtain RecoveryReconcileCommit, perform the read-only
// probe, then invoke it again with both values. RecoveryFinalizeCommit may
// proceed directly to FinalizePreparationCommit. A blocked committing journal
// may return to preparation rollback only through
// ResolvePreparationCommitNotApplied after stronger method-specific evidence
// establishes that no commit operation took effect.
func (ls *LockedState) PreparationRecovery(key string, p plan.PreparationPlan, desired *plan.ResolvedIdentity, observation *plan.Observation) (plan.PreparationRecoveryDecision, error) {
	journal, err := ls.preparationJournal(key, p)
	if err != nil {
		return plan.PreparationRecoveryDecision{}, err
	}
	return journal.RecoveryFor(p, ls.state.OwnedResources, desired, observation)
}

func (ls *LockedState) preparationJournal(key string, p plan.PreparationPlan) (plan.PreparationJournal, error) {
	storedPlan, journal, err := ls.PreparationTransaction(key)
	if err != nil {
		return plan.PreparationJournal{}, err
	}
	if !preparationPlansEqual(storedPlan, p) {
		return plan.PreparationJournal{}, fmt.Errorf("preparation plan %q changed since transaction began", key)
	}
	return journal, nil
}

// PreparationTransaction returns the exact plan and journal persisted for an
// active transaction. Recovery must use this stored plan rather than rebuilding
// one from current host state, which may already reflect an in-flight mutation.
func (ls *LockedState) PreparationTransaction(key string) (plan.PreparationPlan, plan.PreparationJournal, error) {
	if err := validatePreparationJournalKey(key); err != nil {
		return plan.PreparationPlan{}, plan.PreparationJournal{}, err
	}
	if ls == nil || ls.state == nil {
		return plan.PreparationPlan{}, plan.PreparationJournal{}, errors.New("locked state is nil")
	}
	storedPlan, planExists := ls.state.PreparationPlans[key]
	journal, journalExists := ls.state.PreparationJournals[key]
	if !planExists || !journalExists {
		switch {
		case !planExists && !journalExists:
			return plan.PreparationPlan{}, plan.PreparationJournal{}, fmt.Errorf("preparation transaction %q does not exist", key)
		case !planExists:
			return plan.PreparationPlan{}, plan.PreparationJournal{}, fmt.Errorf("preparation journal %q has no persisted plan", key)
		default:
			return plan.PreparationPlan{}, plan.PreparationJournal{}, fmt.Errorf("preparation plan %q has no active journal", key)
		}
	}
	if err := storedPlan.Validate(); err != nil {
		return plan.PreparationPlan{}, plan.PreparationJournal{}, fmt.Errorf("preparation plan %q: %w", key, err)
	}
	if err := journal.Validate(storedPlan); err != nil {
		return plan.PreparationPlan{}, plan.PreparationJournal{}, fmt.Errorf("preparation journal %q: %w", key, err)
	}
	return clonePreparationPlan(storedPlan), clonePreparationJournal(journal), nil
}

// persistPreparationStart atomically records the exact resolved preparation
// plan and its initial journal before any host mutation may run. The plan is a
// WAL dependency: without it, crash recovery could accidentally derive a
// different transaction from already-mutated host state.
func (ls *LockedState) persistPreparationStart(key string, preparationPlan plan.PreparationPlan, journal plan.PreparationJournal) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	if ls.state.PreparationPlans == nil {
		ls.state.PreparationPlans = make(map[string]plan.PreparationPlan)
	}
	if ls.state.PreparationJournals == nil {
		ls.state.PreparationJournals = make(map[string]plan.PreparationJournal)
	}
	previousPlan, hadPlan := ls.state.PreparationPlans[key]
	previousJournal, hadJournal := ls.state.PreparationJournals[key]
	previousChecksum := ls.state.Checksum

	ls.state.PreparationPlans[key] = clonePreparationPlan(preparationPlan)
	ls.state.PreparationJournals[key] = clonePreparationJournal(journal)
	if err := ls.Save(); err != nil {
		if hadPlan {
			ls.state.PreparationPlans[key] = previousPlan
		} else {
			delete(ls.state.PreparationPlans, key)
		}
		if hadJournal {
			ls.state.PreparationJournals[key] = previousJournal
		} else {
			delete(ls.state.PreparationJournals, key)
		}
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

// persistPreparationJournal saves an in-progress journal while the exclusive
// state lock is held. If saving fails, the in-memory state is restored so a
// caller cannot continue from a transition that was never made durable.
func (ls *LockedState) persistPreparationJournal(key string, journal plan.PreparationJournal) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	if ls.state.PreparationJournals == nil {
		ls.state.PreparationJournals = make(map[string]plan.PreparationJournal)
	}
	if _, exists := ls.state.PreparationPlans[key]; !exists {
		return fmt.Errorf("preparation journal %q has no persisted plan", key)
	}
	previousJournal, hadJournal := ls.state.PreparationJournals[key]
	previousChecksum := ls.state.Checksum

	ls.state.PreparationJournals[key] = clonePreparationJournal(journal)
	if err := ls.Save(); err != nil {
		if hadJournal {
			ls.state.PreparationJournals[key] = previousJournal
		} else {
			delete(ls.state.PreparationJournals, key)
		}
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

// persistPreparationCompletion atomically replaces the active journal with
// the resulting ownership snapshot. A successful completion has no recovery
// work left, so terminal journals are not retained indefinitely.
func (ls *LockedState) persistPreparationCompletion(key string, owned []plan.OwnedResourceState) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	previousJournal, hadJournal := ls.state.PreparationJournals[key]
	previousPlan, hadPlan := ls.state.PreparationPlans[key]
	if !hadJournal || !hadPlan {
		return fmt.Errorf("preparation transaction %q is incomplete in state", key)
	}
	previousOwned := ls.state.OwnedResources
	previousChecksum := ls.state.Checksum

	delete(ls.state.PreparationJournals, key)
	delete(ls.state.PreparationPlans, key)
	ls.state.OwnedResources = cloneOwnedResources(owned)
	if err := ls.Save(); err != nil {
		ls.state.PreparationJournals[key] = previousJournal
		ls.state.PreparationPlans[key] = previousPlan
		ls.state.OwnedResources = previousOwned
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

func validatePreparationJournalKey(key string) error {
	if strings.TrimSpace(key) != key || key == "" || strings.ContainsRune(key, '\x00') {
		return errors.New("preparation journal key is invalid")
	}
	return nil
}

func clonePreparationJournal(journal plan.PreparationJournal) plan.PreparationJournal {
	journal.Applied = append([]string(nil), journal.Applied...)
	journal.RollbackApplied = append([]string(nil), journal.RollbackApplied...)
	return journal
}

func cloneOwnedResources(states []plan.OwnedResourceState) []plan.OwnedResourceState {
	if states == nil {
		return nil
	}
	out := make([]plan.OwnedResourceState, len(states))
	for i, state := range states {
		out[i] = state
		out[i].Dependents = append([]string(nil), state.Dependents...)
	}
	return out
}

func clonePreparationPlan(preparationPlan plan.PreparationPlan) plan.PreparationPlan {
	out := preparationPlan
	out.Probe = cloneOperations(preparationPlan.Probe)
	out.Commit = cloneOperations(preparationPlan.Commit)
	if preparationPlan.Prepare != nil {
		out.Prepare = make([]plan.PreparationMutation, len(preparationPlan.Prepare))
		for i, mutation := range preparationPlan.Prepare {
			out.Prepare[i] = mutation
			out.Prepare[i].Apply = cloneStateOperation(mutation.Apply)
			if mutation.Rollback != nil {
				rollback := cloneStateOperation(*mutation.Rollback)
				out.Prepare[i].Rollback = &rollback
			}
		}
	}
	return out
}

func cloneOperations(operations []plan.Operation) []plan.Operation {
	if operations == nil {
		return nil
	}
	out := make([]plan.Operation, len(operations))
	for i, operation := range operations {
		out[i] = cloneStateOperation(operation)
	}
	return out
}

func cloneStateOperation(operation plan.Operation) plan.Operation {
	out := operation
	out.Command = append([]string(nil), operation.Command...)
	return out
}

func preparationPlansEqual(left, right plan.PreparationPlan) bool {
	if len(left.Probe) != len(right.Probe) || len(left.Prepare) != len(right.Prepare) || len(left.Commit) != len(right.Commit) {
		return false
	}
	for i := range left.Probe {
		if !operationsEqual(left.Probe[i], right.Probe[i]) {
			return false
		}
	}
	for i := range left.Prepare {
		a, b := left.Prepare[i], right.Prepare[i]
		if a.ID != b.ID || a.Resource != b.Resource || a.Ownership != b.Ownership || a.Policy != b.Policy || !operationsEqual(a.Apply, b.Apply) {
			return false
		}
		if (a.Rollback == nil) != (b.Rollback == nil) {
			return false
		}
		if a.Rollback != nil && !operationsEqual(*a.Rollback, *b.Rollback) {
			return false
		}
	}
	for i := range left.Commit {
		if !operationsEqual(left.Commit[i], right.Commit[i]) {
			return false
		}
	}
	return true
}

func operationsEqual(left, right plan.Operation) bool {
	if left.Kind != right.Kind || left.Description != right.Description || left.Effect != right.Effect || left.ArbitraryCode != right.ArbitraryCode || len(left.Command) != len(right.Command) {
		return false
	}
	for i := range left.Command {
		if left.Command[i] != right.Command[i] {
			return false
		}
	}
	return true
}
