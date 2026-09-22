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
)

// hasApplicableNativeMethod reports whether the schema contains a native
// method that applies to the current system. If none applies, both index sync
// and the upfront elevation prompt can be skipped.
func (ex *Executor) hasApplicableNativeMethod(s *config.Schema, clan string) bool {
	if s == nil {
		return false
	}
	for _, tool := range s.Tools {
		for _, mc := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
			if mc.When != nil && !mc.When.Match(ex.facts) {
				continue
			}
			if mc.Kind == "native" || mc.Kind == clan {
				return true
			}
			// Also check native manager aliases (apt, dnf, pacman, etc.).
			if nm, ok := native.ManagerNameToClan(mc.Kind); ok && nm == clan {
				return true
			}
		}
	}
	return false
}

// needsElevation reports whether any applicable method will need root.
func (ex *Executor) needsElevation(s *config.Schema, clan string) bool {
	if ex.preparationRecoveryNeedsElevation() {
		return true
	}
	mgr, ok := native.Lookup(clan)
	if ok && mgr.SudoRequired && ex.hasApplicableNativeMethod(s, clan) {
		return true
	}

	for _, tool := range s.Tools {
		for _, mc := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
			if mc.When != nil && !mc.When.Match(ex.facts) {
				continue
			}
			for _, configuredSource := range mc.Sources {
				if configuredSource.Kind == "apt-ppa" || configuredSource.Kind == "dnf-copr" {
					return true
				}
			}
			adapter := ex.LookupAdapter(mc.Kind)
			requirer, ok := adapter.(ElevationRequirer)
			if ok && requirer.RequiresElevation(tool, mc) {
				return true
			}
		}
	}
	return false
}

