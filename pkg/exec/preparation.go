package exec

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/source"
	depstate "github.com/Khorea1/depengine/pkg/state"
)

type recoveredCandidateCommit struct {
	toolName   string
	methodKind string
	method     string
	config     map[string]any
	intent     *plan.ResolvedInstallPlan
}

func (r recoveredCandidateCommit) result() ToolResult {
	return ToolResult{
		Tool:             r.toolName,
		Status:           StatusInstalled,
		Method:           r.method,
		MethodKind:       r.methodKind,
		Config:           r.config,
		PlanIntent:       r.intent,
		InstallCommitted: true,
	}
}

type candidateSourcePreparation struct {
	resourceUses    []plan.ResourceUse
	missing         []config.Source
	added           []config.Source // direct/no-state fallback only
	preparationPlan *plan.PreparationPlan
	tx              *sourcePreparationTransaction
}

type sourcePreparationTransaction struct {
	ex     *Executor
	locked *depstate.LockedState
	key    string
	plan   plan.PreparationPlan
	closed bool
}

type preparationBlockedError struct{ err error }

func (e *preparationBlockedError) Error() string { return e.err.Error() }
func (e *preparationBlockedError) Unwrap() error { return e.err }

func preparationBlocked(err error) bool {
	var blocked *preparationBlockedError
	return errors.As(err, &blocked)
}

func blockPreparation(format string, args ...any) error {
	return &preparationBlockedError{err: fmt.Errorf(format, args...)}
}

func candidatePreparationKey(toolName, methodKind string, intent *plan.ResolvedInstallPlan) (string, error) {
	if toolName == "" || methodKind == "" {
		return "", errors.New("candidate preparation identity requires tool and method")
	}
	identity := any(map[string]string{"tool": toolName, "method": methodKind})
	if intent != nil {
		identity = intent
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("marshal candidate preparation identity: %w", err)
	}
	sum := sha256.Sum256(data)
	encode := base64.RawURLEncoding.EncodeToString
	return fmt.Sprintf("candidate/v1/%s/%s/%s", encode([]byte(toolName)), encode([]byte(methodKind)), hex.EncodeToString(sum[:])), nil
}

func candidatePreparationSubject(key string) (string, string, error) {
	const prefix = "candidate/v1/"
	if !strings.HasPrefix(key, prefix) {
		return "", "", fmt.Errorf("preparation key %q does not contain recoverable candidate metadata", key)
	}
	parts := strings.Split(strings.TrimPrefix(key, prefix), "/")
	if len(parts) != 3 || len(parts[2]) != sha256.Size*2 {
		return "", "", fmt.Errorf("preparation key %q has invalid candidate metadata", key)
	}
	decode := base64.RawURLEncoding.DecodeString
	toolBytes, err := decode(parts[0])
	if err != nil || len(toolBytes) == 0 {
		return "", "", fmt.Errorf("preparation key %q has invalid tool identity", key)
	}
	methodBytes, err := decode(parts[1])
	if err != nil || len(methodBytes) == 0 {
		return "", "", fmt.Errorf("preparation key %q has invalid method identity", key)
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", "", fmt.Errorf("preparation key %q has invalid candidate digest", key)
	}
	return string(toolBytes), string(methodBytes), nil
}

func (ex *Executor) probeCandidateSources(ctx context.Context, configured []config.Source) (candidateSourcePreparation, error) {
	var prepared candidateSourcePreparation
	if len(configured) == 0 {
		return prepared, nil
	}

	// Validate every ownership identity before any mutation and reject duplicate
	// host resources before an add command can run.
	if _, err := sourceResourceUses(configured, nil); err != nil {
		return prepared, err
	}
	missing, err := ex.sources.Missing(ctx, configured)
	if err != nil {
		return prepared, err
	}
	prepared.missing = append([]config.Source(nil), missing...)
	prepared.resourceUses, err = sourceResourceUses(configured, missing)
	if err != nil {
		return candidateSourcePreparation{}, err
	}
	if len(missing) == 0 {
		return prepared, nil
	}

	preparationPlan, err := source.PreparationPlan(missing)
	if err != nil {
		return candidateSourcePreparation{}, err
	}
	prepared.preparationPlan = &preparationPlan
	return prepared, nil
}

