package exec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/secret"
)

// attemptOutcome tells attemptMethod what to do after a candidate phase runs.
type attemptOutcome int

const (
	// proceed runs the next phase for the same candidate.
	proceed attemptOutcome = iota
	// nextMethod records the attempt and tries the next method candidate.
	nextMethod
	// finishTool records a terminal tool result; tryMethods returns.
	finishTool
)

// candidateAttempt carries per-candidate mutable state across the attempt
// pipeline. tryMethods iterates candidates; each candidate flows through the
// phases in order and each phase either advances, skips to the next
// candidate, or finishes the tool.
type candidateAttempt struct {
	run         *runContext
	toolCtx     context.Context
	tool        *config.Tool
	method      *config.MethodCandidate
	displayKind string
	adapter     AdapterV2
	planIntent  *plan.ResolvedInstallPlan
	resolved    *plan.ResolvedInstallPlan
	reported    *plan.ResolvedInstallPlan
	attempt     MethodAttempt
	prepared    candidateSourcePreparation
	probed      candidateSourcePreparation
	transition  plan.TransitionKind
	preHookRan  bool
	deferred    bool // availability deferred until missing sources are prepared
	resources   []plan.ResourceUse
	toolStart   time.Time
	resolution  *candidateResolutionSeed
}

// candidateResolutionSeed carries a resolution already performed by an
// earlier read-only planning phase (currently native batch preflight). It lets
// serial fallback preserve the one-resolution-per-candidate invariant.
type candidateResolutionSeed struct {
	method   *config.MethodCandidate
	resolved *plan.ResolvedInstallPlan
	err      error
}

// skipCandidate records a non-terminal attempt (skip or recoverable failure)
// and moves on to the next method candidate.
func (ex *Executor) skipCandidate(ac *candidateAttempt, result *ToolResult, status, err string) {
	ac.attempt.Status = status
	ac.attempt.Error = err
	result.Methods = append(result.Methods, ac.attempt)
}

// failCandidate records a failed attempt and a terminal tool failure.
func (ex *Executor) failCandidate(ac *candidateAttempt, result *ToolResult, err string) {
	ex.skipCandidate(ac, result, "failed", err)
	result.Status = StatusFailed
	result.Error = err
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.Duration = time.Since(ac.toolStart).String()
}

// gateStaticIntent builds the static plan intent and enforces the capability
// and when gates. No host probes or mutations happen here.
func (ex *Executor) gateStaticIntent(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	planIntent, mismatch := candidatePlanIntent(ac.tool, ac.method)
	ac.planIntent = ex.hostResolvedPlanIntent(ac.method, planIntent)
	ac.attempt.PlanIntent = ac.planIntent

	if mismatch != "" {
		ex.skipCandidate(ac, result, "skip_capability", mismatch)
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_capability", "reason", mismatch)
		return nextMethod
	}

	if ac.method.When != nil && !ac.method.When.Match(ex.facts) {
		ex.skipCandidate(ac, result, "skip_when", "")
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_when", "requires", fmt.Sprintf("%v", ac.method.When))
		return nextMethod
	}
	return proceed
}

// gateAdapterAvailable resolves the adapter and checks its runtime exists.
// No installation state is consulted here.
func (ex *Executor) gateAdapterAvailable(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	adapter := ex.LookupAdapter(ac.method.Kind)
	if adapter == nil {
		ex.skipCandidate(ac, result, "skip_unavailable", fmt.Sprintf("no adapter for %q", ac.displayKind))
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_no_adapter")
		return nextMethod
	}
	ac.adapter = adapter
	if !adapter.Available(ac.toolCtx, ex.probeRunner(ac.tool.Name, ac.displayKind)) {
		ex.skipCandidate(ac, result, "skip_unavailable", fmt.Sprintf("adapter %q not available", ac.displayKind))
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_unavailable")
		return nextMethod
	}
	return proceed
}

