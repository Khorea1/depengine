package exec

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/source"
	depstate "github.com/Khorea1/depengine/internal/state"
)

// runContext carries execution-wide mutable state across run levels:
// the schema under execution, the accumulating report, and the
// toolName -> reason map of tools that did not get installed (failed or
// unavailable), so dependents in later levels are blocked instead of
// silently proceeding.
type runContext struct {
	ctx               context.Context
	clan              string
	nativeManagerName string
	methodOrder       []string
	sources           *source.Manager
	schema            *config.Schema
	report            *ExecReport
	failed            map[string]string
	recoveredCommits  map[string]recoveredCandidateCommit
	dependencies      map[string]*dependencyRun
	dependencyMu      sync.Mutex
}

func (ex *Executor) newRunContext(ctx context.Context, s *config.Schema, clan string) *runContext {
	configuredMethodOrder := ex.configuredMethodOrder
	if s != nil && s.Defaults.MethodOrder != nil {
		configuredMethodOrder = s.Defaults.MethodOrder
	}
	methodOrder := append([]string{}, configuredMethodOrder...)
	managerName := ""
	if manager, ok := native.Lookup(clan); ok {
		managerName = manager.Name
	}
	return &runContext{
		ctx:               ctx,
		sources:           source.NewManager(ex.rn, ex.dryRun),
		schema:            s,
		clan:              clan,
		nativeManagerName: managerName,
		methodOrder:       methodOrder,
		report:            &ExecReport{},
		failed:            make(map[string]string),
		recoveredCommits:  make(map[string]recoveredCandidateCommit),
		dependencies:      make(map[string]*dependencyRun),
	}
}

func (rc *runContext) selectedMethods(tool *config.Tool) []*config.MethodCandidate {
	return config.SelectMethods(tool, rc.methodOrder, rc.nativeManagerName)
}

// initializeRun establishes upfront interactive elevation. Effective host
// and method selection state was captured when rc was created.
func (ex *Executor) initializeRun(ctx context.Context, rc *runContext) (func(), error) {
	ex.logDebug(ctx, "executor", "phase", "init", "clan", rc.clan, "tools", len(rc.schema.Tools))
	return ex.startElevation(ctx, rc)
}

// startElevation asks the production runner to obtain elevation once,
// upfront, with the real terminal attached, and keep it alive for the rest
// of the run. Without this, every individual elevated command (native.
// withSudo) would rely on sudo's own prompt, which OSExecRunner can
// never deliver (its Stdin/Stdout/Stderr are buffers, not the real
// terminal): elevation would silently fail on every run that isn't
// already NOPASSWD. This is skipped in dry-run: a plan should never
// prompt for credentials it won't use.
func (ex *Executor) startElevation(ctx context.Context, rc *runContext) (func(), error) {
	session, ok := ex.rn.(run.ElevationSession)
	if ex.dryRun || !ex.needsElevation(rc) || !ok {
		return nil, nil
	}
	stop, err := session.StartElevationSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("elevation: %w", err)
	}
	return stop, nil
}

// recoverAndRecord resolves any durable candidate-preparation transaction
// before allowing unrelated new host mutations, then records every
// reconciled commit exactly once before graph execution. Source-only
// transactions can be recovered automatically from source presence;
// ambiguous candidate commits remain fail-closed.
func (ex *Executor) recoverAndRecord(ctx context.Context, rc *runContext) error {
	if err := ex.recoverReplacementTransactions(ctx, rc); err != nil {
		return fmt.Errorf("replacement recovery: %w", err)
	}
	if err := ex.recoverPreparationTransactions(ctx, rc.sources, rc); err != nil {
		return fmt.Errorf("preparation recovery: %w", err)
	}

	// A reconciled commit is already a completed host transition, including
	// DependencyOnly tools that may not appear in the root graph at all. The
	// root and lazy-dependency paths treat these entries as terminal and
	// must not probe, replay hooks, or invoke an installer again.
	recoveredNames := make([]string, 0, len(rc.recoveredCommits))
	for name := range rc.recoveredCommits {
		recoveredNames = append(recoveredNames, name)
	}
	sort.Strings(recoveredNames)
	for _, name := range recoveredNames {
		result := rc.recoveredCommits[name].result()
		ex.recordToolResult(ctx, rc, &result)
	}
	return nil
}