func (ex *Executor) prepareCandidateSources(ctx context.Context, toolName, methodKind string, intent *plan.ResolvedInstallPlan, prepared candidateSourcePreparation) (candidateSourcePreparation, error) {
	if len(prepared.missing) == 0 {
		return prepared, nil
	}
	if prepared.preparationPlan == nil {
		return candidateSourcePreparation{}, errors.New("source preparation probe is missing its resolved preparation plan")
	}
	missing := prepared.missing
	preparationPlan := *prepared.preparationPlan
	if ex.dryRun {
		for _, configured := range missing {
			ex.outputf("    prepare: would add source %s %s\n", configured.Kind, configured.Name)
		}
		return prepared, nil
	}

	// Library callers that intentionally disabled state tracking retain the old
	// best-effort semantics. The CLI always configures schemaPath and therefore
	// takes the durable WAL path below.
	if ex.schemaPath == "" {
		for _, configured := range missing {
			if err := ex.sources.Add(ctx, configured); err != nil {
				present, probeErr := ex.sources.Present(ctx, configured)
				if probeErr != nil {
					return candidateSourcePreparation{}, blockPreparation("source preparation outcome is ambiguous for %s %s: add failed: %v; probe failed: %v", configured.Kind, configured.Name, err, probeErr)
				}
				if present {
					prepared.added = append(prepared.added, configured)
				}
				if rollbackErr := ex.sources.Remove(ctx, prepared.added); rollbackErr != nil {
					return candidateSourcePreparation{}, blockPreparation("source preparation failed: %v; rollback failed: %v", err, rollbackErr)
				}
				return candidateSourcePreparation{}, fmt.Errorf("source preparation failed: %w", err)
			}
			prepared.added = append(prepared.added, configured)
		}
		return prepared, nil
	}

	key, err := candidatePreparationKey(toolName, methodKind, intent)
	if err != nil {
		return candidateSourcePreparation{}, err
	}
	locked, err := depstate.LoadLocked()
	if err != nil {
		return candidateSourcePreparation{}, fmt.Errorf("state lock for source preparation: %w", err)
	}
	tx := &sourcePreparationTransaction{ex: ex, locked: locked, key: key, plan: preparationPlan}
	if _, err := locked.BeginPreparation(key, preparationPlan); err != nil {
		_ = tx.close()
		return candidateSourcePreparation{}, fmt.Errorf("begin source preparation: %w", err)
	}
	prepared.tx = tx

	for i, configured := range missing {
		mutation := preparationPlan.Prepare[i]
		if _, err := locked.PlanPreparationApply(key, preparationPlan, mutation.ID); err != nil {
			_ = tx.close()
			return candidateSourcePreparation{}, blockPreparation("persist source preparation boundary: %v", err)
		}
		if addErr := ex.sources.Add(ctx, configured); addErr != nil {
			present, probeErr := ex.sources.Present(ctx, configured)
			if probeErr != nil {
				_ = tx.close()
				return candidateSourcePreparation{}, blockPreparation("source preparation outcome is ambiguous for %s %s: add failed: %v; probe failed: %v", configured.Kind, configured.Name, addErr, probeErr)
			}
			outcome := plan.PreparationMutationNotApplied
			if present {
				outcome = plan.PreparationMutationApplied
			}
			if _, err := locked.ResolvePreparationApplying(key, preparationPlan, mutation.ID, outcome); err != nil {
				_ = tx.close()
				return candidateSourcePreparation{}, blockPreparation("resolve failed source preparation %s: %v", mutation.ID, err)
			}
			if rollbackErr := tx.rollback(ctx); rollbackErr != nil {
				return candidateSourcePreparation{}, blockPreparation("source preparation failed: %v; rollback failed: %v", addErr, rollbackErr)
			}
			return candidateSourcePreparation{}, fmt.Errorf("source preparation failed: %w", addErr)
		}
		if _, err := locked.RecordPreparationApplied(key, preparationPlan, mutation.ID); err != nil {
			// The host mutation completed but confirmation did not. Leave the
			// applying record durable; recovery will probe the source before taking
			// any further action.
			_ = tx.close()
			return candidateSourcePreparation{}, blockPreparation("source %s %s was added but its WAL confirmation failed: %v", configured.Kind, configured.Name, err)
		}
	}
	return prepared, nil
}

