package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
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
	toolCtx     context.Context
	tool        *config.Tool
	method      *config.MethodCandidate
	displayKind string
	adapter     Adapter
	installer   ResolvedInstaller // set only for resolving adapters in real mode
	planIntent  *plan.ResolvedInstallPlan
	resolved    *plan.ResolvedInstallPlan
	reported    *plan.ResolvedInstallPlan
	attempt     MethodAttempt
	prepared    candidateSourcePreparation
	probed      candidateSourcePreparation
	deferred    bool // availability deferred until missing sources are prepared
	resources   []plan.ResourceUse
	toolStart   time.Time
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
// host compatibility against the concrete plan. Resolution failures and the
// fail-closed missing-installer error happen before any mutation.
func (ex *Executor) resolveConcretePlan(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	resolved, resolveErr := ex.resolveCandidatePlan(ac.toolCtx, ac.tool, ac.method, ac.adapter, ac.planIntent, ac.displayKind)
	if resolveErr != nil {
		ex.skipCandidate(ac, result, "failed", resolveErr.Error())
		return nextMethod
	}
	ac.resolved = resolved
	ac.attempt.PlanIntent = resolved

	// Fail closed before any mutation: a resolving adapter must execute
	// the exact plan produced above. Silent fallback to Install() would
	// resolve a second time and reinstall the A != B divergence.
	// Dry-run never mutates, so it needs no installer.
	if _, isResolver := ac.adapter.(PlanResolver); isResolver && !ex.dryRun {
		installer, ok := ac.adapter.(ResolvedInstaller)
		if !ok {
			ex.skipCandidate(ac, result, "failed", fmt.Sprintf("%s: adapter resolves plans but does not implement ResolvedInstaller", ac.displayKind))
			return nextMethod
		}
		ac.installer = installer
	}

	if checker, ok := ac.adapter.(HostCompatibilityChecker); ok {
		if compatibilityErr := checker.CheckHostCompatibility(ac.tool, ac.method, ac.resolved, ex.facts, ex.clan); compatibilityErr != nil {
			ex.skipCandidate(ac, result, "skip_unavailable", compatibilityErr.Error())
			ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_incompatible_host", "reason", compatibilityErr.Error())
			return nextMethod
		}
	}
	return proceed
}

// gateAlreadyInstalled finishes the tool when the adapter reports the
// desired state already present. No mutation follows.
func (ex *Executor) gateAlreadyInstalled(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if ac.adapter.Check(ac.toolCtx, ex.probeRunner(ac.tool.Name, ac.displayKind), ac.tool, ac.method) {
		result.Status = StatusAlready
		result.Method = ac.displayKind
		result.MethodKind = ac.method.Kind
		result.Config = ac.method.Config
		result.PlanIntent = ac.resolved
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "already_installed")
		result.Duration = time.Since(ac.toolStart).String()
		return finishTool
	}
	return proceed
}

// prepareCandidate probes source presence, validates availability, prepares
// missing sources transactionally, revalidates after preparation, and
// installs lazy prerequisites. Every gate that can reject the candidate
// runs before the next mutation.
func (ex *Executor) prepareCandidate(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if out := ex.probeSourceAvailability(ac, result); out != proceed {
		return out
	}
	if out := ex.prepareMissingSources(ac, result); out != proceed {
		return out
	}
	if out := ex.recheckPostPrepareAvailability(ac, result); out != proceed {
		return out
	}
	return ex.requireMethodPrerequisites(ac, result)
}

// probeSourceAvailability is the read-only part of candidate selection. A
// package backed by a source that is currently absent cannot be judged by
// the manager's current repository index: "not found" may be exactly what
// the declared source is meant to change.
func (ex *Executor) probeSourceAvailability(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	sourceProbe, err := ex.probeCandidateSources(ac.toolCtx, ac.method.Sources)
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
	prepared, err := ex.prepareCandidateSources(ac.toolCtx, ac.tool.Name, ac.method.Kind, ac.planIntent, ac.probed)
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
		if rollbackErr := ac.prepared.rollback(ac.toolCtx, ex); rollbackErr != nil {
			ex.failCandidate(ac, result, fmt.Sprintf("%s: package not found after source preparation; source rollback failed: %v", ac.displayKind, rollbackErr))
			return finishTool
		}
		ex.skipCandidate(ac, result, "skip_unavailable", fmt.Sprintf("%s: package not found in repo/index after source preparation", ac.displayKind))
		ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "skip_not_in_repo_after_prepare")
		return nextMethod
	}
	return proceed
}