// resolveConcretePlan runs the single read-only resolution point and checks
// host compatibility against the concrete plan. Resolution failures happen
// before any mutation.
func (ex *Executor) resolveConcretePlan(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	var resolved *plan.ResolvedInstallPlan
	var resolveErr error
	if ac.resolution != nil {
		resolved = ac.resolution.resolved
		resolveErr = ac.resolution.err
	} else {
		resolved, resolveErr = ex.resolveCandidatePlan(ac.toolCtx, ac.tool, ac.method, ac.adapter, ac.planIntent, ac.displayKind)
	}
	if resolveErr != nil {
		ex.skipCandidate(ac, result, "failed", resolveErr.Error())
		return nextMethod
	}
	ac.resolved = resolved
	ac.attempt.PlanIntent = resolved

	if compatibilityErr := ac.adapter.CheckHostCompatibility(ac.tool, ac.method, ac.resolved, ex.facts, ex.clan); compatibilityErr != nil {
		ex.skipCandidate(ac, result, "skip_unavailable", compatibilityErr.Error())
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_incompatible_host", "reason", compatibilityErr.Error())
		return nextMethod
	}
	return proceed
}

// gateAlreadyInstalled finishes only when the observed identity satisfies the
// fully resolved desired state. Presence alone is insufficient.
func (ex *Executor) gateAlreadyInstalled(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if ac.resolved == nil {
		return proceed
	}
	verification, err := ex.VerifyResolvedCandidate(ac.toolCtx, ac.tool, ac.method, ac.resolved)
	if err != nil {
		detail := fmt.Sprintf("%s: verify desired state: %v", ac.displayKind, err)
		ex.skipCandidate(ac, result, "failed", detail)
		ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "verification_failed", "error", detail)
		return nextMethod
	}
	decision, decisionErr := plan.TransitionForVerification(verification)
	if decisionErr != nil {
		detail := verificationDetail(verification)
		if detail == "" {
			detail = decisionErr.Error()
		}
		detail = fmt.Sprintf("%s: desired state %s: %s", ac.displayKind, verification.State, detail)
		ex.skipCandidate(ac, result, "failed", detail)
		ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "verification_"+string(verification.State), "error", detail)
		return nextMethod
	}
	if !decision.Required {
		return ex.finishAlreadyInstalled(ac, result)
	}
	ac.transition = decision.Transition
	return proceed
}

func (ex *Executor) finishAlreadyInstalled(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if err := ac.prepared.rollback(ac.toolCtx); err != nil {
		ex.failCandidate(ac, result, fmt.Sprintf("close unused candidate preparation: %v", err))
		return finishTool
	}
	result.Status = StatusAlready
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.Config = configForResolvedTarget(ac.method, ac.resolved)
	result.PlanIntent = ac.resolved
	ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "already_installed")
	result.Duration = time.Since(ac.toolStart).String()
	return finishTool
}

// selectCandidateForTransition finishes candidate viability checks before a
// lifecycle hook is allowed to run. Most candidates are selected entirely by
// read-only probes. A candidate whose repository source is missing may require
// transactional source preparation before availability can be known; that
// preparation is rolled back if the subsequent pre-hook fails.
func (ex *Executor) selectCandidateForTransition(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if out := ex.probeSourceAvailability(ac, result); out != proceed {
		return out
	}
	if out := ex.prepareMissingSources(ac, result); out != proceed {
		return out
	}
	return ex.recheckPostPrepareAvailability(ac, result)
}

// probeSourceAvailability is the read-only part of candidate selection. A
// package backed by a source that is currently absent cannot be judged by
// the manager's current repository index: "not found" may be exactly what
// the declared source is meant to change.
func (ex *Executor) probeSourceAvailability(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	sources, err := sourcesForResolvedPlan(ac.method.Sources, ac.resolved)
	if err != nil {
		ex.skipCandidate(ac, result, "failed", err.Error())
		return nextMethod
	}
	sourceProbe, err := probeCandidateSources(ac.toolCtx, ac.run.sources, sources)
	if err != nil {
		ex.skipCandidate(ac, result, "failed", err.Error())
		return nextMethod
	}
	ac.deferred = len(sourceProbe.missing) > 0
	ac.probed = sourceProbe
	if !ac.deferred && !checkAvailable(ac.toolCtx, ex.probeRunner(ac.tool.Name, ac.displayKind), ac.adapter, ac.tool, ac.method) {
		ex.skipCandidate(ac, result, "skip_unavailable", fmt.Sprintf("%s: package not found in repo/index", ac.displayKind))
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_not_in_repo")
		return nextMethod
	}
	return proceed
}