func (prepared *candidateSourcePreparation) planCommit() error {
	if prepared == nil || prepared.tx == nil {
		return nil
	}
	_, err := prepared.tx.locked.PlanPreparationCommit(prepared.tx.key, prepared.tx.plan)
	if err != nil {
		return blockPreparation("persist preparation commit boundary: %v", err)
	}
	return nil
}

func (prepared *candidateSourcePreparation) finalizeCommit(dependent string) error {
	if prepared == nil || prepared.tx == nil {
		return nil
	}
	tx := prepared.tx
	if _, err := tx.locked.FinalizePreparationCommit(tx.key, tx.plan, dependent); err != nil {
		_ = tx.close()
		return blockPreparation("finalize preparation commit: %v", err)
	}
	return tx.close()
}

func (prepared *candidateSourcePreparation) rollback(ctx context.Context, ex *Executor) error {
	if prepared == nil {
		return nil
	}
	if prepared.tx != nil {
		return prepared.tx.rollback(ctx)
	}
	if len(prepared.added) == 0 {
		return nil
	}
	return ex.sources.Remove(ctx, prepared.added)
}

func (prepared *candidateSourcePreparation) leaveCommitUnresolved() error {
	if prepared == nil || prepared.tx == nil {
		return nil
	}
	return prepared.tx.close()
}

func (tx *sourcePreparationTransaction) rollback(ctx context.Context) error {
	if tx == nil || tx.closed {
		return nil
	}
	_, decision, err := tx.locked.PlanPreparationRollback(tx.key, tx.plan)
	if err != nil {
		_ = tx.close()
		return err
	}
	if err := tx.applyRollbackDecision(ctx, decision); err != nil {
		_ = tx.close()
		return err
	}
	if _, err := tx.locked.FinalizePreparationRollback(tx.key, tx.plan); err != nil {
		_ = tx.close()
		return err
	}
	return tx.close()
}

func (tx *sourcePreparationTransaction) resumeRollback(ctx context.Context, decision plan.RollbackDecision) error {
	if tx == nil || tx.closed {
		return nil
	}
	if err := tx.applyRollbackDecision(ctx, decision); err != nil {
		return err
	}
	_, err := tx.locked.FinalizePreparationRollback(tx.key, tx.plan)
	return err
}