// recoverReplacementTransactions runs before preparation recovery or any new
// host mutation. It observes the exact persisted old and desired candidates,
// then resumes only the journaled destructive step. Candidate names are sorted
// so partial recovery has stable ordering across runs.
func (ex *Executor) recoverReplacementTransactions(ctx context.Context, rc *runContext) error {
	if ex.dryRun || ex.schemaPath == "" {
		return nil
	}
	locked, err := depstate.LoadLocked()
	if err != nil {
		return fmt.Errorf("load replacement recovery state: %w", err)
	}
	defer func() { _ = locked.Close() }()
	names := make([]string, 0, len(locked.State().ReplacementTransactions))
	for name := range locked.State().ReplacementTransactions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ex.recoverReplacementTransaction(ctx, rc, locked, name); err != nil {
			return fmt.Errorf("recover replacement %q: %w", name, err)
		}
	}
	return nil
}

func (ex *Executor) recoverReplacementTransaction(ctx context.Context, rc *runContext, locked *depstate.LockedState, name string) error {
	tx, err := locked.ReplacementTransaction(name)
	if err != nil {
		return err
	}
	tool, ok := rc.schema.Tools[name]
	if !ok || tool == nil {
		return fmt.Errorf("replacement tool %q is absent from the current schema", name)
	}
	method, err := exactReplacementMethod(tool, tx.Candidate, tx.CandidateLabel)
	if err != nil {
		return err
	}
	oldMethod, err := exactReplacementMethod(tool, tx.PreviousCandidate, tx.PreviousCandidateLabel)
	if err != nil {
		return fmt.Errorf("old candidate: %w", err)
	}
	oldMethod.Config = cloneMethodConfig(tx.Previous.Config)
	oldAdapter := ex.LookupAdapter(oldMethod.Kind)
	desiredAdapter := ex.LookupAdapter(method.Kind)
	if oldAdapter == nil || desiredAdapter == nil {
		return fmt.Errorf("replacement adapters are unavailable for old=%q desired=%q", oldMethod.Kind, method.Kind)
	}
	desiredPlan, err := resolvedReplacementTarget(tx.Desired)
	if err != nil {
		return err
	}
	oldPlan := plan.New(name, tx.PreviousCandidate.Method, tx.PreviousCandidate.Explicit)
	oldPlan.Identity.Package = packageName(tool, oldMethod)
	oldPlan.Identity.Version = tx.Previous.Version
	if err := oldPlan.Validate(); err != nil {
		return fmt.Errorf("persisted old candidate: %w", err)
	}

	oldObservation := ex.observeRecoveryCandidate(ctx, tool, oldMethod, &oldPlan, ex.LookupAdapter(oldMethod.Kind))
	desiredObservation := ex.observeRecoveryCandidate(ctx, tool, method, desiredPlan, ex.LookupAdapter(method.Kind))
	oldVerification := plan.Reconcile(projectVerificationIdentity(oldMethod.Kind, oldPlan.Identity), oldObservation)
	if tx.Previous.Version == "" && oldVerification.State == plan.StateSatisfied {
		oldVerification = plan.VerificationResult{State: plan.StateUnknown, Detail: "tracked old candidate has no persisted concrete version"}
	}
	desiredVerification := plan.Reconcile(projectVerificationIdentity(method.Kind, desiredPlan.Identity), desiredObservation)
	if err := oldVerification.Validate(); err != nil {
		return fmt.Errorf("old candidate observation: %w", err)
	}
	if err := desiredVerification.Validate(); err != nil {
		return fmt.Errorf("desired candidate observation: %w", err)
	}
	action, err := locked.ReplacementRecovery(name, oldVerification, desiredVerification)
	if err != nil {
		return err
	}
	if action == plan.ReplacementBlocked {
		return fmt.Errorf("replacement recovery is ambiguous: old=%s desired=%s phase=%s", oldVerification.State, desiredVerification.State, tx.Journal.Phase)
	}

	runner := ex.mutationRunner(name, method.Kind)
	switch action {
	case plan.ReplacementRetryRemoval:
		oldAdapter := ex.LookupAdapter(oldMethod.Kind)
		if oldAdapter == nil || !oldAdapter.CanRemove() {
			return fmt.Errorf("old adapter %q cannot remove the tracked candidate", oldMethod.Kind)
		}
		if err := locked.PlanReplacementRemoval(name); err != nil {
			return err
		}
		removeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		removeErr := oldAdapter.Remove(removeCtx, runner, tool, methodForResolvedTarget(oldMethod, &oldPlan))
		cancel()
		if removeErr != nil {
			return fmt.Errorf("remove tracked candidate: %w", removeErr)
		}
		oldVerification, desiredVerification, err = ex.observeReplacementPair(ctx, tool, oldMethod, method, &oldPlan, desiredPlan)
		if err != nil {
			return err
		}
		if oldVerification.State != plan.StateAbsent || desiredVerification.State != plan.StateAbsent {
			return fmt.Errorf("replacement removal outcome is ambiguous: old=%s desired=%s", oldVerification.State, desiredVerification.State)
		}
		if err := locked.RecordReplacementRemoved(name); err != nil {
			return err
		}
		if err := ex.planReplacementPreparationCommit(locked, tx); err != nil {
			return err
		}
		if err := locked.PlanReplacementInstall(name); err != nil {
			return err
		}
		return ex.resumeReplacementInstall(ctx, rc, locked, tool, method, oldMethod, &oldPlan, desiredPlan, tx)
	case plan.ReplacementRecordRemoved:
		if err := locked.RecordReplacementRemoved(name); err != nil {
			return err
		}
		if err := ex.planReplacementPreparationCommit(locked, tx); err != nil {
			return err
		}
		if err := locked.PlanReplacementInstall(name); err != nil {
			return err
		}
		return ex.resumeReplacementInstall(ctx, rc, locked, tool, method, oldMethod, &oldPlan, desiredPlan, tx)
	case plan.ReplacementStartInstall:
		if tx.Journal.Phase == plan.ReplacementRemoved {
			if err := ex.planReplacementPreparationCommit(locked, tx); err != nil {
				return err
			}
			if err := locked.PlanReplacementInstall(name); err != nil {
				return err
			}
		} else if tx.Journal.Phase != plan.ReplacementInstalling {
			return fmt.Errorf("cannot resume exact install from phase %q", tx.Journal.Phase)
		}
		return ex.resumeReplacementInstall(ctx, rc, locked, tool, method, oldMethod, &oldPlan, desiredPlan, tx)
	case plan.ReplacementRecordInstalled:
		if tx.Journal.Phase == plan.ReplacementRemoved {
			if err := ex.planReplacementPreparationCommit(locked, tx); err != nil {
				return err
			}
			if err := locked.PlanReplacementInstall(name); err != nil {
				return err
			}
		} else if tx.Journal.Phase != plan.ReplacementInstalling {
			return fmt.Errorf("cannot record verified install from phase %q", tx.Journal.Phase)
		}
		result, err := ex.commitRecoveredReplacementInstall(ctx, rc, locked, tool, method, desiredPlan, tx)
		if err != nil {
			return err
		}
		return ex.continueRecoveredReplacement(ctx, rc, locked, tool, method, desiredPlan, result)
	case plan.ReplacementContinueInstalled:
		result := replacementResult(tool, method, desiredPlan)
		return ex.continueRecoveredReplacement(ctx, rc, locked, tool, method, desiredPlan, result)
	default:
		return fmt.Errorf("unsupported replacement recovery action %q", action)
	}
}