func (ex *Executor) Execute(ctx context.Context, s *config.Schema, clan string) (*ExecReport, error) {
	start := time.Now()
	report := &ExecReport{}
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

	// Ask the production runner to obtain elevation once, upfront, with the
	// real terminal attached, and keep it alive for the rest of the
	// run. Without this, every individual elevated command (native.
	// withSudo) would rely on sudo's own prompt, which OSExecRunner can
	// never deliver (its Stdin/Stdout/Stderr are buffers, not the real
	// terminal): elevation would silently fail on every run that isn't
	// already NOPASSWD. This is skipped in dry-run: a plan should never
	// prompt for credentials it won't use.
	if session, ok := ex.rn.(run.ElevationSession); !ex.dryRun && ex.needsElevation(s, clan) && ok {
		stop, err := session.StartElevationSession(ctx)
		if err != nil {
			return nil, fmt.Errorf("elevation: %w", err)
		}
		if stop != nil {
			defer stop()
		}
	}

	// Resolve any durable candidate-preparation transaction before allowing
	// unrelated new host mutations. Source-only transactions can be recovered
	// automatically from source presence; ambiguous candidate commits remain
	// fail-closed.
	if err := ex.recoverPreparationTransactions(ctx); err != nil {
		return nil, fmt.Errorf("preparation recovery: %w", err)
	}

	// A reconciled commit is already a completed host transition. Record every
	// recovered result exactly once before graph execution, including
	// DependencyOnly tools that may not appear in the root graph at all. The
	// root and lazy-dependency paths below treat these entries as terminal and
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

	// Only sync native package index if at least one tool uses a native method.
	if ex.hasApplicableNativeMethod(s, clan) {
		syncMgr := NewSyncManager(ex.mutationRunner("native-index", "sync"), clan)
		if syncMgr.NeedsSync() {
			if ex.dryRun {
				ex.outputf("  package index: would sync via %s\n", ex.nativeManagerName)
				ex.logDebug(ctx, "sync", "status", "would_sync")
			} else {
				ex.outputf("  syncing package index...\n")
				ex.logDebug(ctx, "sync", "status", "syncing")
				// Sync() never returns a fatal error (see its doc comment): a
				// failed index sync is a soft failure, already logged at WARN
				// by the runner. Installation proceeds regardless — either
				// against a stale-but-usable native cache, or via tools whose
				// method never depended on this sync in the first place. See
				// findings.md, Achado 1: aborting the whole run here used to
				// veto every tool over one unrelated broken repo.
				_ = syncMgr.Sync(ctx)
				ex.logDebug(ctx, "sync", "status", "done")
			}
		}
	}

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

	// failedTools accumulates tools that did not get installed (failed or
	// unavailable), so dependents in later levels (requires) are blocked
	// instead of silently proceeding and reporting themselves installed.
	failedTools := make(map[string]string) // toolName -> reason

	for _, level := range levels {
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

		// PHASE 0: requires — a tool whose dependency failed must not attempt
		// to install (its runtime prerequisite is absent). It is marked failed
		// so the run exits non-zero and its own dependents are blocked
		// transitively.
		blockedByRequires := make(map[string]string) // toolName -> failure message
		for _, toolName := range executionLevel {
			tool, ok := s.Tools[toolName]
			if !ok {
				continue
			}
			for _, dep := range tool.EffectiveRequires(ex.facts) {
				if reason, bad := failedTools[dep]; bad {
					blockedByRequires[toolName] = fmt.Sprintf("requires failed dependency: %s (%s)", dep, reason)
					break
				}
			}
		}

		// PHASE 1: Dangerous-code filter (BEFORE any execution — including PreInstall).
		filteredLevel := make([]string, 0, len(executionLevel))
		for _, toolName := range executionLevel {
			if msg, blocked := blockedByRequires[toolName]; blocked {
				failedTools[toolName] = msg
				ex.recordToolResult(ctx, &ToolResult{
					Tool: toolName, Status: StatusFailed, Error: msg,
				}, report)
				continue
			}
			tool, ok := s.Tools[toolName]
			if !ok {
				filteredLevel = append(filteredLevel, toolName)
				continue
			}
			if !ex.allowArbitraryCode {
				hasDanger := ex.hasArbitraryCode(tool)
				if hasDanger {
					ex.outputf("  ⚠  %s: has hooks or build scripts that may execute arbitrary code. Use --allow-arbitrary-code to permit execution.\n", toolName)
					ex.logWarn(ctx, "security", "tool", toolName, "warning", "has dangerous hooks")
					ex.recordBlockedTool(ctx, toolName, report)
					continue
				}
			}
			filteredLevel = append(filteredLevel, toolName)
		}

		// PHASE 2: PreInstall hooks (only for tools that passed the security gate).
		preinstallFailed := make(map[string]bool)
		preinstallDone := make(map[string]bool)
		for _, toolName := range filteredLevel {
			tool, ok := s.Tools[toolName]
			if !ok || len(tool.PreInstall) == 0 {
				continue
			}
			preCtx, preCancel := context.WithTimeout(ctx, ex.methodTimeout)
			err := ex.runPreinstall(preCtx, tool)
			preCancel()
			if err != nil {
				ex.recordToolResult(ctx, &ToolResult{
					Tool:   toolName,
					Status: StatusFailed,
					Error:  fmt.Sprintf("pre-install: %v", err),
				}, report)
				preinstallFailed[toolName] = true
				ex.logWarn(ctx, "preinstall", "tool", toolName, "error", err.Error())
			} else if !ex.dryRun {
				preinstallDone[toolName] = true
			}
		}

		// PHASE 3: Further filter out tools that failed preinstall.
		survivorLevel := make([]string, 0, len(filteredLevel))
		for _, toolName := range filteredLevel {
			if !preinstallFailed[toolName] {
				survivorLevel = append(survivorLevel, toolName)
			}
		}

		// PHASE 4: Optimistic batch native install.
		candidates, remaining := ex.identifyBatchCandidates(ctx, survivorLevel, s, report)

		if len(candidates) > 0 && ex.clan != "" {
			switch {
			case ex.dryRun:
				names := make([]string, len(candidates))
				for i, c := range candidates {
					names[i] = c.toolName
				}
				ex.outputf("  ⚡  commit: would batch native install: %s via %s\n", strings.Join(names, ", "), ex.nativeManagerName)
				for _, c := range candidates {
					if len(c.tool.PostInstall) > 0 {
						postCtx, postCancel := context.WithTimeout(ctx, ex.methodTimeout)
						_ = ex.runPostinstall(postCtx, c.tool)
						postCancel()
					}
					ex.recordToolResult(ctx, &ToolResult{
						Tool: c.toolName, Status: StatusWouldInstall, Method: displayMethodKind(c.method),
						MethodKind: c.method.Kind, Config: c.method.Config, PlanIntent: c.planIntent,
					}, report)
				}
			case ex.batchNativeInstall(ctx, candidates):
				// The manager returning exit 0 does not guarantee every package
				// landed (some managers silently skip unknown package names).
				// Verify per tool: only tools whose Check passes are marked
				// installed; the rest fall back to the serial path, which
				// re-tries native and then any remaining methods.
				for _, c := range candidates {
					adapter := ex.LookupAdapter(c.method.Kind)
					if adapter != nil && adapter.Check(ctx, ex.probeRunner(c.toolName, c.method.Kind), c.tool, c.method) {
						tr := ToolResult{
							Tool: c.toolName, Status: StatusInstalled, Method: displayMethodKind(c.method),
							MethodKind: c.method.Kind, Config: c.method.Config, PlanIntent: c.planIntent, InstallCommitted: true,
						}
						tr.RebootRequired, _ = c.method.Config["_reboot_required"].(bool)
						if preinstallDone[c.toolName] {
							tr.PreinstallDone = true
						}
						if len(c.tool.PostInstall) > 0 {
							postCtx, postCancel := context.WithTimeout(ctx, ex.methodTimeout)
							if err := ex.runPostinstall(postCtx, c.tool); err != nil {
								tr.Status = StatusFailed
								tr.Error = fmt.Sprintf("post-install: %v", err)
							} else {
								tr.PostinstallDone = true
							}
							postCancel()
						}
						ex.recordToolResult(ctx, &tr, report)
					} else {
						remaining = append(remaining, c.toolName)
					}
				}
			default:
				// Batch failed — transparent fallback to per-tool.
				// remaining already excludes tools recorded as StatusAlready by
				// identifyBatchCandidates; we just add the candidates back so they
				// go through the serial path.
				for _, c := range candidates {
					remaining = append(remaining, c.toolName)
				}
			}
		} // else: no candidates — remaining from identifyBatchCandidates is correct

		// PHASE 5: Serial or parallel for remaining.
		// Set PreinstallDone on executeTool results for tools that had successful PreInstall.
		if ex.maxJobs <= 1 || len(remaining) <= 1 {
			for _, toolName := range remaining {
				tool, ok := s.Tools[toolName]
				if !ok {
					continue
				}
				result := ex.executeTool(ctx, tool)
				if preinstallDone[toolName] {
					result.PreinstallDone = true
				}
				ex.recordToolResult(ctx, &result, report)
			}
		} else {
			ex.executeLevelParallel(ctx, s, remaining, report, preinstallDone)
		}

		// Record this level's failures so dependents in later levels are
		// blocked. Tools already recorded as blocked by requires above are
		// skipped here (they were registered at PHASE 0).
		for _, toolName := range level {
			if _, blocked := blockedByRequires[toolName]; blocked {
				continue
			}
			tr := recordedResult(report, toolName)
			if tr == nil {
				continue
			}
			if tr.Status == StatusFailed || tr.Status == StatusSkippedWhen || tr.Status == StatusSkippedUnavailable {
				reason := tr.Error
				if reason == "" {
					reason = "not installed"
				}
				failedTools[toolName] = reason
			}
		}
	}

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
			// The installs may have succeeded, but the run is not complete:
			// without state, status/diff/sbom cannot report what happened.
			return nil, fmt.Errorf("persisting state: %w", err)
		}
	}

	return report, nil
}