func (tx *sourcePreparationTransaction) applyRollbackDecision(ctx context.Context, decision plan.RollbackDecision) error {
	if len(decision.OperationIDs) != len(decision.Operations) {
		return fmt.Errorf("rollback decision has %d ids for %d operations", len(decision.OperationIDs), len(decision.Operations))
	}
	for i, operation := range decision.Operations {
		id := decision.OperationIDs[i]
		mutation, err := preparationMutationByID(tx.plan, id)
		if err != nil {
			return err
		}
		if operation.Kind != "remove-source" || mutation.Resource.Kind != plan.ResourceSource {
			return fmt.Errorf("recovery cannot execute rollback operation %q for resource kind %q", operation.Kind, mutation.Resource.Kind)
		}
		configured, err := source.FromResourceIdentity(mutation.Resource)
		if err != nil {
			return fmt.Errorf("decode rollback source %q: %w", mutation.Resource.Key, err)
		}
		if _, _, err := tx.locked.PlanPreparationRollbackApply(tx.key, tx.plan, id); err != nil {
			return err
		}
		if removeErr := tx.ex.sources.Remove(ctx, []config.Source{configured}); removeErr != nil {
			present, probeErr := tx.ex.sources.Present(ctx, configured)
			if probeErr != nil {
				return fmt.Errorf("rollback source %s failed: %v; outcome probe failed: %v", configured.Name, removeErr, probeErr)
			}
			outcome := plan.PreparationMutationApplied
			if present {
				outcome = plan.PreparationMutationNotApplied
			}
			if _, err := tx.locked.ResolvePreparationRollbackApplying(tx.key, tx.plan, id, outcome); err != nil {
				return fmt.Errorf("resolve rollback source %s: %w", configured.Name, err)
			}
			if present {
				return removeErr
			}
			continue
		}
		if _, err := tx.locked.RecordPreparationRollbackApplied(tx.key, tx.plan, id); err != nil {
			return fmt.Errorf("confirm rollback source %s: %w", configured.Name, err)
		}
	}
	return nil
}

func (tx *sourcePreparationTransaction) close() error {
	if tx == nil || tx.closed || tx.locked == nil {
		return nil
	}
	tx.closed = true
	return tx.locked.Close()
}

func preparationMutationByID(preparationPlan plan.PreparationPlan, id string) (plan.PreparationMutation, error) {
	for _, mutation := range preparationPlan.Prepare {
		if mutation.ID == id {
			return mutation, nil
		}
	}
	return plan.PreparationMutation{}, fmt.Errorf("preparation mutation %q not found", id)
}

func (ex *Executor) preparationRecoveryNeedsElevation() bool {
	if ex == nil || ex.dryRun || ex.schemaPath == "" {
		return false
	}
	st, err := depstate.Load()
	if err != nil {
		// Recovery itself will surface corruption or incompatible state before any
		// new host mutation. This helper only decides whether to establish the
		// interactive elevation session first.
		return false
	}
	for _, preparationPlan := range st.PreparationPlans {
		for _, mutation := range preparationPlan.Prepare {
			if mutation.Resource.Kind != plan.ResourceSource {
				continue
			}
			configured, err := source.FromResourceIdentity(mutation.Resource)
			if err != nil {
				continue
			}
			if configured.Kind == "apt-ppa" || configured.Kind == "dnf-copr" {
				return true
			}
		}
	}
	return false
}

func (ex *Executor) recoveryCandidate(key string) (*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, Adapter, error) {
	toolName, methodKind, err := candidatePreparationSubject(key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if ex.schema == nil {
		return nil, nil, nil, nil, errors.New("current schema is unavailable for commit reconciliation")
	}
	tool := ex.schema.Tools[toolName]
	if tool == nil {
		return nil, nil, nil, nil, fmt.Errorf("tool %q no longer exists in the current schema", toolName)
	}

	var matched *config.MethodCandidate
	var matchedIntent *plan.ResolvedInstallPlan
	for _, method := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
		if method.Kind != methodKind {
			continue
		}
		intent, mismatch := candidatePlanIntent(tool, method)
		candidateKey, keyErr := candidatePreparationKey(toolName, methodKind, intent)
		if keyErr != nil || candidateKey != key {
			continue
		}
		if mismatch != "" {
			return nil, nil, nil, nil, fmt.Errorf("candidate %s/%s no longer satisfies planning requirements: %s", toolName, methodKind, mismatch)
		}
		if intent == nil {
			return nil, nil, nil, nil, fmt.Errorf("candidate %s/%s has no resolved identity for commit reconciliation", toolName, methodKind)
		}
		if matched != nil {
			return nil, nil, nil, nil, fmt.Errorf("candidate preparation key %q matches multiple current methods", key)
		}
		matched = method
		matchedIntent = intent
	}
	if matched == nil {
		return nil, nil, nil, nil, fmt.Errorf("candidate %s/%s no longer matches the persisted preparation identity", toolName, methodKind)
	}
	adapter := ex.LookupAdapter(methodKind)
	if adapter == nil {
		return nil, nil, nil, nil, fmt.Errorf("adapter %q is unavailable for commit reconciliation", methodKind)
	}
	return tool, matched, matchedIntent, adapter, nil
}