func exactReplacementMethod(tool *config.Tool, identity plan.CandidateIdentity, label string) (*config.MethodCandidate, error) {
	var match *config.MethodCandidate
	for _, method := range tool.Methods {
		if method == nil || method.Kind != identity.Method || (!method.Inferred) != identity.Explicit {
			continue
		}
		if label != "" && method.Label != label {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("candidate identity %q with label %q is ambiguous in current schema", identity.Method, label)
		}
		copy := *method
		match = &copy
	}
	if match == nil {
		return nil, fmt.Errorf("candidate identity %q with label %q is absent from current schema", identity.Method, label)
	}
	return match, nil
}

func cloneMethodConfig(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func resolvedReplacementTarget(projection plan.LockProjection) (*plan.ResolvedInstallPlan, error) {
	if err := projection.RequireImmutable(); err != nil {
		return nil, err
	}
	resolved := plan.New(projection.Tool.Name, projection.Candidate.Method, projection.Candidate.Explicit)
	resolved.Identity = plan.ResolvedIdentity{
		Package: projection.Identity.Package, Version: projection.Identity.Version,
		Revision: projection.Identity.Revision, Digest: projection.Identity.Digest,
		Source: projection.Identity.Source, Registry: projection.Identity.Registry,
		Scope: projection.Identity.Scope, Environment: projection.Identity.Environment,
		Architecture: projection.Identity.Architecture, Platform: projection.Identity.Platform,
	}
	if projection.RequestedIntent != nil {
		intent := *projection.RequestedIntent
		if intent.Channel != nil {
			channel := *intent.Channel
			intent.Channel = &channel
		}
		resolved.Identity.RequestedVersion = &intent
	}
	for _, artifact := range projection.Identity.Artifacts {
		resolved.Artifacts = append(resolved.Artifacts, plan.Artifact(artifact))
	}
	for _, source := range projection.Identity.Sources {
		resolved.Sources = append(resolved.Sources, plan.SourceReference{
			Role: source.Role, Kind: source.Kind, Name: source.Name, URL: source.URL,
			Revision: source.Revision, Owned: source.Owned, Trust: source.Trust,
		})
	}
	if err := resolved.Validate(); err != nil {
		return nil, fmt.Errorf("persisted desired candidate: %w", err)
	}
	return &resolved, nil
}

func (ex *Executor) observeReplacementPair(ctx context.Context, tool *config.Tool, oldMethod, desiredMethod *config.MethodCandidate, oldPlan, desiredPlan *plan.ResolvedInstallPlan) (plan.VerificationResult, plan.VerificationResult, error) {
	oldObservation := ex.observeRecoveryCandidate(ctx, tool, oldMethod, oldPlan, ex.LookupAdapter(oldMethod.Kind))
	desiredObservation := ex.observeRecoveryCandidate(ctx, tool, desiredMethod, desiredPlan, ex.LookupAdapter(desiredMethod.Kind))
	oldVerification := plan.Reconcile(projectVerificationIdentity(oldMethod.Kind, oldPlan.Identity), oldObservation)
	desiredVerification := plan.Reconcile(projectVerificationIdentity(desiredMethod.Kind, desiredPlan.Identity), desiredObservation)
	if err := oldVerification.Validate(); err != nil {
		return oldVerification, desiredVerification, err
	}
	if err := desiredVerification.Validate(); err != nil {
		return oldVerification, desiredVerification, err
	}
	return oldVerification, desiredVerification, nil
}

func (ex *Executor) planReplacementPreparationCommit(locked *depstate.LockedState, tx depstate.ReplacementTransaction) error {
	if tx.PreparationKey == "" {
		return nil
	}
	p, journal, err := locked.PreparationTransaction(tx.PreparationKey)
	if err != nil {
		return err
	}
	if journal.Status == plan.PreparationCommitting {
		return nil
	}
	if journal.Status != plan.PreparationReady {
		return fmt.Errorf("preparation transaction %q is %s, not ready to commit", tx.PreparationKey, journal.Status)
	}
	_, err = locked.PlanPreparationCommitWithUses(tx.PreparationKey, p, p.CommitUses)
	return err
}

func (ex *Executor) resumeReplacementInstall(ctx context.Context, rc *runContext, locked *depstate.LockedState, tool *config.Tool, method, oldMethod *config.MethodCandidate, oldPlan, desiredPlan *plan.ResolvedInstallPlan, tx depstate.ReplacementTransaction) error {
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return fmt.Errorf("desired adapter %q is unavailable", method.Kind)
	}
	methodCtx, cancel := context.WithTimeout(ctx, ex.methodTimeout)
	defer cancel()
	credentialCtx, err := ex.executionCredentialContext(methodCtx, method)
	if err != nil {
		return fmt.Errorf("resolve replacement credentials: %w", err)
	}
	if err := adapter.InstallResolved(credentialCtx, ex.mutationRunner(tool.Name, method.Kind), tool, method, desiredPlan); err != nil {
		return fmt.Errorf("resume exact replacement install: %w", err)
	}
	oldVerification, desiredVerification, err := ex.observeReplacementPair(ctx, tool, oldMethod, method, oldPlan, desiredPlan)
	if err != nil {
		return err
	}
	if (oldVerification.State != plan.StateAbsent && oldVerification.State != plan.StateDrifted) || desiredVerification.State != plan.StateSatisfied {
		return fmt.Errorf("replacement install verification is ambiguous: old=%s desired=%s", oldVerification.State, desiredVerification.State)
	}
	result, err := ex.commitRecoveredReplacementInstall(ctx, rc, locked, tool, method, desiredPlan, tx)
	if err != nil {
		return err
	}
	return ex.continueRecoveredReplacement(ctx, rc, locked, tool, method, desiredPlan, result)
}

