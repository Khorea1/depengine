package exec

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	depstate "github.com/Khorea1/depengine/internal/state"
)

// replaceCandidate owns the destructive boundary for a reconciled upgrade.
// All candidate selection, source, hook, prerequisite, and credential gates
// have completed before this function is called.
func (ex *Executor) replaceCandidate(ac *candidateAttempt, result *ToolResult, runner run.Runner, methodCtx context.Context) attemptOutcome {
	if ac.transition != plan.TransitionUpgrade || ac.resolved == nil || ac.reported == nil {
		return ex.failReplacement(ac, result, fmt.Errorf("upgrade replacement requires an exact resolved target"))
	}
	if ac.adapter == nil || !ac.adapter.CanRemove() {
		return ex.failReplacement(ac, result, fmt.Errorf("adapter %q does not support removal", ac.method.Kind))
	}
	projection, err := plan.ProjectLock(*ac.reported)
	if err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("project replacement target: %w", err))
	}

	var locked *depstate.LockedState
	closeLocked := false
	if ac.prepared.tx != nil {
		locked = ac.prepared.tx.locked
	} else {
		locked, err = depstate.LoadLocked()
		if err != nil {
			return ex.failReplacement(ac, result, fmt.Errorf("lock replacement state: %w", err))
		}
		closeLocked = true
	}
	if closeLocked {
		defer func() { _ = locked.Close() }()
	}
	current := locked.State()
	previous, exists := current.Tools[ac.tool.Name]
	if !exists {
		return ex.failReplacement(ac, result, fmt.Errorf("replacement requires tracked state for %q", ac.tool.Name))
	}
	if ac.resolution == nil || ac.resolution.expectedPrevious == nil {
		return ex.failReplacement(ac, result, fmt.Errorf("replacement for %q requires the tracked state captured during upgrade discovery", ac.tool.Name))
	}
	if !reflect.DeepEqual(previous, *ac.resolution.expectedPrevious) {
		return ex.failReplacement(ac, result, fmt.Errorf("tracked state for %q changed after upgrade discovery", ac.tool.Name))
	}
	expectedPrevious := *ac.resolution.expectedPrevious
	oldKind := expectedPrevious.MethodKind
	if oldKind == "" {
		oldKind = expectedPrevious.Method
	}
	if oldKind != ac.method.Kind {
		return ex.failReplacement(ac, result, fmt.Errorf("tracked method %q differs from replacement candidate %q", oldKind, ac.method.Kind))
	}
	oldLabel := expectedPrevious.Method
	if oldLabel == oldKind {
		// The persisted method may be either the legacy kind-only identity or a
		// real candidate label whose value happens to equal its kind. Prefer the
		// exact label when present; use kind-only lookup only for legacy state.
		hasExactLabel := false
		for _, candidate := range ac.tool.Methods {
			if candidate != nil && candidate.Kind == oldKind && candidate.Label == oldLabel && candidate.Label != "" {
				hasExactLabel = true
				break
			}
		}
		if !hasExactLabel {
			oldLabel = ""
		}
	}
	trackedMethod, lookupErr := config.FindMethodCandidate(ac.tool, oldKind, oldLabel)
	if lookupErr != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("resolve exact tracked candidate for %q: %w", ac.tool.Name, lookupErr))
	}
	oldMethodValue := *trackedMethod
	if expectedPrevious.Config != nil {
		oldMethodValue.Config = cloneExpectedStateValue(expectedPrevious.Config).(map[string]any)
	} else {
		oldMethodValue.Config = nil
	}
	oldMethod := &oldMethodValue
	oldAdapter := ex.LookupAdapter(oldKind)
	if oldAdapter == nil || !oldAdapter.CanRemove() {
		return ex.failReplacement(ac, result, fmt.Errorf("tracked adapter %q cannot remove the old installation", oldKind))
	}
	preparationKey := ""
	if ac.prepared.tx != nil {
		preparationKey = ac.prepared.tx.key
	}
	removeCredentials, credentialErr := ex.executionCredentialContext(ac.toolCtx, oldMethod)
	if credentialErr != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("resolve tracked removal credentials: %w", credentialErr))
	}
	previousCandidate := depstate.ReplacementCandidate{
		Identity: plan.CandidateIdentity{Method: oldMethod.Kind, Explicit: !oldMethod.Inferred},
		Label:    oldMethod.Label,
	}
	desiredCandidate := depstate.ReplacementCandidate{
		Identity: ac.resolved.Candidate,
		Label:    ac.method.Label,
	}
	if err := locked.BeginReplacement(
		ac.tool.Name, oldKind, previousCandidate, desiredCandidate,
		previous, projection, preparationKey, ac.resources,
	); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("persist replacement intent: %w", err))
	}
	if ex.beforeReplacementSave != nil {
		ex.beforeReplacementSave("removal_boundary")
	}
	if err := locked.PlanReplacementRemoval(ac.tool.Name); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("persist removal boundary: %w", err))
	}
	removeCtx, cancelRemove := context.WithTimeout(removeCredentials, 2*time.Minute)
	removeCtx = run.WithOmittedEnv(removeCtx, methodSecretEnvNames(oldMethod)...)
	var stopElevation func()
	if requirer, ok := oldAdapter.(RemovalElevationRequirer); ok && requirer.RequiresRemovalElevation(ac.tool, oldMethod) {
		if session, ok := runner.(run.ElevationSession); ok {
			var elevationErr error
			stopElevation, elevationErr = session.StartElevationSession(removeCtx)
			if elevationErr != nil {
				cancelRemove()
				return ex.failReplacement(ac, result, fmt.Errorf("removal elevation: %w", elevationErr))
			}
		}
	}
	removeErr := oldAdapter.Remove(removeCtx, runner, ac.tool, oldMethod)
	if stopElevation != nil {
		stopElevation()
	}
	cancelRemove()
	if removeErr != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("remove tracked installation: %w", removeErr))
	}
	if err := locked.RecordReplacementRemoved(ac.tool.Name); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("persist removed state: %w", err))
	}
	if err := ac.prepared.planCommit(ac.resources); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("persist preparation commit: %w", err))
	}
	if ex.beforeReplacementSave != nil {
		ex.beforeReplacementSave("install_boundary")
	}
	if err := locked.PlanReplacementInstall(ac.tool.Name); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("persist install boundary: %w", err))
	}
	if err := ac.adapter.InstallResolved(methodCtx, runner, ac.tool, ac.method, ac.reported); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("install exact replacement target: %w", err))
	}
	verification, err := ex.VerifyResolvedCandidate(methodCtx, ac.tool, ac.method, ac.resolved)
	if err != nil || verification.State != plan.StateSatisfied {
		if err == nil {
			err = fmt.Errorf("replacement verification returned %s: %s", verification.State, verification.Detail)
		}
		return ex.failReplacement(ac, result, fmt.Errorf("verify exact replacement target: %w", err))
	}

	result.Status = StatusInstalled
	result.InstallCommitted = true
	result.PreinstallDone = ac.preHookRan
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.Config = configForResolvedTarget(ac.method, ac.resolved)
	result.PlanIntent = ac.reported
	result.ResourceUses = append([]plan.ResourceUse(nil), ac.resources...)
	ex.prepareStateMetadata(locked.State())
	toolState := ex.toolStateForResult(ac.toolCtx, ac.tool, *result, previous, true, !ac.tool.DependencyOnly)
	if ac.resolved.Identity.Version != "" {
		toolState.Version = ac.resolved.Identity.Version
	}
	release, err := plan.ReleaseDependentResources(locked.State().OwnedResources, ac.tool.Name)
	if err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("release prior replacement resources: %w", err))
	}
	owned, err := plan.ClaimResourceUses(release.Updated, ac.tool.Name, ac.resources)
	if err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("claim replacement resources: %w", err))
	}
	var preparationPlan plan.PreparationPlan
	if ac.prepared.tx != nil {
		preparationPlan = ac.prepared.tx.plan
	}
	if err := locked.CommitReplacementInstallWithPreparation(ac.tool.Name, toolState, owned, preparationKey, preparationPlan, ac.tool.Name); err != nil {
		return ex.failReplacement(ac, result, fmt.Errorf("commit verified replacement install: %w", err))
	}
	ac.replacementCommitted = true
	ac.replacementPlanPostHook = func() error {
		if ex.beforeReplacementSave != nil {
			ex.beforeReplacementSave("post_hook_running")
		}
		return locked.PlanReplacementPostHook(ac.tool.Name)
	}
	ac.replacementComplete = func(result *ToolResult) error {
		current := locked.State()
		ex.prepareStateMetadata(current)
		installed, exists := current.Tools[ac.tool.Name]
		if !exists {
			return fmt.Errorf("replacement installed state for %q is missing", ac.tool.Name)
		}
		finalState := ex.toolStateForResult(ac.toolCtx, ac.tool, *result, installed, true, !ac.tool.DependencyOnly)
		if ac.resolved.Identity.Version != "" {
			finalState.Version = ac.resolved.Identity.Version
		}
		return locked.CompleteReplacement(ac.tool.Name, finalState)
	}
	return ex.finishInstalled(ac, result)
}

func (ex *Executor) failReplacement(ac *candidateAttempt, result *ToolResult, err error) attemptOutcome {
	result.Status = StatusFailed
	result.Error = err.Error()
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.Duration = time.Since(ac.toolStart).String()
	ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "replacement_failed", "error", err)
	return finishTool
}