// requireMethodPrerequisites installs lazy method.requires edges. They are
// mutations too, so they run only after the candidate has survived every
// availability gate that can be answered before the target install.
func (ex *Executor) requireMethodPrerequisites(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	prerequisiteUses, err := ex.ensureMethodDependencies(ac.toolCtx, ac.tool, ac.method)
	if err != nil {
		rollbackErr := ac.prepared.rollback(ac.toolCtx, ex)
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
func (ex *Executor) installCandidate(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if ex.dryRun {
		return ex.finishWouldInstall(ac, result)
	}

	ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "installing")
	runner := ex.mutationRunner(ac.tool.Name, ac.displayKind)

	// Persist the commit boundary before the adapter can mutate the target. If
	// the install process dies after this point, recovery must reconcile the
	// target instead of assuming candidate preparation is safe to undo.
	if err := ac.prepared.planCommit(); err != nil {
		rollbackErr := ac.prepared.rollback(ac.toolCtx, ex)
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

	// method-timeout applies to each individual attempt. Resolving
	// adapters execute exactly the plan resolved above; all others use
	// the legacy Install entry point.
	methodCtx, methodCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)
	var err error
	if ac.installer != nil {
		err = ac.installer.InstallResolved(methodCtx, runner, ac.tool, ac.method, ac.resolved)
	} else {
		err = ac.adapter.Install(methodCtx, runner, ac.tool, ac.method)
	}
	methodCancel()

	if err == nil {
		return ex.finishInstalled(ac, result)
	}

	if ac.prepared.tx != nil {
		_ = ac.prepared.leaveCommitUnresolved()
		ex.failCandidate(ac, result, fmt.Sprintf("install failed after transactional preparation: %v; commit outcome is unresolved and recovery is required", err))
		ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "commit_unresolved", "error", result.Error)
		return finishTool
	}
	if rollbackErr := ac.prepared.rollback(ac.toolCtx, ex); rollbackErr != nil {
		ex.failCandidate(ac, result, fmt.Sprintf("install failed: %v; source rollback failed: %v", err, rollbackErr))
		ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "rollback_failed", "error", result.Error)
		return finishTool
	}
	ex.skipCandidate(ac, result, "failed", err.Error())
	ex.logWarn(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "failed", "error", err.Error())
	return nextMethod
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
	if len(ac.tool.PostInstall) > 0 {
		postCtx, postCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)
		_ = ex.runPostinstall(postCtx, ac.tool)
		postCancel()
	}
	result.Duration = time.Since(ac.toolStart).String()
	return finishTool
}

// finishInstalled records a successful install. A failing post-install hook
// means the tool is not in the state the schema requires, so the tool is
// marked failed instead of being silently reported as installed.
func (ex *Executor) finishInstalled(ac *candidateAttempt, result *ToolResult) attemptOutcome {
	if finalizeErr := ac.prepared.finalizeCommit(ac.tool.Name); finalizeErr != nil {
		result.Status = StatusFailed
		result.Error = finalizeErr.Error()
		result.Method = ac.displayKind
		result.MethodKind = ac.method.Kind
		result.Config = ac.method.Config
		result.PlanIntent = ac.reported
		result.InstallCommitted = true
		result.ResourceUses = append([]plan.ResourceUse(nil), ac.resources...)
		result.Duration = time.Since(ac.toolStart).String()
		return finishTool
	}
	result.Status = StatusInstalled
	result.InstallCommitted = true
	result.Method = ac.displayKind
	result.MethodKind = ac.method.Kind
	result.Config = ac.method.Config
	result.PlanIntent = ac.reported
	result.ResourceUses = append([]plan.ResourceUse(nil), ac.resources...)
	result.RebootRequired, _ = ac.method.Config["_reboot_required"].(bool)
	ex.logDebug(ac.toolCtx, "tool", "tool", ac.tool.Name, "method", ac.displayKind, "status", "installed")
	if len(ac.tool.PostInstall) > 0 {
		// Postinstall gets a fresh timeout from the tool-level context,
		// not the cancelled method context.
		postCtx, postCancel := context.WithTimeout(ac.toolCtx, ex.methodTimeout)
		perr := ex.runPostinstall(postCtx, ac.tool)
		postCancel()
		if perr != nil {
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("post-install: %v", perr)
			// The adapter commit already succeeded. Keep source ownership bound
			// to the installed tool instead of removing a repository that the
			// installed package may still depend on for upgrades/removal.
			result.Duration = time.Since(ac.toolStart).String()
			return finishTool
		}
		result.PostinstallDone = true
	}
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