func replacementRequiresResourceIdentity(tx depstate.ReplacementTransaction, tool *config.Tool, method *config.MethodCandidate) bool {
	return len(tx.Desired.Identity.Sources) > 0 || len(method.Sources) > 0 || len(method.Requires) > 0 || len(tool.Requires) > 0
}

func (ex *Executor) commitRecoveredReplacementInstall(ctx context.Context, rc *runContext, locked *depstate.LockedState, tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan, tx depstate.ReplacementTransaction) (*ToolResult, error) {
	result := replacementResult(tool, method, desired)
	current := locked.State()
	ex.prepareStateMetadata(current)
	previous, hadPrevious := current.Tools[tool.Name]
	toolState := ex.toolStateForResult(ctx, tool, *result, previous, hadPrevious, !tool.DependencyOnly)
	if desired.Identity.Version != "" {
		toolState.Version = desired.Identity.Version
	}
	release, err := plan.ReleaseDependentResources(current.OwnedResources, tool.Name)
	if err != nil {
		return nil, err
	}
	var preparationPlan plan.PreparationPlan
	if tx.PreparationKey != "" {
		preparationPlan, _, err = locked.PreparationTransaction(tx.PreparationKey)
		if err != nil {
			return nil, err
		}
	}
	resourceUses := tx.ResourceUses
	if resourceUses == nil && tx.PreparationKey != "" {
		resourceUses = append([]plan.ResourceUse(nil), preparationPlan.CommitUses...)
	}
	if resourceUses == nil && replacementRequiresResourceIdentity(tx, tool, method) {
		return nil, fmt.Errorf("legacy replacement WAL lacks resource ownership identity for %q", tool.Name)
	}
	owned, err := plan.ClaimResourceUses(release.Updated, tool.Name, resourceUses)
	if err != nil {
		return nil, fmt.Errorf("claim recovered replacement resources: %w", err)
	}
	if err := locked.CommitReplacementInstallWithPreparation(tool.Name, toolState, owned, tx.PreparationKey, preparationPlan, tool.Name); err != nil {
		return nil, fmt.Errorf("commit recovered replacement install: %w", err)
	}
	return result, nil
}