// prepareMissingSources materializes absent sources transactionally. The WAL
// transaction key identifies the stable candidate intent, not a mutable
// resolved release: persisting the resolved plan here would break recovery
// identity across runs.
func (ex *Executor) prepareMissingSources(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	prepared, err := ex.prepareCandidateSources(ac.toolCtx, ac.run.sources, ac.tool.Name, ac.method.Kind, ac.planIntent, ac.probed)
	if err != nil {
		ex.skipCandidate(ac, result, "failed", err.Error())
		if preparationBlocked(err) {
			result.Status = StatusFailed
			result.Error = err.Error()
			result.Method = ac.displayKind
			result.MethodKind = ac.method.Kind
			result.Duration = time.Since(ac.toolStart).String()
			return finishTool
		}
		return nextMethod
	}
	ac.prepared = prepared
	return proceed
}

// recheckPostPrepareAvailability re-runs the read-only repository check once
// a missing host source exists. A dry-run cannot materialize the source, so
// it intentionally reports the selected prepare+commit plan without
// pretending the old index is authoritative for the post-prepare state.
func (ex *Executor) recheckPostPrepareAvailability(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	// A missing host source makes pre-prepare repository availability
	// inconclusive. If the target is still unavailable, compensate the
	// source transaction and allow fallback.
	if ac.deferred && !ex.dryRun && !checkAvailable(ac.toolCtx, ex.probeRunner(ac.tool.Name, ac.displayKind), ac.adapter, ac.tool, ac.method) {
		if rollbackErr := ac.prepared.rollback(ac.toolCtx); rollbackErr != nil {
			ex.failCandidate(ac, result, fmt.Sprintf("%s: package not found after source preparation; source rollback failed: %v", ac.displayKind, rollbackErr))
			return finishTool
		}
		ex.skipCandidate(ac, result, "skip_unavailable", fmt.Sprintf("%s: package not found in repo/index after source preparation", ac.displayKind))
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_not_in_repo_after_prepare")
		return nextMethod
	}
	return proceed
}

// runCandidatePreinstall executes only the before-hook schedule for the
// candidate and concrete transition selected by verification. The report flag
// is recorded only after that same transition commits.
func (ex *Executor) runCandidatePreinstall(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	preCtx, preCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)
	ran, err := ex.runLifecycleHooks(preCtx, ac.tool.Name, ac.resolved, ac.transition, plan.HookBefore)
	preCancel()
	ac.preHookRan = ran
	if err == nil {
		return proceed
	}

	phase := lifecycleHookPhase(ac.transition, plan.HookBefore)
	detail := fmt.Sprintf("%s: %v", phase, err)
	if rollbackErr := ac.prepared.rollback(ac.toolCtx); rollbackErr != nil {
		detail = fmt.Sprintf("%s; source rollback failed: %v", detail, rollbackErr)
	}
	ex.failCandidate(ac, result, detail)
	ex.logWarn(ac.toolCtx, phase, "tool", ac.tool.Name, "method", ac.displayKind, "error", detail)
	return finishTool
}

