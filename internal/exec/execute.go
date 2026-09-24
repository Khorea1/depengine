package exec

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Khorea1/depengine/internal/config"
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
	housekeepingCtx := run.WithOmittedEnv(ctx, schemaSecretEnvNames(s)...)

	stop, err := ex.initializeRun(housekeepingCtx, s, clan, report)
	if err != nil {
		return nil, err
	}
	if stop != nil {
		defer stop()
	}

	if err := ex.recoverAndRecord(housekeepingCtx, report); err != nil {
		return nil, err
	}

	ex.syncNativeIndex(housekeepingCtx, s, clan)

	levels, err := ex.sortExecutionLevels(ctx, s)
	if err != nil {
		return nil, err
	}

	// failedTools accumulates tools that did not get installed (failed or
	// unavailable), so dependents in later levels (requires) are blocked
	// instead of silently proceeding and reporting themselves installed.
	rc := &runContext{ctx: ctx, schema: s, report: report, failed: make(map[string]string)}
	for _, level := range levels {
		ex.runLevel(rc, level)
	}

	return ex.finishRun(ctx, s, report, start)
}

func (ex *Executor) executeTool(ctx context.Context, tool *config.Tool) ToolResult {
	return ex.executeToolWithResolution(ctx, tool, nil)
}

func (ex *Executor) executeToolWithResolution(ctx context.Context, tool *config.Tool, resolution *candidateResolutionSeed) ToolResult {
	ctx = omitToolSecretEnvironment(ctx, tool)
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

	ex.tryMethodsWithResolution(toolCtx, tool, &result, toolStart, resolution)
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
	ex.tryMethodsWithResolution(toolCtx, tool, result, toolStart, nil)
}

func (ex *Executor) tryMethodsWithResolution(toolCtx context.Context, tool *config.Tool, result *ToolResult, toolStart time.Time, resolution *candidateResolutionSeed) {
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

		var methodResolution *candidateResolutionSeed
		if resolution != nil && resolution.method == method {
			methodResolution = resolution
		}
		if ex.attemptMethod(toolCtx, tool, method, result, toolStart, methodResolution) {
			return
		}
	}

	ex.finishExhausted(result, lastMethodKind, toolStart)
}

// attemptMethod runs one method candidate through the attempt pipeline.
// It returns true when the tool result is terminal and tryMethods must
// return, false when the next candidate should be tried.
func (ex *Executor) attemptMethod(toolCtx context.Context, tool *config.Tool, method *config.MethodCandidate, result *ToolResult, toolStart time.Time, resolution *candidateResolutionSeed) bool {
	ac := &candidateAttempt{
		toolCtx:     toolCtx,
		tool:        tool,
		method:      method,
		displayKind: displayMethodKind(method),
		attempt:     MethodAttempt{Kind: method.Kind, Label: method.Label},
		toolStart:   toolStart,
		resolution:  resolution,
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
func (ex *Executor) executeLevelParallel(ctx context.Context, s *config.Schema, level []string, report *ExecReport, preinstallDone map[string]bool, resolutions map[string]*candidateResolutionSeed) {
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
				result := ex.executeToolWithResolution(ctx, tool, resolutions[toolName])
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