func (ex *Executor) continueRecoveredReplacement(ctx context.Context, rc *runContext, locked *depstate.LockedState, tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan, result *ToolResult) error {
	hookPlan, mismatch := candidatePlanIntent(tool, method)
	if mismatch != "" {
		return fmt.Errorf("resolve replacement lifecycle hooks: %s", mismatch)
	}
	var hooks []plan.LifecycleHook
	if hookPlan != nil {
		var err error
		hooks, err = hookPlan.HookSchedule(plan.TransitionUpgrade, plan.HookAfter)
		if err != nil {
			return fmt.Errorf("resolve replacement lifecycle hooks: %w", err)
		}
	}
	if len(hooks) > 0 {
		if err := locked.PlanReplacementPostHook(tool.Name); err != nil {
			return fmt.Errorf("persist replacement post-hook boundary: %w", err)
		}
		postCtx, cancel := context.WithTimeout(ctx, ex.methodTimeout)
		postRan, hookErr := ex.runLifecycleHooks(postCtx, tool.Name, hookPlan, plan.TransitionUpgrade, plan.HookAfter)
		cancel()
		toolState := locked.State().Tools[tool.Name]
		toolState.PostinstallDone = postRan && hookErr == nil
		if err := locked.CompleteReplacement(tool.Name, toolState); err != nil {
			return fmt.Errorf("complete recovered replacement after post-hook: %w", err)
		}
		if hookErr != nil {
			return fmt.Errorf("replacement after-upgrade hook failed: %w", hookErr)
		}
	} else {
		toolState, ok := locked.State().Tools[tool.Name]
		if !ok {
			return fmt.Errorf("replacement installed state for %q is missing", tool.Name)
		}
		toolState.PostinstallDone = false
		if err := locked.CompleteReplacement(tool.Name, toolState); err != nil {
			return fmt.Errorf("complete recovered replacement: %w", err)
		}
	}
	if result.Method == "" {
		result.Method = method.Kind
	}
	recovered := recoveredCandidateCommit{toolName: tool.Name, methodKind: method.Kind, method: result.Method, config: result.Config, intent: desired}
	rc.recoveredCommits[tool.Name] = recovered
	return nil
}

func replacementResult(tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan) *ToolResult {
	name := method.Label
	if name == "" {
		name = method.Kind
	}
	return &ToolResult{Tool: tool.Name, Status: StatusInstalled, InstallCommitted: true, Method: name, MethodKind: method.Kind, Config: configForResolvedTarget(method, desired), PlanIntent: desired}
}

