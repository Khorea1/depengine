package exec

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/source"
)

// runContext carries execution-wide mutable state across run levels:
// the schema under execution, the accumulating report, and the
// toolName -> reason map of tools that did not get installed (failed or
// unavailable), so dependents in later levels are blocked instead of
// silently proceeding.
type runContext struct {
	ctx    context.Context
	schema *config.Schema
	report *ExecReport
	failed map[string]string
}

// initializeRun sets the executor's per-run state and establishes the
// upfront interactive elevation session when the run needs it. It returns
// the session stop func for the caller to defer; a nil stop needs no defer.
func (ex *Executor) initializeRun(ctx context.Context, s *config.Schema, clan string, report *ExecReport) (func(), error) {
	ex.schema = s
	ex.report = report
	ex.sources = source.NewManager(ex.rn, ex.dryRun)
	ex.recoveredCommits = make(map[string]recoveredCandidateCommit)
	ex.dependencies = make(map[string]*dependencyRun)

	ex.clan = clan

	// Resolve native manager name from clan for method_order expansion.
	if mgr, ok := native.Lookup(clan); ok {
		ex.nativeManagerName = mgr.Name
	}
	if len(s.Defaults.MethodOrder) > 0 {
		ex.defaultMethodOrder = s.Defaults.MethodOrder
	}

	ex.logDebug(ctx, "executor", "phase", "init", "clan", clan, "tools", len(s.Tools))

	return ex.startElevation(ctx, s, clan)
}