// requireMethodPrerequisites installs lazy method.requires edges. They are
// mutations too, so they run only after the candidate has survived every
// availability gate that can be answered before the target install.
func (ex *Executor) requireMethodPrerequisites(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	var prerequisiteUses []plan.ResourceUse
	var err error
	if ac.prepared.tx != nil {
		err = ac.prepared.tx.suspend()
	}
	if err == nil {
		prerequisiteUses, err = ex.ensureMethodDependencies(ac.toolCtx, ac.run, ac.tool, ac.method)
	}
	if ac.prepared.tx != nil {
		if resumeErr := ac.prepared.tx.resume(); err == nil {
			err = resumeErr
		}
	}
	if err == nil && len(ac.method.Requires) > 0 && ac.prepared.tx == nil {
		prepared := ac.prepared
		prepared.transactionRequired = true
		if prepared.preparationPlan == nil {
			empty := plan.PreparationPlan{}
			prepared.preparationPlan = &empty
		}
		prepared, err = ex.prepareCandidateSources(ac.toolCtx, ac.run.sources, ac.tool.Name, ac.method.Kind, ac.planIntent, prepared)
		if err == nil {
			ac.prepared = prepared
		}
	}
	if err != nil {
		rollbackErr := ac.prepared.rollback(ac.toolCtx)
		detail := err.Error()
		if rollbackErr != nil {
			detail = fmt.Sprintf("%s; source rollback failed: %v", detail, rollbackErr)
		}
		ex.skipCandidate(ac, result, "failed", detail)
		if rollbackErr != nil {
			result.Status = StatusFailed
			result.Error = detail
			result.Method = ac.displayKind
			result.MethodKind = ac.method.Kind
			result.Duration = time.Since(ac.toolStart).String()
			return finishTool
		}
		return nextMethod
	}
	ac.resources = append([]plan.ResourceUse(nil), ac.prepared.resourceUses...)
	ac.resources = append(ac.resources, prerequisiteUses...)

	// Preparation is projected into the same concrete plan in both modes:
	// dry-run PlanIntent == plan that would be executed,
	// real PlanIntent == plan that was executed.
	ac.reported = ac.resolved
	if ac.reported != nil && ac.prepared.preparationPlan != nil {
		projected := *ac.reported
		projected.Preparation = ac.prepared.preparationPlan
		ac.reported = &projected
	}
	ac.attempt.PlanIntent = ac.reported
	return proceed
}

// installCandidate executes the dry-run terminal or the real commit+install
// sequence. Plain install failures fall through to the next method; every
// other path finishes the tool.
func (ex *Executor) reconcileFailedOwnerCommit(ac *candidateAttempt) (committed, notApplied bool, err error) {
	if ac == nil || ac.prepared.tx == nil || ac.planIntent == nil || ac.adapter == nil {
		return false, false, errors.New("owner commit recovery context is incomplete")
	}
	observation := ex.observeRecoveryCandidate(ac.toolCtx, ac.tool, ac.method, ac.planIntent, ac.adapter)
	desired := projectVerificationIdentity(ac.method.Kind, ac.planIntent.Identity)
	decision, err := ac.prepared.tx.locked.PreparationRecovery(ac.prepared.tx.key, ac.prepared.tx.plan, &desired, &observation)
	if err != nil {
		return false, false, err
	}
	if decision.Action == plan.RecoveryFinalizeCommit {
		return true, false, nil
	}
	if observation.Presence != plan.PresenceAbsent {
		return false, false, nil
	}
	if _, err := ac.prepared.tx.locked.ResolvePreparationCommitNotApplied(ac.prepared.tx.key, ac.prepared.tx.plan); err != nil {
		return false, false, err
	}
	return false, true, nil
}