// syncNativeIndex syncs the native package index only when at least one
// tool uses a native method. Sync() never returns a fatal error: a failed
// index sync is a soft failure and installation proceeds regardless.
func (ex *Executor) syncNativeIndex(ctx context.Context, rc *runContext) {
	if !ex.hasApplicableNativeMethod(rc) {
		return
	}
	syncMgr := NewSyncManager(ex.mutationRunner("native-index", "sync"), rc.clan)
	if !syncMgr.NeedsSync() {
		return
	}
	if ex.dryRun {
		ex.outputf("  package index: would sync via %s\n", rc.nativeManagerName)
		ex.logDebug(ctx, "sync", "status", "would_sync")
		return
	}
	ex.outputf("  syncing package index...\n")
	ex.logDebug(ctx, "sync", "status", "syncing")
	// A failed index sync is a soft failure, already logged at WARN
	// by the runner. Installation proceeds regardless — either
	// against a stale-but-usable native cache, or via tools whose
	// method never depended on this sync in the first place. See
	// findings.md, Achado 1: aborting the whole run here used to
	// veto every tool over one unrelated broken repo.
	_ = syncMgr.Sync(ctx)
	ex.logDebug(ctx, "sync", "status", "done")
}

// sortExecutionLevels validates the full dependency graph (facts-filtered
// requires are edges only on matching platforms) and returns the
// topological levels of root tools to execute in order.
func (ex *Executor) sortExecutionLevels(ctx context.Context, s *config.Schema) ([][]string, error) {
	// Graph sees facts-filtered requires: a gated dep (requires_when) is an
	// edge only on platforms where its condition matches.
	if _, err := graph.Sort(allDependencyEdges(config.FilteredTools(s.Tools, ex.facts))); err != nil {
		return nil, fmt.Errorf("dependency resolution: %w", err)
	}
	toolsForGraph := config.FilteredTools(rootTools(s.Tools), ex.facts)
	levels, err := graph.Sort(toolsForGraph, graph.WithLogger(ex.logger))
	if err != nil {
		return nil, fmt.Errorf("dependency resolution: %w", err)
	}

	ex.logDebug(ctx, "executor", "phase", "graph", "levels", len(levels))
	// Log dependency levels in debug mode (even without --dry-run).
	for i, level := range levels {
		ex.logDebug(ctx, "graph", "level", i, "tools", strings.Join(level, ", "))
	}
	// The level-by-level graph breakdown is internal decision-making detail
	// (why the engine picked this order) rather than "what will happen to
	// my system" — the per-tool ✓/✗/→ lines right below already answer
	// that. Keep it out of the default dry-run so a plain `install
	// --dry-run` reads as a plan, not a debugger dump; --diagnose still
	// gets the full picture.
	if ex.dryRun && ex.diagnose {
		ex.outputf("  dependency order (%d levels):\n", len(levels))
		for i, level := range levels {
			ex.outputf("    level %d: %s\n", i, strings.Join(level, ", "))
		}
	}
	return levels, nil
}

// runLevel executes one topological level through its phase pipeline:
// requires gating, dangerous-code filtering, optimistic batch native install,
// then serial or parallel execution of the rest. Pre-install hooks stay inside
// the candidate attempt pipeline so they are tied to a concrete candidate
// rather than merely to topological eligibility.
func (ex *Executor) runLevel(rc *runContext, level []string) {
	// Reconciled commits were recorded before graph execution and are terminal
	// for this run. Exclude them before dependency/security/hook/batch phases
	// so no transient second probe or host mutation can replay the transition.
	executionLevel := make([]string, 0, len(level))
	for _, toolName := range level {
		if _, ok := rc.recoveredCommits[toolName]; ok {
			continue
		}
		executionLevel = append(executionLevel, toolName)
	}

	blockedByRequires := ex.blockFailedRequires(rc, executionLevel)
	filteredLevel := ex.filterDangerousTools(rc, executionLevel, blockedByRequires)

	remaining, resolutions := ex.runBatchPhase(rc, filteredLevel)
	ex.runRemaining(rc, remaining, resolutions)
	ex.recordLevelFailures(rc, level, blockedByRequires)
}

// blockFailedRequires marks tools whose dependencies failed so they never
// attempt installation. A tool whose dependency failed must not install
// (its runtime prerequisite is absent); it is marked failed so the run
// exits non-zero and its own dependents are blocked transitively.
func (ex *Executor) blockFailedRequires(rc *runContext, executionLevel []string) map[string]string {
	blockedByRequires := make(map[string]string) // toolName -> failure message
	for _, toolName := range executionLevel {
		tool, ok := rc.schema.Tools[toolName]
		if !ok {
			continue
		}
		for _, dep := range tool.EffectiveRequires(ex.facts) {
			if reason, bad := rc.failed[dep]; bad {
				blockedByRequires[toolName] = fmt.Sprintf("requires failed dependency: %s (%s)", dep, reason)
				break
			}
		}
	}
	return blockedByRequires
}