// observeRecoveryCandidate derives only identity facts that the legacy Adapter
// contract can establish without mutation. A successful candidate-specific
// Check proves target presence and package identity. Version is authoritative
// only when the adapter implements Versioner and returns a concrete value.
// Other identity dimensions remain unverifiable, which deliberately leaves the
// committing journal blocked rather than guessing.
func (ex *Executor) observeRecoveryCandidate(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, intent *plan.ResolvedInstallPlan, adapter Adapter) plan.Observation {
	probeCtx, cancel := context.WithTimeout(ctx, ex.methodTimeout)
	defer cancel()
	if !adapter.Check(probeCtx, ex.probeRunner(tool.Name, method.Kind), tool, method) {
		return plan.Observation{Presence: plan.PresenceAbsent}
	}

	observation := plan.Observation{Presence: plan.PresencePresent}
	if intent.Identity.Package != "" {
		observation.Identity.Package = intent.Identity.Package
		observation.KnownFields = append(observation.KnownFields, plan.FieldPackage)
	}
	if intent.Identity.Version != "" {
		if versioner, ok := adapter.(Versioner); ok {
			versionCtx, versionCancel := context.WithTimeout(ctx, versionProbeTimeout)
			version, err := versioner.InstalledVersion(versionCtx, ex.probeRunner(tool.Name, method.Kind), tool, method)
			versionCancel()
			if err != nil {
				observation.Detail = "installed version probe failed: " + err.Error()
			} else if version != "" {
				observation.Identity.Version = version
				observation.KnownFields = append(observation.KnownFields, plan.FieldVersion)
			}
		}
	}
	return observation
}

func (ex *Executor) reconcilePreparationCommit(ctx context.Context, locked *depstate.LockedState, key string, preparationPlan plan.PreparationPlan) error {
	tool, method, intent, adapter, err := ex.recoveryCandidate(key)
	if err != nil {
		return err
	}
	observation := ex.observeRecoveryCandidate(ctx, tool, method, intent, adapter)
	decision, err := locked.PreparationRecovery(key, preparationPlan, &intent.Identity, &observation)
	if err != nil {
		return err
	}
	if decision.Action != plan.RecoveryFinalizeCommit {
		return fmt.Errorf("candidate commit outcome remains unresolved; %s", decision.Detail)
	}
	if _, err = locked.FinalizePreparationCommit(key, preparationPlan, tool.Name); err != nil {
		return err
	}
	displayMethod := method.Kind
	if method.Label != "" {
		displayMethod = method.Label
	}
	ex.recoveredCommits[tool.Name] = recoveredCandidateCommit{
		toolName: tool.Name, methodKind: method.Kind, method: displayMethod,
		config: method.Config, intent: intent,
	}
	return nil
}

// recoverPreparationTransactions runs before any new host mutation. Source-only
// preparations can be reconciled and rolled back from the persisted plan alone.
// A journal already in commit remains fail-closed until adapter-neutral commit
// observation is wired: replaying or rolling it back would guess whether the
// install operation took effect.
func (ex *Executor) recoverPreparationTransactions(ctx context.Context) error {
	if ex.dryRun || ex.schemaPath == "" {
		return nil
	}
	locked, err := depstate.LoadLocked()
	if err != nil {
		return fmt.Errorf("load preparation recovery state: %w", err)
	}
	defer locked.Close()

	keys := make([]string, 0, len(locked.State().PreparationJournals))
	for key := range locked.State().PreparationJournals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ex.recoverPreparationTransaction(ctx, locked, key); err != nil {
			return fmt.Errorf("recover preparation %q: %w", key, err)
		}
	}
	return nil
}