func (ex *Executor) executeTool(ctx context.Context, tool *config.Tool) ToolResult {
	toolStart := time.Now()
	result := ToolResult{Tool: tool.Name}
	methods := config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName)
	if len(methods) == 0 {
		result.Status = StatusVirtual
		ex.logDebug(ctx, "tool", "tool", tool.Name, "status", "virtual")
		result.Duration = time.Since(toolStart).String()
		return result
	}

	// Security gate for every arbitrary-code execution surface. Keep this as a
	// defensive duplicate of Execute's phase-1 gate for direct callers.
	if !ex.allowArbitraryCode {
		if ex.hasArbitraryCode(tool) {
			detail := "config includes commands that may execute arbitrary code"
			ex.outputf("  ⚠  %s: %s. Use --allow-arbitrary-code to permit execution.\n", tool.Name, detail)
			ex.logWarn(ctx, "security", "tool", tool.Name, "warning", detail)
			// Defensive duplicate of the PHASE 1 gate in Execute: this path is
			// unreachable from Execute (Phase 1 pre-filters every dangerous
			// tool), but if any future caller reaches executeTool directly,
			// the blocked tool must still count as a failure (exit != 0)
			// rather than silently skipping. StatusFailed is what makes
			// report.Failed>0 and therefore the non-zero exit.
			result.Status = StatusFailed
			result.Error = "requires --allow-arbitrary-code (tool has arbitrary code execution capability)"
			result.Duration = time.Since(toolStart).String()
			ex.logDebug(ctx, "tool", "tool", tool.Name, "status", "blocked_dangerous")
			return result
		}
	}

	// toolTimeout wraps the entire tool execution across all method attempts.
	toolCtx := ctx
	if ex.toolTimeout > 0 {
		var cancel context.CancelFunc
		toolCtx, cancel = context.WithTimeout(ctx, ex.toolTimeout)
		defer cancel()
	}

	ex.tryMethods(toolCtx, tool, &result, toolStart)
	return result
}