// filterDangerousTools applies the dangerous-code filter BEFORE any
// execution — including PreInstall. Tools blocked by failed requires are
// recorded as failed here; the rest pass through unless they carry
// arbitrary-code execution surfaces without the explicit opt-in.
func (ex *Executor) filterDangerousTools(rc *runContext, executionLevel []string, blockedByRequires map[string]string) []string {
	filteredLevel := make([]string, 0, len(executionLevel))
	for _, toolName := range executionLevel {
		if msg, blocked := blockedByRequires[toolName]; blocked {
			rc.failed[toolName] = msg
			failed := ToolResult{
				Tool: toolName, Status: StatusFailed, Error: msg,
			}
			ex.recordToolResult(rc.ctx, rc, &failed)
			continue
		}
		tool, ok := rc.schema.Tools[toolName]
		if !ok {
			filteredLevel = append(filteredLevel, toolName)
			continue
		}
		if !ex.allowArbitraryCode {
			hasDanger := ex.hasDangerousMethod(rc, tool)
			if hasDanger {
				ex.outputf("  ⚠  %s: has hooks or build scripts that may execute arbitrary code. Use --allow-arbitrary-code to permit execution.\n", toolName)
				ex.logWarn(rc.ctx, "security", "tool", toolName, "warning", "has dangerous hooks")
				ex.recordBlockedTool(rc.ctx, rc, toolName)
				continue
			}
		}
		filteredLevel = append(filteredLevel, toolName)
	}
	return filteredLevel
}

// runBatchPhase attempts the optimistic batch native install and returns
// the tool names still needing per-tool execution.
func (ex *Executor) runBatchPhase(rc *runContext, survivorLevel []string) ([]string, map[string]*candidateResolutionSeed) {
	candidates, remaining, resolutions := ex.identifyBatchCandidates(rc.ctx, rc, survivorLevel)

	// Batch is only an optimization when at least two candidates can share a
	// package-manager invocation. A singleton goes straight to the serial path
	// while retaining the plan resolved during batch preflight.
	if len(candidates) == 1 && rc.clan != "" {
		candidate := candidates[0]
		remaining = append(remaining, candidate.toolName)
		resolutions[candidate.toolName] = &candidateResolutionSeed{method: candidate.method, resolved: candidate.resolvedPlan}
		return remaining, resolutions
	}

	if len(candidates) > 1 && rc.clan != "" {
		switch {
		case ex.dryRun:
			ex.reportBatchDryRun(rc, candidates)
		case ex.batchNativeInstall(omitBatchSecretEnvironment(rc.ctx, candidates), rc, candidates):
			remaining = ex.verifyBatchInstall(rc, candidates, remaining, resolutions)
		default:
			// Batch failed — transparent fallback to per-tool.
			// remaining already excludes tools recorded as StatusAlready by
			// identifyBatchCandidates; we just add the candidates back so they
			// go through the serial path.
			for _, c := range candidates {
				remaining = append(remaining, c.toolName)
				resolutions[c.toolName] = &candidateResolutionSeed{method: c.method, resolved: c.resolvedPlan}
			}
		}
	} // else: no candidates — remaining from identifyBatchCandidates is correct
	return remaining, resolutions
}

// reportBatchDryRun renders the planned batch install without mutating.
func (ex *Executor) reportBatchDryRun(rc *runContext, candidates []batchCandidate) {
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = c.toolName
	}
	ex.outputf("  ⚡  commit: would batch native install: %s via %s\n", strings.Join(names, ", "), rc.nativeManagerName)
	for _, c := range candidates {
		postCtx, postCancel := context.WithTimeout(omitToolSecretEnvironment(rc.ctx, c.tool), ex.methodTimeout)
		_, _ = ex.runLifecycleHooks(postCtx, c.tool.Name, c.resolvedPlan, plan.TransitionInstall, plan.HookAfter)
		postCancel()
		wouldInstall := ToolResult{
			Tool: c.toolName, Status: StatusWouldInstall, Method: displayMethodKind(c.method),
			MethodKind: c.method.Kind, Config: c.method.Config, PlanIntent: c.resolvedPlan,
		}
		ex.recordToolResult(rc.ctx, rc, &wouldInstall)
	}
}