func (ex *Executor) installCandidate(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if ex.dryRun {
		return ex.finishWouldInstall(ac, result)
	}

	ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "installing")
	runner := ex.mutationRunner(ac.tool.Name, ac.displayKind)
	methodCtx, methodCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)

	// Resolve auth only after this candidate survives every planning and
	// preparation gate. Credentials stay in this method call's context.
	credentialCtx, credentialErr := ex.executionCredentialContext(methodCtx, ac.method)
	if credentialErr != nil {
		methodCancel()
		detail := credentialErr.Error()
		if rollbackErr := ac.prepared.rollback(ac.toolCtx); rollbackErr != nil {
			detail += "; source rollback failed"
			ex.failCandidate(ac, result, detail)
			return finishTool
		}
		ex.skipCandidate(ac, result, "failed", detail)
		return nextMethod
	}
	methodCtx = credentialCtx

	// Persist the commit boundary before the adapter can mutate the target. If
	// the install process dies after this point, recovery must reconcile the
	// target instead of assuming candidate preparation is safe to undo.
	if err := ac.prepared.planCommit(ac.resources); err != nil {
		methodCancel()
		rollbackErr := ac.prepared.rollback(ac.toolCtx)
		detail := err.Error()
		if rollbackErr != nil {
			detail = fmt.Sprintf("%s; source rollback failed: %v", detail, rollbackErr)
		}
		ex.skipCandidate(ac, result, "failed", detail)
		result.Status = StatusFailed
		result.Error = detail
		result.Method = ac.displayKind
		result.MethodKind = ac.method.Kind
		result.Duration = time.Since(ac.toolStart).String()
		return finishTool
	}

	// method-timeout applies to each individual attempt. The adapter receives
	// the exact plan projected after prerequisite preparation.
	err := ac.adapter.InstallResolved(methodCtx, runner, ac.tool, ac.method, ac.reported)
	methodCancel()

	if err == nil {
		return ex.finishInstalled(ac, result)
	}

	if ac.prepared.tx != nil {
		if !ac.prepared.prerequisite && len(ac.prepared.tx.plan.Prepare) == 0 && len(ac.prepared.tx.plan.CommitUses) > 0 {
			committed, notApplied, probeErr := ex.reconcileFailedOwnerCommit(ac)
			if probeErr == nil && committed {
				return ex.finishInstalled(ac, result)
			}
			if probeErr == nil && notApplied {
				if rollbackErr := ac.prepared.rollback(ac.toolCtx); rollbackErr != nil {
					ex.failCandidate(ac, result, fmt.Sprintf("install failed: %v; preparation rollback failed: %v", err, rollbackErr))
					return finishTool
				}
				ex.skipCandidate(ac, result, "failed", err.Error())
				return nextMethod
			}
		}
		_ = ac.prepared.leaveCommitUnresolved()
		ex.failCandidate(ac, result, fmt.Sprintf("install failed after transactional preparation: %v; commit outcome is unresolved and recovery is required", err))
		ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "commit_unresolved", "error", result.Error)
		return finishTool
	}
	if rollbackErr := ac.prepared.rollback(ac.toolCtx); rollbackErr != nil {
		ex.failCandidate(ac, result, fmt.Sprintf("install failed: %v; source rollback failed: %v", err, rollbackErr))
		ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "rollback_failed", "error", result.Error)
		return finishTool
	}
	ex.skipCandidate(ac, result, "failed", err.Error())
	ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "failed", "error", err.Error())
	return nextMethod
}

type httpCredentialReference struct {
	purpose   HTTPBearerPurpose
	reference plan.SecretReference
}

// Sidecar fields are accessed by name because their config declaration is
// maintained in the config package, outside this execution slice.
func httpCredentialReferences(method *config.MethodCandidate) []httpCredentialReference {
	refs := make([]httpCredentialReference, 0, 3)
	if method.SecretRef != nil {
		refs = append(refs, httpCredentialReference{HTTPBearerArtifact, secretPlanReference(method.SecretRef)})
	}
	if method.ChecksumSecretRef != nil {
		refs = append(refs, httpCredentialReference{HTTPBearerChecksum, secretPlanReference(method.ChecksumSecretRef)})
	}
	if method.SignatureSecretRef != nil {
		refs = append(refs, httpCredentialReference{HTTPBearerSignature, secretPlanReference(method.SignatureSecretRef)})
	}
	return refs
}

// HTTPBearerRequired reports whether a candidate declares a credential for
// the given request purpose. Adapters use it to fail closed if called without
// the executor's resolved runtime context.
func HTTPBearerRequired(method *config.MethodCandidate, purpose HTTPBearerPurpose) bool {
	for _, ref := range httpCredentialReferences(method) {
		if ref.purpose == purpose {
			return true
		}
	}
	return false
}

func secretPlanReference(ref *config.SecretReference) plan.SecretReference {
	return plan.SecretReference{Provider: ref.Provider, Name: ref.Name}
}

func secretResolutionClass(err error, credential string) string {
	switch {
	case errors.Is(err, secret.ErrSecretMissing):
		return "missing"
	case errors.Is(err, secret.ErrSecretEmpty), err == nil && credential == "":
		return "empty"
	case errors.Is(err, secret.ErrUnsupportedProvider):
		return "unsupported provider"
	case errors.Is(err, secret.ErrInvalidReference):
		return "invalid reference"
	default:
		return "resolution failed"
	}
}