// tryMethods iterates through all methods of a tool, trying each in order.
// It modifies result in place — on success the result is terminal; on
// exhaustion it sets the final status from the attempts that were made.
//
// Each candidate flows through the attempt pipeline in order: static
// gating, adapter availability, concrete-plan resolution, already-installed
// check, source preparation with availability gates, then commit+install.
// See candidateAttempt and the phase methods in attempt.go.
func (ex *Executor) tryMethods(toolCtx context.Context, tool *config.Tool, result *ToolResult, toolStart time.Time) {
	var lastMethodKind string
	orderedMethods := config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName)
	for _, method := range orderedMethods {
		lastMethodKind = method.Kind
		select {
		case <-toolCtx.Done():
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("tool timeout (%v) exceeded", ex.toolTimeout)
			ex.logWarn(toolCtx, "tool", "tool", tool.Name, "status", "timeout", "duration", ex.toolTimeout)
			result.Duration = time.Since(toolStart).String()
			return
		default:
		}

		if ex.attemptMethod(toolCtx, tool, method, result, toolStart) {
			return
		}
	}

	ex.finishExhausted(result, lastMethodKind, toolStart)
}

// attemptMethod runs one method candidate through the attempt pipeline.
// It returns true when the tool result is terminal and tryMethods must
// return, false when the next candidate should be tried.
func (ex *Executor) attemptMethod(toolCtx context.Context, tool *config.Tool, method *config.MethodCandidate, result *ToolResult, toolStart time.Time) bool {
	ac := &candidateAttempt{
		toolCtx:     toolCtx,
		tool:        tool,
		method:      method,
		displayKind: displayMethodKind(method),
		attempt:     MethodAttempt{Kind: method.Kind, Label: method.Label},
		toolStart:   toolStart,
	}
	for _, phase := range []func(*candidateAttempt, *ToolResult) attemptOutcome{
		ex.gateStaticIntent,
		ex.gateAdapterAvailable,
		ex.resolveConcretePlan,
		ex.gateAlreadyInstalled,
		ex.prepareCandidate,
		ex.installCandidate,
	} {
		switch phase(ac, result) {
		case nextMethod:
			return false
		case finishTool:
			return true
		}
	}
	return false
}