// verifyBatchInstall checks every batch-installed tool individually: only
// tools whose Check passes are marked installed (the manager returning
// exit 0 does not guarantee every package landed — some managers silently
// skip unknown package names). The rest fall back to the serial path,
// which re-tries native and then any remaining methods.
func (ex *Executor) verifyBatchInstall(rc *runContext, candidates []batchCandidate, remaining []string, resolutions map[string]*candidateResolutionSeed) []string {
	for _, c := range candidates {
		verification, ok := ex.batchPresence(omitToolSecretEnvironment(rc.ctx, c.tool), c.tool, c.method, c.resolvedPlan)
		if ok && verification.State == plan.StateSatisfied {
			tr := ToolResult{
				Tool: c.toolName, Status: StatusInstalled, Method: displayMethodKind(c.method),
				MethodKind: c.method.Kind, Config: c.method.Config, PlanIntent: c.resolvedPlan, InstallCommitted: true,
			}
			tr.RebootRequired, _ = c.method.Config["_reboot_required"].(bool)
			postCtx, postCancel := context.WithTimeout(omitToolSecretEnvironment(rc.ctx, c.tool), ex.methodTimeout)
			postRan, err := ex.runLifecycleHooks(postCtx, c.tool.Name, c.resolvedPlan, plan.TransitionInstall, plan.HookAfter)
			postCancel()
			if err != nil {
				tr.Status = StatusFailed
				tr.Error = fmt.Sprintf("post-install: %v", err)
			} else {
				tr.PostinstallDone = postRan
			}
			ex.recordToolResult(rc.ctx, rc, &tr)
		} else {
			remaining = append(remaining, c.toolName)
			if resolutions != nil {
				resolutions[c.toolName] = &candidateResolutionSeed{method: c.method, resolved: c.resolvedPlan}
			}
		}
	}
	return remaining
}

// runRemaining executes leftover tools serially or in parallel within the
// level. Candidate-scoped pre-install hooks run inside the attempt pipeline.
func (ex *Executor) runRemaining(rc *runContext, remaining []string, resolutions map[string]*candidateResolutionSeed) {
	if ex.maxJobs <= 1 || len(remaining) <= 1 {
		for _, toolName := range remaining {
			tool, ok := rc.schema.Tools[toolName]
			if !ok {
				continue
			}
			result := ex.executeToolWithResolution(rc.ctx, rc, tool, resolutions[toolName])
			ex.recordToolResult(rc.ctx, rc, &result)
		}
		return
	}
	ex.executeLevelParallel(rc.ctx, rc, remaining, resolutions)
}

// recordLevelFailures registers this level's failures so dependents in
// later levels are blocked. Tools already recorded as blocked by requires
// are skipped here (they were registered in blockFailedRequires).
func (ex *Executor) recordLevelFailures(rc *runContext, level []string, blockedByRequires map[string]string) {
	for _, toolName := range level {
		if _, blocked := blockedByRequires[toolName]; blocked {
			continue
		}
		tr := recordedResult(rc.report, toolName)
		if tr == nil {
			continue
		}
		if tr.Status == StatusFailed || tr.Status == StatusSkippedWhen || tr.Status == StatusSkippedUnavailable {
			reason := tr.Error
			if reason == "" {
				reason = "not installed"
			}
			rc.failed[toolName] = reason
		}
	}
}

// finishRun sorts, times, logs, and persists the report. Installs may have
// succeeded, but without state the run is not complete: status/diff/sbom
// cannot report what happened.
func (ex *Executor) finishRun(ctx context.Context, s *config.Schema, report *ExecReport, start time.Time) (*ExecReport, error) {
	if ex.sortBy != "" {
		report.SortBy(ex.sortBy)
	}

	report.Duration = time.Since(start)
	// DEBUG, not INFO: the human-readable Summary()/Detail() (and --json for
	// programmatic use) already say this right below, in prose instead of
	// key=value pairs. Logging it again at INFO just doubles up the same
	// information in two different visual languages back to back.
	ex.logDebug(ctx, "executor", "phase", "done",
		"success", report.Success,
		"failed", report.Failed,
		"skipped", report.Skipped,
		"already", report.Already,
		"would_install", report.WouldInstall,
		"duration", report.Duration.String())
	if ex.logger != nil && ex.logger.Enabled(ctx, slog.LevelDebug) {
		ex.logDebug(ctx, "executor", "phase", "report", "json", report.JSON())
	}

	if !ex.dryRun {
		if err := ex.writeState(ctx, s, report); err != nil {
			return nil, fmt.Errorf("persisting state: %w", err)
		}
	}

	return report, nil
}