func (ex *Executor) recoverPreparationTransaction(ctx context.Context, locked *depstate.LockedState, key string) error {
	for {
		preparationPlan, journal, err := locked.PreparationTransaction(key)
		if err != nil {
			return err
		}

		// An in-flight source add/remove has an authoritative read-only recovery
		// probe: presence means add applied / remove did not; absence means the
		// inverse. Resolve that WAL marker before asking the pure recovery planner
		// for the next action.
		if journal.Status == plan.PreparationApplying {
			mutation, err := preparationMutationByID(preparationPlan, journal.Applying)
			if err != nil {
				return err
			}
			if mutation.Resource.Kind != plan.ResourceSource || mutation.Apply.Kind != "add-source" {
				return fmt.Errorf("in-flight mutation %q is not a recoverable source add", mutation.ID)
			}
			configured, err := source.FromResourceIdentity(mutation.Resource)
			if err != nil {
				return err
			}
			present, err := ex.sources.Present(ctx, configured)
			if err != nil {
				return fmt.Errorf("probe in-flight source add %s: %w", configured.Name, err)
			}
			outcome := plan.PreparationMutationNotApplied
			if present {
				outcome = plan.PreparationMutationApplied
			}
			if _, err := locked.ResolvePreparationApplying(key, preparationPlan, mutation.ID, outcome); err != nil {
				return err
			}
			continue
		}
		if journal.Status == plan.PreparationRollingBack && journal.RollbackApplying != "" {
			mutation, err := preparationMutationByID(preparationPlan, journal.RollbackApplying)
			if err != nil {
				return err
			}
			if mutation.Resource.Kind != plan.ResourceSource || mutation.Rollback == nil || mutation.Rollback.Kind != "remove-source" {
				return fmt.Errorf("in-flight rollback mutation %q is not a recoverable source removal", mutation.ID)
			}
			configured, err := source.FromResourceIdentity(mutation.Resource)
			if err != nil {
				return err
			}
			present, err := ex.sources.Present(ctx, configured)
			if err != nil {
				return fmt.Errorf("probe in-flight source rollback %s: %w", configured.Name, err)
			}
			outcome := plan.PreparationMutationApplied
			if present {
				outcome = plan.PreparationMutationNotApplied
			}
			if _, err := locked.ResolvePreparationRollbackApplying(key, preparationPlan, mutation.ID, outcome); err != nil {
				return err
			}
			continue
		}

		decision, err := locked.PreparationRecovery(key, preparationPlan, nil, nil)
		if err != nil {
			return err
		}
		switch decision.Action {
		case plan.RecoveryRollbackPreparation:
			_, rollback, err := locked.PlanPreparationRollback(key, preparationPlan)
			if err != nil {
				return err
			}
			tx := &sourcePreparationTransaction{ex: ex, locked: locked, key: key, plan: preparationPlan}
			if err := tx.applyRollbackDecision(ctx, rollback); err != nil {
				return err
			}
			_, err = locked.FinalizePreparationRollback(key, preparationPlan)
			return err
		case plan.RecoveryResumeRollback:
			tx := &sourcePreparationTransaction{ex: ex, locked: locked, key: key, plan: preparationPlan}
			if err := tx.resumeRollback(ctx, decision.Rollback); err != nil {
				return err
			}
			return nil
		case plan.RecoveryReconcileCommit:
			return ex.reconcilePreparationCommit(ctx, locked, key, preparationPlan)
		case plan.RecoveryFinalizeCommit:
			return fmt.Errorf("unexpected commit-finalize recovery decision without reconciliation evidence")
		case plan.RecoveryBlocked:
			return fmt.Errorf("preparation recovery is blocked: %s", decision.Detail)
		default:
			return fmt.Errorf("unsupported recovery action %q", decision.Action)
		}
	}
}
