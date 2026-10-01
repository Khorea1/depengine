package state

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
)

// ReplacementTransaction persists the prior tracked state and immutable target
// needed to recover an interrupted upgrade without selecting another candidate.
type ReplacementTransaction struct {
	ToolName               string                  `json:"tool_name"`
	MethodKind             string                  `json:"method_kind"`
	PreviousCandidate      plan.CandidateIdentity  `json:"previous_candidate"`
	PreviousCandidateLabel string                  `json:"previous_candidate_label,omitempty"`
	Candidate              plan.CandidateIdentity  `json:"candidate"`
	CandidateLabel         string                  `json:"candidate_label,omitempty"`
	Previous               ToolState               `json:"previous"`
	Desired                plan.LockProjection     `json:"desired"`
	ResourceUses           []plan.ResourceUse      `json:"resource_uses,omitempty"`
	PreparationKey         string                  `json:"preparation_key,omitempty"`
	Journal                plan.ReplacementJournal `json:"journal"`
}

// ReplacementCandidate records one durable candidate identity and its schema
// label. The label is separate because it is local to replacement recovery,
// not part of the shared lock projection identity.
type ReplacementCandidate struct {
	Identity plan.CandidateIdentity
	Label    string
}

// BeginReplacement durably records both candidate identities, resource uses,
// and any coordinated preparation transaction before removal. Call only while
// holding LockedState's lock. Candidate labels are replacement-local and are
// not part of the shared lock projection identity.
func (ls *LockedState) BeginReplacement(
	toolName, methodKind string,
	previousCandidate, candidate ReplacementCandidate,
	previous ToolState, desired plan.LockProjection, preparationKey string,
	resourceUses []plan.ResourceUse,
) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	if err := validateReplacementKey(toolName); err != nil {
		return err
	}
	if strings.TrimSpace(methodKind) != methodKind || methodKind == "" {
		return errors.New("replacement method kind is invalid")
	}
	if preparationKey != "" {
		if err := validatePreparationJournalKey(preparationKey); err != nil {
			return err
		}
	}
	current, exists := ls.state.Tools[toolName]
	if !exists || !reflect.DeepEqual(current, previous) {
		return fmt.Errorf("replacement previous state for %q does not match tracked state", toolName)
	}
	tx := ReplacementTransaction{
		ToolName:               toolName,
		MethodKind:             methodKind,
		PreviousCandidate:      previousCandidate.Identity,
		PreviousCandidateLabel: previousCandidate.Label,
		Candidate:              candidate.Identity,
		CandidateLabel:         candidate.Label,
		Previous:               cloneToolState(previous),
		Desired:                desired.Clone(),
		ResourceUses:           cloneResourceUses(resourceUses),
		PreparationKey:         preparationKey,
		Journal:                plan.NewReplacementJournal(),
	}
	if err := validateReplacementTransaction(toolName, tx); err != nil {
		return err
	}
	if ls.state.ReplacementTransactions == nil {
		ls.state.ReplacementTransactions = make(map[string]ReplacementTransaction)
	}
	if _, exists := ls.state.ReplacementTransactions[toolName]; exists {
		return fmt.Errorf("replacement transaction %q already exists", toolName)
	}
	previousChecksum := ls.state.Checksum
	ls.state.ReplacementTransactions[toolName] = tx
	if err := ls.Save(); err != nil {
		delete(ls.state.ReplacementTransactions, toolName)
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

// PlanReplacementRemoval persists the write-ahead boundary before adapter removal.
func (ls *LockedState) PlanReplacementRemoval(toolName string) error {
	return ls.advanceReplacement(toolName, func(j plan.ReplacementJournal) (plan.ReplacementJournal, error) { return j.PlanRemove() })
}

// RecordReplacementRemoved persists the confirmed removal outcome.
func (ls *LockedState) RecordReplacementRemoved(toolName string) error {
	return ls.advanceReplacement(toolName, func(j plan.ReplacementJournal) (plan.ReplacementJournal, error) { return j.RecordRemoved() })
}

// PlanReplacementInstall persists the write-ahead boundary before exact target installation.
func (ls *LockedState) PlanReplacementInstall(toolName string) error {
	return ls.advanceReplacement(toolName, func(j plan.ReplacementJournal) (plan.ReplacementJournal, error) { return j.PlanInstall() })
}

// PlanReplacementPostHook persists the write-ahead boundary before an
// after-upgrade hook that may have arbitrary side effects.
func (ls *LockedState) PlanReplacementPostHook(toolName string) error {
	return ls.advanceReplacement(toolName, func(j plan.ReplacementJournal) (plan.ReplacementJournal, error) { return j.PlanPostHook() })
}

// CompleteReplacement records the hook outcome and removes the replacement WAL
// in the same save. PostHookRunning remains active if that save fails, forcing
// recovery to block rather than replay a potentially completed hook.
func (ls *LockedState) CompleteReplacement(toolName string, toolState ToolState) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	tx, err := ls.ReplacementTransaction(toolName)
	if err != nil {
		return err
	}
	if tx.Journal.Phase != plan.ReplacementInstalled && tx.Journal.Phase != plan.ReplacementPostHookRunning {
		return fmt.Errorf("replacement transaction %q is not installed or running its post-hook", toolName)
	}
	if toolState.MethodKind != tx.MethodKind {
		return fmt.Errorf("replacement final state for %q does not match the persisted candidate", toolName)
	}
	previousTool, hadTool := ls.state.Tools[toolName]
	previousTx := cloneReplacementTransaction(tx)
	previousChecksum := ls.state.Checksum
	ls.state.Tools[toolName] = cloneToolState(toolState)
	delete(ls.state.ReplacementTransactions, toolName)
	if err := ls.Save(); err != nil {
		if hadTool {
			ls.state.Tools[toolName] = previousTool
		} else {
			delete(ls.state.Tools, toolName)
		}
		ls.state.ReplacementTransactions[toolName] = previousTx
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

// ReplacementTransaction returns a deep copy of an active persisted transaction.
func (ls *LockedState) ReplacementTransaction(toolName string) (ReplacementTransaction, error) {
	if ls == nil || ls.state == nil {
		return ReplacementTransaction{}, errors.New("locked state is nil")
	}
	if err := validateReplacementKey(toolName); err != nil {
		return ReplacementTransaction{}, err
	}
	tx, exists := ls.state.ReplacementTransactions[toolName]
	if !exists {
		return ReplacementTransaction{}, fmt.Errorf("replacement transaction %q does not exist", toolName)
	}
	if err := validateReplacementTransaction(toolName, tx); err != nil {
		return ReplacementTransaction{}, err
	}
	return cloneReplacementTransaction(tx), nil
}

// ReplacementRecovery returns a pure action for the persisted phase and caller-supplied observations.
func (ls *LockedState) ReplacementRecovery(toolName string, old, desired plan.VerificationResult) (plan.ReplacementRecoveryAction, error) {
	tx, err := ls.ReplacementTransaction(toolName)
	if err != nil {
		return plan.ReplacementBlocked, err
	}
	return plan.ReplacementRecoveryFor(tx.Journal.Phase, old, desired)
}

// CommitReplacementInstallWithPreparation atomically records the verified
// installed state and resource ownership, closes any preparation WAL, advances
// the replacement to Installed, and keeps the replacement WAL for hook recovery.
func (ls *LockedState) CommitReplacementInstallWithPreparation(
	toolName string,
	toolState ToolState,
	owned []plan.OwnedResourceState,
	preparationKey string,
	preparationPlan plan.PreparationPlan,
	dependent string,
) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	tx, err := ls.ReplacementTransaction(toolName)
	if err != nil {
		return err
	}
	if tx.Journal.Phase != plan.ReplacementInstalling {
		return fmt.Errorf("replacement transaction %q is not installing", toolName)
	}
	if toolState.MethodKind != tx.MethodKind {
		return fmt.Errorf("replacement final state for %q does not match the persisted candidate", toolName)
	}
	if preparationKey != tx.PreparationKey {
		return fmt.Errorf("replacement preparation key for %q does not match the persisted transaction", toolName)
	}
	if dependent == "" {
		return errors.New("replacement preparation dependent is required")
	}
	if err := plan.ValidateOwnedResourceSnapshot(owned); err != nil {
		return fmt.Errorf("replacement owned resources: %w", err)
	}

	var prepPlan plan.PreparationPlan
	var prepJournal plan.PreparationJournal
	if preparationKey != "" {
		prepPlan, prepJournal, err = ls.PreparationTransaction(preparationKey)
		if err != nil {
			return err
		}
		if !preparationPlansEqual(prepPlan, preparationPlan) {
			return fmt.Errorf("preparation plan %q changed since transaction began", preparationKey)
		}
		if prepJournal.Status != plan.PreparationCommitting {
			return fmt.Errorf("preparation transaction %q is not committing", preparationKey)
		}
		_, owned, err = prepJournal.FinalizeCommit(prepPlan, owned, dependent)
		if err != nil {
			return err
		}
		owned, err = plan.ClaimResourceUses(owned, dependent, prepPlan.CommitUses)
		if err != nil {
			return fmt.Errorf("claim preparation commit resources: %w", err)
		}
		if err := plan.ValidateOwnedResourceSnapshot(owned); err != nil {
			return fmt.Errorf("replacement owned resources after preparation: %w", err)
		}
	}

	previousTool, hadTool := ls.state.Tools[toolName]
	previousOwned := ls.state.OwnedResources
	previousTx := cloneReplacementTransaction(ls.state.ReplacementTransactions[toolName])
	previousChecksum := ls.state.Checksum
	var previousPrepPlan plan.PreparationPlan
	var previousPrepJournal plan.PreparationJournal
	if preparationKey != "" {
		previousPrepPlan = ls.state.PreparationPlans[preparationKey]
		previousPrepJournal = ls.state.PreparationJournals[preparationKey]
	}
	if ls.state.Tools == nil {
		ls.state.Tools = make(map[string]ToolState)
	}
	ls.state.Tools[toolName] = cloneToolState(toolState)
	ls.state.OwnedResources = cloneOwnedResources(owned)
	installedJournal, err := tx.Journal.RecordInstalled()
	if err != nil {
		return err
	}
	tx.Journal = installedJournal
	ls.state.ReplacementTransactions[toolName] = tx
	if preparationKey != "" {
		delete(ls.state.PreparationPlans, preparationKey)
		delete(ls.state.PreparationJournals, preparationKey)
	}
	if err := ls.Save(); err != nil {
		if hadTool {
			ls.state.Tools[toolName] = previousTool
		} else {
			delete(ls.state.Tools, toolName)
		}
		ls.state.OwnedResources = previousOwned
		ls.state.ReplacementTransactions[toolName] = previousTx
		if preparationKey != "" {
			ls.state.PreparationPlans[preparationKey] = previousPrepPlan
			ls.state.PreparationJournals[preparationKey] = previousPrepJournal
		}
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

func (ls *LockedState) advanceReplacement(toolName string, advance func(plan.ReplacementJournal) (plan.ReplacementJournal, error)) error {
	if ls == nil || ls.state == nil {
		return errors.New("locked state is nil")
	}
	tx, err := ls.ReplacementTransaction(toolName)
	if err != nil {
		return err
	}
	next, err := advance(tx.Journal)
	if err != nil {
		return err
	}
	previousChecksum := ls.state.Checksum
	previousTransaction := cloneReplacementTransaction(tx)
	tx.Journal = next
	ls.state.ReplacementTransactions[toolName] = tx
	if err := ls.Save(); err != nil {
		ls.state.ReplacementTransactions[toolName] = previousTransaction
		ls.state.Checksum = previousChecksum
		return err
	}
	return nil
}

func validateReplacementKey(key string) error {
	if strings.TrimSpace(key) != key || key == "" || strings.ContainsRune(key, '\x00') {
		return errors.New("replacement tool name is invalid")
	}
	return nil
}

func validateReplacementTransaction(key string, tx ReplacementTransaction) error {
	if err := validateReplacementKey(key); err != nil {
		return err
	}
	if tx.ToolName != key || strings.TrimSpace(tx.MethodKind) != tx.MethodKind || tx.MethodKind == "" {
		return fmt.Errorf("replacement transaction %q has invalid tool or method identity", key)
	}
	if tx.PreviousCandidate.Method != tx.MethodKind || tx.Candidate.Method != tx.MethodKind || tx.Desired.Tool.Name != key || tx.Desired.Candidate != tx.Candidate {
		return fmt.Errorf("replacement transaction %q has mismatched candidate identity", key)
	}
	if err := validateReplacementLabel(tx.PreviousCandidateLabel); err != nil {
		return fmt.Errorf("replacement previous candidate label %q: %w", key, err)
	}
	if err := validateReplacementLabel(tx.CandidateLabel); err != nil {
		return fmt.Errorf("replacement candidate label %q: %w", key, err)
	}
	if err := validateReplacementResourceUses(tx.ResourceUses); err != nil {
		return fmt.Errorf("replacement resource uses %q: %w", key, err)
	}
	if tx.PreparationKey != "" {
		if err := validatePreparationJournalKey(tx.PreparationKey); err != nil {
			return fmt.Errorf("replacement preparation key %q: %w", key, err)
		}
	}
	if err := tx.Desired.Validate(); err != nil {
		return fmt.Errorf("replacement target %q: %w", key, err)
	}
	if err := tx.Desired.RequireImmutable(); err != nil {
		return fmt.Errorf("replacement target %q is not immutable: %w", key, err)
	}
	if err := tx.Journal.Validate(); err != nil {
		return fmt.Errorf("replacement journal %q: %w", key, err)
	}
	return nil
}

func cloneReplacementTransaction(tx ReplacementTransaction) ReplacementTransaction {
	tx.Previous = cloneToolState(tx.Previous)
	tx.Desired = tx.Desired.Clone()
	tx.ResourceUses = cloneResourceUses(tx.ResourceUses)
	return tx
}

func cloneResourceUses(uses []plan.ResourceUse) []plan.ResourceUse {
	return append([]plan.ResourceUse(nil), uses...)
}

func validateReplacementLabel(label string) error {
	if strings.TrimSpace(label) != label || strings.ContainsRune(label, '\x00') {
		return errors.New("label is invalid")
	}
	return nil
}

func validateReplacementResourceUses(uses []plan.ResourceUse) error {
	seen := make(map[plan.ResourceIdentity]struct{}, len(uses))
	for i, use := range uses {
		if err := use.Resource.Validate(); err != nil {
			return fmt.Errorf("resource use %d: %w", i, err)
		}
		if _, duplicate := seen[use.Resource]; duplicate {
			return fmt.Errorf("duplicate resource use %q/%q", use.Resource.Kind, use.Resource.Key)
		}
		seen[use.Resource] = struct{}{}
	}
	return nil
}

func cloneToolState(ts ToolState) ToolState {
	if ts.Config != nil {
		ts.Config = cloneStateValue(ts.Config).(map[string]any)
	}
	return ts
}

func cloneStateValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = cloneStateValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = cloneStateValue(child)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return v
	}
}