// finishWouldInstall records the dry-run terminal result. Planning only:
// post-install hooks are rendered, never executed.
func (ex *Executor) finishWouldInstall(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	result.Status = StatusWouldInstall
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.PlanIntent = ac.reported
	ac.attempt.PlanIntent = ac.reported
	ac.attempt.Status = "success"
	result.Methods = append(result.Methods, ac.attempt)
	ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "would_install")
	postCtx, postCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)
	_, _ = ex.runLifecycleHooks(postCtx, ac.tool.Name, ac.reported, ac.transition, plan.HookAfter)
	postCancel()
	result.Duration = time.Since(ac.toolStart).String()
	return finishTool
}

// finishInstalled records a successful install. A failing post-install hook
// means the tool is not in the state the schema requires, so the tool is
// marked failed instead of being silently reported as installed.
func (ex *Executor) finishInstalled(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	// The adapter mutation already succeeded. Populate the committed result
	// before closing the WAL so lazy prerequisite state can be projected in the
	// same durable save as transaction completion.
	result.Status = StatusInstalled
	result.InstallCommitted = true
	result.PreinstallDone = ac.preHookRan
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.Config = configForResolvedTarget(ac.method, ac.resolved)
	result.PlanIntent = ac.reported
	result.ResourceUses = append([]plan.ResourceUse(nil), ac.resources...)

	var finalizeErr error
	if ac.prepared.tx != nil {
		current := ac.prepared.tx.locked.State()
		ex.prepareStateMetadata(current)
		existing, hadExisting := current.Tools[ac.tool.Name]
		toolState := ex.toolStateForResult(ac.toolCtx, ac.tool, *result, existing, hadExisting, !ac.tool.DependencyOnly)
		var trackedUses []plan.ResourceUse
		if ac.prepared.prerequisite {
			resource, resourceErr := plan.PrerequisiteResource(ac.tool.Name)
			if resourceErr != nil {
				_ = ac.prepared.leaveCommitUnresolved()
				finalizeErr = resourceErr
			} else {
				trackedUses = []plan.ResourceUse{{Resource: resource, Created: true}}
			}
		}
		if finalizeErr == nil {
			finalizeErr = ac.prepared.finalizeCommitWithTool(
				ac.tool.Name,
				ac.tool.Name,
				toolState,
				trackedUses,
			)
		}
	} else {
		finalizeErr = ac.prepared.finalizeCommit(ac.tool.Name)
	}
	if finalizeErr != nil {
		result.Status = StatusFailed
		result.Error = finalizeErr.Error()
		result.Duration = time.Since(ac.toolStart).String()
		return finishTool
	}

	result.RebootRequired, _ = ac.method.Config["_reboot_required"].(bool)
	ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "installed")
	// Post hooks get a fresh timeout from the tool-level context, not the
	// cancelled method context. Only this candidate/transition's schedule is
	// eligible to run.
	postCtx, postCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)
	postRan, perr := ex.runLifecycleHooks(postCtx, ac.tool.Name, ac.reported, ac.transition, plan.HookAfter)
	postCancel()
	if perr != nil {
		result.Status = StatusFailed
		result.Error = fmt.Sprintf("%s: %v", lifecycleHookPhase(ac.transition, plan.HookAfter), perr)
		// The adapter commit already succeeded. Keep source ownership bound
		// to the installed tool instead of removing a repository that the
		// installed package may still depend on for upgrades/removal.
		result.Duration = time.Since(ac.toolStart).String()
		return finishTool
	}
	result.PostinstallDone = postRan
	result.Duration = time.Since(ac.toolStart).String()
	return finishTool
}

// finishExhausted distinguishes a platform-gated tool from one whose
// applicable methods were unavailable once every candidate is spent.
func (ex *Executor) finishExhausted(result *ToolResult, lastMethodKind string, toolStart time.Time) {
	result.Status = StatusSkippedWhen
	for _, m := range result.Methods {
		if m.Status == "failed" {
			result.Status = StatusFailed
			break
		}
		if m.Status != "skip_when" {
			result.Status = StatusSkippedUnavailable
		}
	}
	if len(result.Methods) > 0 {
		last := result.Methods[len(result.Methods)-1]
		result.Error = last.Error
		result.Method = last.Kind
		result.MethodKind = lastMethodKind
		result.PlanIntent = last.PlanIntent
	}
	result.Duration = time.Since(toolStart).String()
}