func sourceResourceUses(sources, added []config.Source) ([]plan.ResourceUse, error) {
	created := make(map[plan.ResourceIdentity]struct{}, len(added))
	for i, configured := range added {
		identity, err := source.ResourceIdentity(configured)
		if err != nil {
			return nil, fmt.Errorf("added source %d: %w", i, err)
		}
		created[identity] = struct{}{}
	}
	uses := make([]plan.ResourceUse, 0, len(sources))
	seen := make(map[plan.ResourceIdentity]struct{}, len(sources))
	for i, configured := range sources {
		identity, err := source.ResourceIdentity(configured)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}
		if _, duplicate := seen[identity]; duplicate {
			return nil, fmt.Errorf("source %d duplicates resource %q", i, identity.Key)
		}
		seen[identity] = struct{}{}
		_, wasCreated := created[identity]
		uses = append(uses, plan.ResourceUse{Resource: identity, Created: wasCreated})
	}
	return uses, nil
}

func allDependencyEdges(tools map[string]*config.Tool) map[string]*config.Tool {
	out := make(map[string]*config.Tool, len(tools))
	for name, tool := range tools {
		clone := *tool
		clone.Requires = append([]string(nil), tool.Requires...)
		seen := make(map[string]bool, len(clone.Requires))
		for _, dep := range clone.Requires {
			seen[dep] = true
		}
		for _, method := range tool.Methods {
			for _, dep := range method.Requires {
				if !seen[dep] {
					clone.Requires = append(clone.Requires, dep)
					seen[dep] = true
				}
			}
		}
		out[name] = &clone
	}
	return out
}