// startElevation asks the production runner to obtain elevation once,
// upfront, with the real terminal attached, and keep it alive for the rest
// of the run. Without this, every individual elevated command (native.
// withSudo) would rely on sudo's own prompt, which OSExecRunner can
// never deliver (its Stdin/Stdout/Stderr are buffers, not the real
// terminal): elevation would silently fail on every run that isn't
// already NOPASSWD. This is skipped in dry-run: a plan should never
// prompt for credentials it won't use.
func (ex *Executor) startElevation(ctx context.Context, s *config.Schema, clan string) (func(), error) {
	session, ok := ex.rn.(run.ElevationSession)
	if ex.dryRun || !ex.needsElevation(s, clan) || !ok {
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
func (ex *Executor) recoverAndRecord(ctx context.Context, report *ExecReport) error {
	if err := ex.recoverPreparationTransactions(ctx); err != nil {
		return fmt.Errorf("preparation recovery: %w", err)
	}

	// A reconciled commit is already a completed host transition, including
	// DependencyOnly tools that may not appear in the root graph at all. The
	// root and lazy-dependency paths treat these entries as terminal and
	// must not probe, replay hooks, or invoke an installer again.
	recoveredNames := make([]string, 0, len(ex.recoveredCommits))
	for name := range ex.recoveredCommits {
		recoveredNames = append(recoveredNames, name)
	}
	sort.Strings(recoveredNames)
	for _, name := range recoveredNames {
		result := ex.recoveredCommits[name].result()
		ex.recordToolResult(ctx, &result, report)
	}
	return nil
}

// syncNativeIndex syncs the native package index only when at least one
// tool uses a native method. Sync() never returns a fatal error: a failed
// index sync is a soft failure and installation proceeds regardless.
func (ex *Executor) syncNativeIndex(ctx context.Context, s *config.Schema, clan string) {
	if !ex.hasApplicableNativeMethod(s, clan) {
		return
	}
	syncMgr := NewSyncManager(ex.mutationRunner("native-index", "sync"), clan)
	if !syncMgr.NeedsSync() {
		return
	}
	if ex.dryRun {
		ex.outputf("  package index: would sync via %s\n", ex.nativeManagerName)
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
// requires gating, dangerous-code filtering, preinstall hooks, optimistic
// batch native install, then serial or parallel execution of the rest.
func (ex *Executor) runLevel(rc *runContext, level []string) {
	// Reconciled commits were recorded before graph execution and are terminal
	// for this run. Exclude them before dependency/security/hook/batch phases
	// so no transient second probe or host mutation can replay the transition.
	executionLevel := make([]string, 0, len(level))
	for _, toolName := range level {
		if _, ok := ex.recoveredCommits[toolName]; ok {
			continue
		}
		executionLevel = append(executionLevel, toolName)
	}

	blockedByRequires := ex.blockFailedRequires(rc, executionLevel)
	filteredLevel := ex.filterDangerousTools(rc, executionLevel, blockedByRequires)
	preinstallFailed, preinstallDone := ex.runPreinstallHooks(rc, filteredLevel)

	// Further filter out tools that failed preinstall.
	survivorLevel := make([]string, 0, len(filteredLevel))
	for _, toolName := range filteredLevel {
		if !preinstallFailed[toolName] {
			survivorLevel = append(survivorLevel, toolName)
		}
	}

	remaining, resolutions := ex.runBatchPhase(rc, survivorLevel, preinstallDone)
	ex.runRemaining(rc, remaining, preinstallDone, resolutions)
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
			ex.recordToolResult(rc.ctx, &failed, rc.report)
			continue
		}
		tool, ok := rc.schema.Tools[toolName]
		if !ok {
			filteredLevel = append(filteredLevel, toolName)
			continue
		}
		if !ex.allowArbitraryCode {
			hasDanger := ex.hasArbitraryCode(tool)
			if hasDanger {
				ex.outputf("  ⚠  %s: has hooks or build scripts that may execute arbitrary code. Use --allow-arbitrary-code to permit execution.\n", toolName)
				ex.logWarn(rc.ctx, "security", "tool", toolName, "warning", "has dangerous hooks")
				ex.recordBlockedTool(rc.ctx, toolName, rc.report)
				continue
			}
		}
		filteredLevel = append(filteredLevel, toolName)
	}
	return filteredLevel
}

// runPreinstallHooks runs PreInstall hooks for tools that passed the
// security gate. It returns the sets of tools whose hooks failed and whose
// hooks succeeded in real (non-dry-run) mode.
func (ex *Executor) runPreinstallHooks(rc *runContext, filteredLevel []string) (failed, done map[string]bool) {
	preinstallFailed := make(map[string]bool)
	preinstallDone := make(map[string]bool)
	for _, toolName := range filteredLevel {
		tool, ok := rc.schema.Tools[toolName]
		if !ok || len(tool.PreInstall) == 0 {
			continue
		}
		preCtx, preCancel := context.WithTimeout(omitToolSecretEnvironment(rc.ctx, tool), ex.methodTimeout)
		err := ex.runPreinstall(preCtx, tool)
		preCancel()
		if err != nil {
			failedResult := ToolResult{
				Tool:   toolName,
				Status: StatusFailed,
				Error:  fmt.Sprintf("pre-install: %v", err),
			}
			ex.recordToolResult(rc.ctx, &failedResult, rc.report)
			preinstallFailed[toolName] = true
			ex.logWarn(rc.ctx, "preinstall", "tool", toolName, "error", err.Error())
		} else if !ex.dryRun {
			preinstallDone[toolName] = true
		}
	}
	return preinstallFailed, preinstallDone
}

// runBatchPhase attempts the optimistic batch native install and returns
// the tool names still needing per-tool execution.
func (ex *Executor) runBatchPhase(rc *runContext, survivorLevel []string, preinstallDone map[string]bool) ([]string, map[string]*candidateResolutionSeed) {
	candidates, remaining, resolutions := ex.identifyBatchCandidates(rc.ctx, survivorLevel, rc.schema, rc.report)

	if len(candidates) > 0 && ex.clan != "" {
		switch {
		case ex.dryRun:
			ex.reportBatchDryRun(rc, candidates)
		case ex.batchNativeInstall(omitBatchSecretEnvironment(rc.ctx, candidates), candidates):
			remaining = ex.verifyBatchInstall(rc, candidates, remaining, preinstallDone, resolutions)
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
	ex.outputf("  ⚡  commit: would batch native install: %s via %s\n", strings.Join(names, ", "), ex.nativeManagerName)
	for _, c := range candidates {
		if len(c.tool.PostInstall) > 0 {
			postCtx, postCancel := context.WithTimeout(omitToolSecretEnvironment(rc.ctx, c.tool), ex.methodTimeout)
			_ = ex.runPostinstall(postCtx, c.tool)
			postCancel()
		}
		wouldInstall := ToolResult{
			Tool: c.toolName, Status: StatusWouldInstall, Method: displayMethodKind(c.method),
			MethodKind: c.method.Kind, Config: c.method.Config, PlanIntent: c.resolvedPlan,
		}
		ex.recordToolResult(rc.ctx, &wouldInstall, rc.report)
	}
}

// verifyBatchInstall checks every batch-installed tool individually: only
// tools whose Check passes are marked installed (the manager returning
// exit 0 does not guarantee every package landed — some managers silently
// skip unknown package names). The rest fall back to the serial path,
// which re-tries native and then any remaining methods.
func (ex *Executor) verifyBatchInstall(rc *runContext, candidates []batchCandidate, remaining []string, preinstallDone map[string]bool, resolutions map[string]*candidateResolutionSeed) []string {
	for _, c := range candidates {
		adapter := ex.LookupAdapter(c.method.Kind)
		presence, ok := ex.batchPresence(omitToolSecretEnvironment(rc.ctx, c.tool), adapter, c.toolName, c.tool, c.method)
		if ok && presence == plan.PresencePresent {
			tr := ToolResult{
				Tool: c.toolName, Status: StatusInstalled, Method: displayMethodKind(c.method),
				MethodKind: c.method.Kind, Config: c.method.Config, PlanIntent: c.resolvedPlan, InstallCommitted: true,
			}
			tr.RebootRequired, _ = c.method.Config["_reboot_required"].(bool)
			if preinstallDone[c.toolName] {
				tr.PreinstallDone = true
			}
			if len(c.tool.PostInstall) > 0 {
				postCtx, postCancel := context.WithTimeout(omitToolSecretEnvironment(rc.ctx, c.tool), ex.methodTimeout)
				if err := ex.runPostinstall(postCtx, c.tool); err != nil {
					tr.Status = StatusFailed
					tr.Error = fmt.Sprintf("post-install: %v", err)
				} else {
					tr.PostinstallDone = true
				}
				postCancel()
			}
			ex.recordToolResult(rc.ctx, &tr, rc.report)
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
// level. Results carry PreinstallDone for tools with successful PreInstall.
func (ex *Executor) runRemaining(rc *runContext, remaining []string, preinstallDone map[string]bool, resolutions map[string]*candidateResolutionSeed) {
	if ex.maxJobs <= 1 || len(remaining) <= 1 {
		for _, toolName := range remaining {
			tool, ok := rc.schema.Tools[toolName]
			if !ok {
				continue
			}
			result := ex.executeToolWithResolution(rc.ctx, tool, resolutions[toolName])
			if preinstallDone[toolName] {
				result.PreinstallDone = true
			}
			ex.recordToolResult(rc.ctx, &result, rc.report)
		}
		return
	}
	ex.executeLevelParallel(rc.ctx, rc.schema, remaining, rc.report, preinstallDone, resolutions)
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
