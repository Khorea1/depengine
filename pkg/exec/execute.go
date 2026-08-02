package exec

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/graph"
	"github.com/Khorea1/depengine/pkg/native"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
)

// Execute runs all tools in the schema in dependency order.
// needsNativeSync reports whether the schema contains any tool with a native
// method for the given clan. If no tool uses native, index sync is skipped.
func needsNativeSync(s *config.Schema, clan string) bool {
	if s == nil {
		return false
	}
	for _, tool := range s.Tools {
		for _, mc := range tool.Methods {
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

// batchCandidate represents one tool eligible for batch native install.
type batchCandidate struct {
	toolName string
	tool     *config.Tool
	method   *config.MethodCandidate
	pkg      string // resolved package name (with clan-specific overrides)
}

// identifyBatchCandidates examines all tools in a level and returns those
// whose next non-skipped method candidate is "native". It also records
// already-installed tools directly into the report (StatusAlready) and
// excludes them from the remaining set.
func (ex *Executor) identifyBatchCandidates(ctx context.Context, level []string, s *config.Schema, report *ExecReport) (candidates []batchCandidate, remaining []string) {
	remaining = make([]string, 0, len(level))

	for _, toolName := range level {
		tool, ok := s.Tools[toolName]
		if !ok {
			continue
		}
		if len(tool.Methods) == 0 {
			remaining = append(remaining, toolName)
			continue
		}

		orderedMethods := config.OrderMethods(tool.Methods, ex.effectiveMethodOrder(tool))

		foundNative := false
		for _, method := range orderedMethods {
			if method.When != nil && !method.When.Match(ex.facts) {
				continue
			}

			adapter := ex.LookupAdapter(method.Kind)
			if adapter == nil {
				continue
			}

			if !adapter.Available(ctx, ex.rn) {
				continue
			}

			isNative := method.Kind == "native" || native.IsNativeManagerName(method.Kind)

			if !isNative {
				break
			}

			// Already installed — record and exclude from remaining.
			if adapter.Check(ctx, ex.rn, tool, method) {
				ex.recordToolResult(ctx, &ToolResult{
					Tool:   toolName,
					Status: StatusAlready,
					Method: method.Kind,
				}, report)
				foundNative = true
				break
			}

			// Resolve package name for batch.
			pkg := pkgFromConfig(method, ex.clan)
			if pkg == "" {
				break
			}

			if !validBatchPkgName(pkg) {
				break
			}

			if !native.IsBatchCapable(ex.clan) {
				break
			}

			candidates = append(candidates, batchCandidate{
				toolName: toolName,
				tool:     tool,
				method:   method,
				pkg:      pkg,
			})
			foundNative = true
			break
		}

		if !foundNative {
			remaining = append(remaining, toolName)
		}
	}

	return candidates, remaining
}

// validBatchPkgName checks that a package name is safe for batch inclusion.
// We reject names that could be misinterpreted as command-line flags.
var pkgNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+-_]*$`)

func validBatchPkgName(name string) bool {
	return pkgNameRegexp.MatchString(name)
}

// batchNativeInstall runs a single elevated command to install all candidate
// packages. Returns true if the batch succeeded (exit code 0), false if it
// failed (any other exit). On failure, the caller MUST NOT mark any tool as
// installed — the serial fallback handles each tool independently.
func (ex *Executor) batchNativeInstall(ctx context.Context, candidates []batchCandidate) bool {
	pkgs := make([]string, len(candidates))
	for i, c := range candidates {
		pkgs[i] = c.pkg
	}

	cmd := native.BuildBatchInstallCmd(ex.clan, pkgs)
	if cmd == nil {
		return false
	}

	timeout := ex.methodTimeout * time.Duration(max(1, len(pkgs)))
	if ex.batchTimeout > 0 {
		timeout = ex.batchTimeout
	}
	if ex.toolTimeout > 0 && timeout > ex.toolTimeout {
		timeout = ex.toolTimeout
	}

	batchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runner := ex.rn
	if lr, ok := runner.(*run.LoggingRunner); ok {
		runner = lr.WithContext(run.Context{Tool: "batch", Method: "native"})
	}

	ex.outputf("  ⚡  batch installing %d packages via %s (1 elevation)\n", len(pkgs), ex.nativeManagerName)
	ex.logDebug(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "started")

	res := runner.Run(batchCtx, cmd[0], cmd[1:]...)

	if res.Err != nil || res.ExitCode != 0 {
		ex.outputf("  ⚡  batch install failed (%s), falling back to per-tool install\n", formatBatchError(res))
		ex.logWarn(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "failed", "error", res.Err, "exit_code", res.ExitCode)
		return false
	}

	ex.logDebug(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "succeeded")
	return true
}

// formatBatchError produces a short error string from a batch run result.
func formatBatchError(res run.Result) string {
	if res.Err != nil {
		return res.Err.Error()
	}
	return fmt.Sprintf("exit code %d", res.ExitCode)
}

func (ex *Executor) Execute(ctx context.Context, s *config.Schema, clan string) (*ExecReport, error) {
	start := time.Now()
	report := &ExecReport{}

	ex.clan = clan

	// Resolve native manager name from clan for method_order expansion.
	if mgr, ok := native.Lookup(clan); ok {
		ex.nativeManagerName = mgr.Name
	}
	if len(s.Defaults.MethodOrder) > 0 {
		ex.defaultMethodOrder = s.Defaults.MethodOrder
	}

	ex.logDebug(ctx, "executor", "phase", "init", "clan", clan, "tools", len(s.Tools))
	// Only sync native package index if at least one tool uses a native method.
	if needsNativeSync(s, clan) {
		syncMgr := NewSyncManager(ex.rn, clan)
		if syncMgr.NeedsSync() {
			ex.outputf("  syncing package index...\n")
			ex.logInfo(ctx, "sync", "status", "syncing")
			if err := syncMgr.Sync(ctx); err != nil {
				// A failed index sync leaves the native manager with stale
				// metadata — native installs may silently install wrong
				// versions or fail unpredictably. Fail loudly instead of
				// continuing.
				ex.logWarn(ctx, "sync", "status", "failed", "error", err)
				return nil, fmt.Errorf("package index sync failed: %w", err)
			}
			ex.logDebug(ctx, "sync", "status", "done")
		}
	}

	levels, err := graph.Sort(s.Tools, graph.WithLogger(ex.logger))
	if err != nil {
		return nil, fmt.Errorf("dependency resolution: %w", err)
	}

	ex.logDebug(ctx, "executor", "phase", "graph", "levels", len(levels))
	// Log dependency levels in debug mode (even without --dry-run).
	for i, level := range levels {
		ex.logDebug(ctx, "graph", "level", i, "tools", strings.Join(level, ", "))
	}
	if ex.dryRun {
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
		// PHASE 0: requires — a tool whose dependency failed must not attempt
		// to install (its runtime prerequisite is absent). It is marked failed
		// so the run exits non-zero and its own dependents are blocked
		// transitively.
		blockedByRequires := make(map[string]string) // toolName -> failure message
		for _, toolName := range level {
			tool, ok := s.Tools[toolName]
			if !ok {
				continue
			}
			for _, dep := range tool.Requires {
				if reason, bad := failedTools[dep]; bad {
					blockedByRequires[toolName] = fmt.Sprintf("requires failed dependency: %s (%s)", dep, reason)
					break
				}
			}
		}

		// PHASE 1: Dangerous-code filter (BEFORE any execution — including PreInstall).
		filteredLevel := make([]string, 0, len(level))
		for _, toolName := range level {
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
				hasDanger := ex.hasDangerousMethod(tool) || tool.PostInstall != "" || tool.PreInstall != ""
				if hasDanger {
					ex.outputf("  ⚠  %s: has hooks or build scripts that may execute arbitrary code. Use --allow-arbitrary-code to suppress this warning.\n", toolName)
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
			if !ok || tool.PreInstall == "" {
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
			} else {
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
			if ex.dryRun {
				names := make([]string, len(candidates))
				for i, c := range candidates {
					names[i] = c.toolName
				}
				ex.outputf("  ⚡  would batch native install: %s via %s\n", strings.Join(names, ", "), ex.nativeManagerName)
				for _, c := range candidates {
					ex.recordToolResult(ctx, &ToolResult{
						Tool: c.toolName, Status: StatusWouldInstall, Method: "native",
					}, report)
				}
			} else if ex.batchNativeInstall(ctx, candidates) {
				// The manager returning exit 0 does not guarantee every package
				// landed (some managers silently skip unknown package names).
				// Verify per tool: only tools whose Check passes are marked
				// installed; the rest fall back to the serial path, which
				// re-tries native and then any remaining methods.
				for _, c := range candidates {
					adapter := ex.LookupAdapter(c.method.Kind)
					if adapter != nil && adapter.Check(ctx, ex.rn, c.tool, c.method) {
						tr := ToolResult{
							Tool: c.toolName, Status: StatusInstalled, Method: "native", Config: c.method.Config,
						}
						if preinstallDone[c.toolName] {
							tr.PreinstallDone = true
						}
						if c.tool.PostInstall != "" {
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
			} else {
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
			if tr.Status == StatusFailed || tr.Status == StatusSkippedUnavailable {
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
	ex.logInfo(ctx, "executor", "phase", "done",
		"success", report.Success,
		"failed", report.Failed,
		"skipped", report.Skipped,
		"already", report.Already,
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

// Versioner is an optional interface adapters may implement to report the
// version of a tool at install time. The executor calls InstalledVersion
// while recording state; a non-nil error or an empty string means the version
// is unknown and ToolState.Version is left empty.
type Versioner interface {
	Adapter
	InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error)
}

// versionProbeTimeout bounds the per-tool version probe so a hanging
// `--version` call cannot stall an install run.
const versionProbeTimeout = 15 * time.Second

// installedVersion asks the adapter that installed the tool for the version
// it knows about. Best-effort: unknown versions and failures yield "".
func (ex *Executor) installedVersion(ctx context.Context, tool *config.Tool, tr ToolResult) string {
	adapter := ex.LookupAdapter(tr.MethodKind)
	if adapter == nil {
		return ""
	}
	ver, ok := adapter.(Versioner)
	if !ok {
		return ""
	}
	mc := &config.MethodCandidate{Kind: tr.MethodKind, Config: tr.Config}
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	version, err := ver.InstalledVersion(probeCtx, ex.rn, tool, mc)
	if err != nil {
		return ""
	}
	return version
}

// writeState persists the installation state file after a successful run.
// It loads existing state and merges in the current run's results, preserving
// tools installed by other schemas or earlier runs. Errors are returned so
// the caller can fail the run with a visible message instead of silently
// leaving the state file stale.
func (ex *Executor) writeState(ctx context.Context, s *config.Schema, report *ExecReport) error {
	if ex.schemaPath == "" {
		ex.logWarn(ctx, "state not persisted: no schema path configured (install may not be trackable)")
		return nil
	}

	// Load existing state under exclusive lock to prevent TOCTOU races.
	ls, err := state.LoadLocked()
	if err != nil {
		return fmt.Errorf("state lock failed: %w", err)
	}
	defer ls.Close()

	st := ls.State()
	st.SchemaPath = ex.schemaPath
	st.SchemaModifiedAt = ex.schemaModTime.UTC().Format(time.RFC3339)
	if st.Version == 0 {
		st.Version = 1
	}
	if st.Tools == nil {
		st.Tools = make(map[string]state.ToolState, len(report.Tools))
	}

	for _, tr := range report.Tools {
		if tr.Status != StatusInstalled && tr.Status != StatusAlready {
			continue
		}
		tool, ok := s.Tools[tr.Tool]
		if !ok {
			continue
		}
		existing, hadExisting := st.Tools[tr.Tool]
		ts := state.ToolState{
			Method:          tr.Method,
			MethodKind:      tr.MethodKind,
			InstalledAt:     time.Now().UTC().Format(time.RFC3339),
			PostinstallDone: tr.PostinstallDone,
			DefinitionHash:  state.DefinitionHash(tool),
			Config:          tr.Config,
		}
		// Record the installed version when the adapter can determine it.
		// When it cannot (e.g. a {latest} pin baked into the download URL),
		// keep a previously recorded version instead of clearing it.
		if ver := ex.installedVersion(ctx, tool, tr); ver != "" {
			ts.Version = ver
		} else if hadExisting {
			ts.Version = existing.Version
		}
		st.Tools[tr.Tool] = ts
	}

	if err := ls.Save(); err != nil {
		return fmt.Errorf("state save failed: %w", err)
	}
	return nil
}

// hasDangerousMethod checks whether any of the tool's methods have config
// keys that trigger arbitrary code execution (build scripts, etc.).
func (ex *Executor) hasDangerousMethod(tool *config.Tool) bool {
	for _, m := range tool.Methods {
		for _, key := range []string{"build", "build_cmd", "build_command"} {
			if v, ok := m.Config[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return true
				}
			}
		}
	}
	return false
}

func (ex *Executor) runPreinstall(ctx context.Context, tool *config.Tool) error {
	cmd := strings.TrimSpace(tool.PreInstall)
	if cmd == "" {
		return nil
	}
	ex.outputf("    pre-install: %s\n", cmd)
	ex.logDebug(ctx, "preinstall", "tool", tool.Name, "cmd", cmd)
	// Run through sh -c to support shell syntax (pipes, redirections, quotes).
	res := ex.rn.Run(ctx, "sh", "-c", cmd)
	if res.Err != nil {
		ex.outputf("    ⚠  pre-install: %s (aborting)\n", res.Err.Error())
		ex.logWarn(ctx, "preinstall", "tool", tool.Name, "error", res.Err.Error())
		return res.Err
	}
	if res.ExitCode != 0 {
		err := fmt.Errorf("pre-install exit %d", res.ExitCode)
		ex.outputf("    ⚠  pre-install: exit %d (aborting)\n", res.ExitCode)
		ex.logWarn(ctx, "preinstall", "tool", tool.Name, "exit_code", res.ExitCode)
		return err
	}
	return nil
}

func (ex *Executor) executeTool(ctx context.Context, tool *config.Tool) ToolResult {
	toolStart := time.Now()
	result := ToolResult{Tool: tool.Name}
	methods := tool.Methods
	if len(methods) == 0 {
		result.Status = StatusVirtual
		ex.logDebug(ctx, "tool", "tool", tool.Name, "status", "virtual")
		result.Duration = time.Since(toolStart).String()
		return result
	}

	// Security warnings for tools with arbitrary code execution surfaces.
	type dangerCheck struct {
		has    func(*config.Tool) bool
		detail string
	}
	checks := []dangerCheck{
		{ex.hasDangerousMethod, "config includes build scripts that may execute arbitrary code"},
		{func(t *config.Tool) bool { return t.PostInstall != "" }, "has a post-install hook (arbitrary code execution)"},
		{func(t *config.Tool) bool { return t.PreInstall != "" }, "has a pre-install hook (arbitrary code execution)"},
	}
	if !ex.allowArbitraryCode {
		hasDanger := false
		for _, c := range checks {
			if c.has(tool) {
				hasDanger = true
				ex.outputf("  ⚠  %s: %s. Use --allow-arbitrary-code to suppress this warning.\n", tool.Name, c.detail)
				ex.logWarn(ctx, "security", "tool", tool.Name, "warning", c.detail)
			}
		}
		if hasDanger {
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

// effectiveMethodOrder returns the method_order effective for a given
// tool, applying per-tool overrides and native manager expansion.
func (ex *Executor) effectiveMethodOrder(tool *config.Tool) []string {
	return config.EffectiveMethodOrder(tool, ex.defaultMethodOrder, ex.nativeManagerName)
}

// tryMethods iterates through all methods of a tool, trying each in order.
// It modifies result in place — on success the result is terminal; on
// exhaustion it sets the final status to StatusFailed or StatusSkippedUnavailable.
func (ex *Executor) tryMethods(toolCtx context.Context, tool *config.Tool, result *ToolResult, toolStart time.Time) {
	var lastMethodKind string
	orderedMethods := config.OrderMethods(tool.Methods, ex.effectiveMethodOrder(tool))
	for _, method := range orderedMethods {
		lastMethodKind = method.Kind
		displayKind := method.Kind
		if method.Label != "" {
			displayKind = method.Label
		}
		select {
		case <-toolCtx.Done():
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("tool timeout (%v) exceeded", ex.toolTimeout)
			ex.logWarn(toolCtx, "tool", "tool", tool.Name, "status", "timeout", "duration", ex.toolTimeout)
			result.Duration = time.Since(toolStart).String()
			return
		default:
		}

		attempt := MethodAttempt{Kind: displayKind}

		if method.When != nil && !method.When.Match(ex.facts) {
			attempt.Status = "skip_when"
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "skip_when", "requires", fmt.Sprintf("%v", method.When))
			continue
		}

		adapter := ex.LookupAdapter(method.Kind)
		if adapter == nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("no adapter for %q", displayKind)
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "skip_no_adapter")
			continue
		}

		if !adapter.Available(toolCtx, ex.rn) {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("adapter %q not available", displayKind)
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "skip_unavailable")
			continue
		}

		if adapter.Check(toolCtx, ex.rn, tool, method) {
			result.Status = StatusAlready
			result.Method = displayKind
			result.MethodKind = method.Kind
			result.Config = method.Config
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "already_installed")
			result.Duration = time.Since(toolStart).String()
			return
		}

		if ex.dryRun {
			result.Status = StatusWouldInstall
			result.Method = displayKind
			result.MethodKind = method.Kind
			attempt.Status = "success"
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "would_install")
			result.Duration = time.Since(toolStart).String()
			return
		}

		ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "installing")
		runner := ex.rn
		if lr, ok := runner.(*run.LoggingRunner); ok {
			runner = lr.WithContext(run.Context{Tool: tool.Name, Method: displayKind})
		}

		// method-timeout applies to each individual attempt.
		methodCtx, methodCancel := context.WithTimeout(toolCtx, ex.methodTimeout)
		err := adapter.Install(methodCtx, runner, tool, method)
		methodCancel()

		if err == nil {
			result.Status = StatusInstalled
			result.Method = displayKind
			result.MethodKind = method.Kind
			result.Config = method.Config
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "installed")
			if tool.PostInstall != "" {
				// Postinstall gets a fresh timeout from the tool-level context,
				// not the cancelled method context. A failing post-install
				// hook means the tool is not in the state the schema requires,
				// so the tool is marked failed (and the run exits non-zero)
				// instead of being silently reported as installed.
				postCtx, postCancel := context.WithTimeout(toolCtx, ex.methodTimeout)
				perr := ex.runPostinstall(postCtx, tool)
				postCancel()
				if perr != nil {
					result.Status = StatusFailed
					result.Error = fmt.Sprintf("post-install: %v", perr)
					result.Duration = time.Since(toolStart).String()
					return
				}
				result.PostinstallDone = true
			}
			result.Duration = time.Since(toolStart).String()
			return
		}

		attempt.Status = "failed"
		attempt.Error = err.Error()
		result.Methods = append(result.Methods, attempt)
		ex.logWarn(toolCtx, "tool", "tool", tool.Name, "method", displayKind, "status", "failed", "error", err.Error())
	}

	// All methods exhausted — determine terminal status.
	result.Status = StatusSkippedUnavailable
	for _, m := range result.Methods {
		if m.Status == "failed" {
			result.Status = StatusFailed
			break
		}
	}
	if len(result.Methods) > 0 {
		last := result.Methods[len(result.Methods)-1]
		result.Error = last.Error
		result.Method = last.Kind
		result.MethodKind = lastMethodKind
	}
	result.Duration = time.Since(toolStart).String()
}

// shouldUseColor reports whether status output should include ANSI color
// codes. It respects the NO_COLOR environment variable (standard convention)
// and checks that stderr is a character device (terminal). On non-Unix systems
// where os.ModeCharDevice is not set, it defaults to no color.
func shouldUseColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// colorizeStatusSymbol wraps the status symbol character in a line with
// the appropriate ANSI color code, followed by a reset. Returns the line
// unchanged when colors are not enabled.
func (ex *Executor) colorizeStatusSymbol(line string) string {
	if !ex.color {
		return line
	}
	type symColor struct {
		symbol string
		ansi   string
	}
	colors := []symColor{
		{"✓", "\033[32m"}, // green
		{"✗", "\033[31m"}, // red
		{"–", "\033[33m"}, // yellow
		{"→", "\033[36m"}, // cyan
		{"•", "\033[2m"},  // dim
	}
	for _, c := range colors {
		if strings.Contains(line, c.symbol) {
			return strings.Replace(line, c.symbol, c.ansi+c.symbol+"\033[0m", 1)
		}
	}
	return line
}

func formatToolResult(tool string, status StatusEnum, method, errMsg string) string {
	switch status {
	case StatusInstalled:
		return fmt.Sprintf("  ✓ %s: installed via %s\n", tool, method)
	case StatusAlready:
		return fmt.Sprintf("  ✓ %s: already installed (%s)\n", tool, method)
	case StatusSkippedWhen:
		return fmt.Sprintf("  – %s: skipped (when condition)\n", tool)
	case StatusSkippedUnavailable:
		if errMsg != "" {
			return fmt.Sprintf("  – %s: skipped (%s)\n", tool, errMsg)
		}
		return fmt.Sprintf("  – %s: skipped (no method available)\n", tool)
	case StatusWouldInstall:
		return fmt.Sprintf("  → %s: would install via %s (dry-run)\n", tool, method)
	case StatusVirtual:
		return fmt.Sprintf("  • %s: dependency group\n", tool)
	case StatusFailed:
		return fmt.Sprintf("  ✗ %s: failed (%s)\n", tool, errMsg)
	default:
		return fmt.Sprintf("  ? %s: %v\n", tool, status)
	}
}

// recordToolResult records a tool execution result into the report and emits
// user-facing status output and debug-level logs. It is safe for concurrent
// calls (report is protected by a mutex).
func (ex *Executor) recordToolResult(ctx context.Context, result *ToolResult, report *ExecReport) {
	report.mu.Lock()
	defer report.mu.Unlock()
	report.Tools = append(report.Tools, *result)
	switch result.Status {
	case StatusInstalled:
		report.Success++
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "installed", "duration", result.Duration)
	case StatusAlready:
		report.Already++
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "already", "duration", result.Duration)
	case StatusSkippedWhen:
		report.Skipped++
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "skipped_when")
	case StatusSkippedUnavailable:
		report.Skipped++
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "skipped_unavailable")
	case StatusFailed:
		report.Failed++
		ex.logWarn(ctx, "tool", "tool", result.Tool, "status", "failed", "error", result.Error, "duration", result.Duration)
	case StatusWouldInstall:
		// WouldInstall isn't counted in report totals, just logged.
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "would_install")
	case StatusVirtual:
		// Virtual isn't counted in report totals, just logged.
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "virtual")
	}
	if !ex.quiet {
		line := formatToolResult(result.Tool, result.Status, result.Method, result.Error)
		line = ex.colorizeStatusSymbol(line)
		ex.outputf("%s", line)
	}
}

// recordBlockedTool records a tool blocked by the security gate. The tool
// keeps the "skipped" status (no attempt was made), but the run must exit
// non-zero: a requested tool that was not installed is a failure, and the
// install command keys its exit code on report.Failed. The extra Failed
// increment is intentional — recordToolResult only bumps Skipped for this
// status, and callers of the report (install.go) treat Failed>0 as exit 1.
func (ex *Executor) recordBlockedTool(ctx context.Context, toolName string, report *ExecReport) {
	ex.recordToolResult(ctx, &ToolResult{
		Tool:   toolName,
		Status: StatusSkippedUnavailable,
		Error:  "requires --allow-arbitrary-code (tool has arbitrary code execution capability)",
	}, report)
	report.mu.Lock()
	report.Failed++
	report.mu.Unlock()
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

func (ex *Executor) runPostinstall(ctx context.Context, tool *config.Tool) error {
	cmd := strings.TrimSpace(tool.PostInstall)
	if cmd == "" {
		return nil
	}
	ex.outputf("    postinstall: %s\n", cmd)
	ex.logDebug(ctx, "postinstall", "tool", tool.Name, "cmd", cmd)
	// Run through sh -c to support shell syntax (pipes, redirections, quotes).
	res := ex.rn.Run(ctx, "sh", "-c", cmd)
	if res.Err != nil {
		ex.outputf("    ⚠  postinstall: %s (failed)\n", res.Err.Error())
		ex.logWarn(ctx, "postinstall", "tool", tool.Name, "error", res.Err.Error())
		return res.Err
	}
	if res.ExitCode != 0 {
		err := fmt.Errorf("postinstall exited %d", res.ExitCode)
		ex.outputf("    ⚠  postinstall: exit %d (failed)\n", res.ExitCode)
		ex.logWarn(ctx, "postinstall", "tool", tool.Name, "exit_code", res.ExitCode)
		return err
	}
	ex.logDebug(ctx, "postinstall", "tool", tool.Name, "status", "done")
	return nil
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
func (ex *Executor) logInfo(ctx context.Context, msg string, attrs ...any) {
	ex.log(ctx, slog.LevelInfo, msg, attrs...)
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