func rootTools(tools map[string]*config.Tool) map[string]*config.Tool {
	out := make(map[string]*config.Tool, len(tools))
	queue := make([]string, 0, len(tools))
	for name, tool := range tools {
		if !tool.DependencyOnly {
			queue = append(queue, name)
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, exists := out[name]; exists {
			continue
		}
		tool, exists := tools[name]
		if !exists {
			continue
		}
		out[name] = tool
		queue = append(queue, tool.Requires...)
	}
	return out
}

func (ex *Executor) ensureMethodDependencies(ctx context.Context, owner *config.Tool, method *config.MethodCandidate) ([]plan.ResourceUse, error) {
	uses := make([]plan.ResourceUse, 0, len(method.Requires))
	seen := make(map[plan.ResourceIdentity]struct{}, len(method.Requires))
	for _, name := range method.Requires {
		result, err := ex.executeDependency(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("%s: method %s requires %s: %w", owner.Name, method.Kind, name, err)
		}
		if result.Status != StatusInstalled && result.Status != StatusAlready && result.Status != StatusWouldInstall && result.Status != StatusVirtual {
			return nil, fmt.Errorf("%s: method %s requires %s: %s", owner.Name, method.Kind, name, result.Error)
		}
		if result.Status == StatusVirtual {
			continue
		}
		resource, err := plan.PrerequisiteResource(name)
		if err != nil {
			return nil, fmt.Errorf("%s: method %s prerequisite %s: %w", owner.Name, method.Kind, name, err)
		}
		if _, duplicate := seen[resource]; duplicate {
			return nil, fmt.Errorf("%s: method %s declares duplicate prerequisite %s", owner.Name, method.Kind, name)
		}
		seen[resource] = struct{}{}
		uses = append(uses, plan.ResourceUse{
			Resource: resource,
			Created:  result.Status == StatusInstalled,
		})
	}
	return uses, nil
}

func (ex *Executor) executeDependency(ctx context.Context, name string) (ToolResult, error) {
	// Startup recovery has already reconciled and recorded this exact candidate.
	// A lazy method.requires edge must consume that terminal result rather than
	// probing or installing the dependency a second time.
	if recovered, ok := ex.recoveredCommits[name]; ok {
		return recovered.result(), nil
	}

	ex.dependencyMu.Lock()
	if existing := ex.dependencies[name]; existing != nil {
		ex.dependencyMu.Unlock()
		select {
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		case <-existing.done:
			return existing.result, nil
		}
	}
	run := &dependencyRun{done: make(chan struct{})}
	ex.dependencies[name] = run
	ex.dependencyMu.Unlock()

	tool := ex.schema.Tools[name]
	if tool == nil {
		run.result = ToolResult{Tool: name, Status: StatusFailed, Error: "dependency is not defined"}
	} else {
		blocked := false
		for _, dependency := range tool.EffectiveRequires(ex.facts) {
			result, err := ex.executeDependency(ctx, dependency)
			if err != nil || !dependencySucceeded(result) {
				reason := result.Error
				if err != nil {
					reason = err.Error()
				}
				run.result = ToolResult{Tool: name, Status: StatusFailed, Error: fmt.Sprintf("requires failed dependency: %s (%s)", dependency, reason)}
				blocked = true
				break
			}
		}
		if !blocked {
			run.result = ex.executeTool(ctx, tool)
		}
		ex.recordToolResult(ctx, &run.result, ex.report)
	}
	close(run.done)
	return run.result, nil
}

func dependencySucceeded(result ToolResult) bool {
	return result.Status == StatusInstalled || result.Status == StatusAlready || result.Status == StatusWouldInstall || result.Status == StatusVirtual
}

// recordedResult returns the last ToolResult recorded for toolName, or nil.
// Callers must invoke it only after the tool's level has finished executing
// (no concurrent writers at that point); the mutex is held defensively.
func recordedResult(report *ExecReport, toolName string) *ToolResult {
	report.mu.Lock()
	defer report.mu.Unlock()
	for i := len(report.Tools) - 1; i >= 0; i-- {
		if report.Tools[i].Tool == toolName {
			return &report.Tools[i]
		}
	}
	return nil
}

// executeLevelParallel runs all tools in a topological level concurrently,
// limiting concurrency to ex.maxJobs. Results are collected thread-safely
// via recordToolResult.
func (ex *Executor) executeLevelParallel(ctx context.Context, s *config.Schema, level []string, report *ExecReport, preinstallDone map[string]bool) {
	toolCh := make(chan string, len(level))
	resultCh := make(chan ToolResult, len(level))

	// Seed the tool channel.
	for _, name := range level {
		toolCh <- name
	}
	close(toolCh)

	// Worker pool: at most maxJobs workers, but cap at level size.
	var wg sync.WaitGroup
	numWorkers := ex.maxJobs
	if numWorkers > len(level) {
		numWorkers = len(level)
	}

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for toolName := range toolCh {
				tool, ok := s.Tools[toolName]
				if !ok {
					continue
				}
				result := ex.executeTool(ctx, tool)
				if preinstallDone[toolName] {
					result.PreinstallDone = true
				}
				resultCh <- result
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	// Collect results into a slice, then sort by tool name for deterministic
	// output across executions with --jobs > 1.
	results := make([]ToolResult, 0, len(level))
	for result := range resultCh {
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Tool < results[j].Tool
	})
	for i := range results {
		ex.recordToolResult(ctx, &results[i], report)
	}
}

// log emits a structured log entry at the given level, if a logger is set.
func (ex *Executor) log(ctx context.Context, level slog.Level, msg string, attrs ...any) {
	if ex.logger != nil {
		ex.logger.Log(ctx, level, msg, attrs...)
	}
}

func (ex *Executor) logDebug(ctx context.Context, msg string, attrs ...any) {
	ex.log(ctx, slog.LevelDebug, msg, attrs...)
}
func (ex *Executor) logWarn(ctx context.Context, msg string, attrs ...any) {
	ex.log(ctx, slog.LevelWarn, msg, attrs...)
}

// outputf formats user-facing output (status lines, sync messages, etc.).
func (ex *Executor) outputf(format string, args ...any) {
	if ex.outWriter != nil {
		fmt.Fprintf(ex.outWriter, format, args...)
	}
}
