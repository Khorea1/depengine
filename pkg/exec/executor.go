package exec

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"depengine/pkg/engine"
	"depengine/pkg/graph"
	"depengine/pkg/native"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/state"
)

// Executor orchestrates the installation of all tools in a schema.
// Use New() to create, then configure with Option funcs.
type Executor struct {
	clan               string
	rn                 run.Runner
	toolTimeout        time.Duration
	methodTimeout      time.Duration
	dryRun             bool
	quiet              bool // suppress per-tool status line output (--quiet)
	sortBy             SortField
	adapters           map[string]Adapter // per-instance adapter registry
	logger             *slog.Logger       // structured logger; nil = no structured output
	outWriter          io.Writer          // user-facing formatted output; defaults to os.Stderr
	maxJobs            int                // max concurrent tools; 0 or 1 = sequential (default)
	allowArbitraryCode bool               // if false, warn about dangerous methods (build scripts, etc.)

	defaultMethodOrder []string // from schema.Defaults.MethodOrder; default = schema.DefaultMethodOrder
	nativeManagerName  string   // resolved from clan via native.Lookup

	// system facts for when-condition evaluation
	facts *engine.Facts

	// schema info for state tracking
	schemaPath    string
	schemaModTime time.Time

	color bool // whether to emit ANSI color codes in status output
}

// Option configures the executor.
type Option func(*Executor)

func WithRunner(rn run.Runner) Option {
	return func(e *Executor) {
		if rn != nil {
			e.rn = rn
		}
	}
}

func WithToolTimeout(d time.Duration) Option {
	return func(e *Executor) {
		if d > 0 {
			e.toolTimeout = d
		}
	}
}

func WithMethodTimeout(d time.Duration) Option {
	return func(e *Executor) {
		if d > 0 {
			e.methodTimeout = d
		}
	}
}

func WithDryRun() Option {
	return func(e *Executor) {
		e.dryRun = true
	}
}

// WithSortBy sets the sort criterion for the output report.
// The empty string (default) means no sorting — tools keep dependency order.
func WithSortBy(field SortField) Option {
	return func(e *Executor) {
		e.sortBy = field
	}
}

// WithMaxJobs sets the maximum number of concurrent tool installations
// within a topological level. Values of 0 or 1 mean sequential (default).
func WithMaxJobs(n int) Option {
	return func(e *Executor) {
		if n > 1 {
			e.maxJobs = n
		}
	}
}

// WithAllowArbitraryCode suppresses security warnings about dangerous methods
// (build scripts, arbitrary shell execution, etc.).
func WithAllowArbitraryCode() Option {
	return func(e *Executor) {
		e.allowArbitraryCode = true
	}
}

// WithQuiet suppresses per-tool status line output, showing only the
// final summary. Restores the old summary-only behavior.
func WithQuiet() Option {
	return func(e *Executor) {
		e.quiet = true
	}
}

// WithLogger sets the structured logger for the executor. When set, the
// executor emits structured DEBUG/INFO logs at each decision point in
// addition to the user-facing output via outWriter (default os.Stderr).
func WithLogger(l *slog.Logger) Option {
	return func(e *Executor) {
		e.logger = l
	}
}

// WithOutput sets the writer for user-facing formatted output (the
// ✓/✗/–/→ status lines, sync messages, dependency-level listing,
// postinstall progress). Defaults to os.Stderr.
func WithOutput(w io.Writer) Option {
	return func(e *Executor) {
		if w != nil {
			e.outWriter = w
		}
	}
}

// WithAdapters registers adapters into the executor's per-instance
// registry. Each adapter is stored by its Kind(). Duplicate kinds
// are silently overwritten — the last one wins (explicit construction
// overrides global registrations from init()).
func WithAdapters(adapters ...Adapter) Option {
	return func(e *Executor) {
		if e.adapters == nil {
			e.adapters = make(map[string]Adapter, len(adapters))
		}
		for _, a := range adapters {
			if a != nil {
				e.adapters[a.Kind()] = a
			}
		}
	}
}

// WithSchemaInfo sets the schema file path and modification time for state
// tracking. When set, the executor will write a state file after successful
// installation.
func WithSchemaInfo(path string, modTime time.Time) Option {
	return func(e *Executor) {
		e.schemaPath = path
		e.schemaModTime = modTime
	}
}

func WithDefaultMethodOrder(order []string) Option {
	return func(ex *Executor) {
		if len(order) > 0 {
			ex.defaultMethodOrder = order
		}
	}
}
func WithFacts(f *engine.Facts) Option {
	return func(ex *Executor) { ex.facts = f }
}
func New() *Executor {
	ex := &Executor{
		rn:                 run.OSExecRunner{},
		toolTimeout:        5 * time.Minute,
		methodTimeout:      2 * time.Minute,
		maxJobs:            1,
		adapters:           make(map[string]Adapter, len(adapters)),
		outWriter:          os.Stderr,
		defaultMethodOrder: schema.DefaultMethodOrder,
		color:              shouldUseColor(),
	}
	// Pre-populate from the global adapter registry.
	// Adapters registered via init() (git, http, native, lang, etc.)
	// are available to every executor. WithAdapters can override them.
	adaptersMu.RLock()
	for k, a := range adapters {
		ex.adapters[k] = a
	}
	adaptersMu.RUnlock()
	return ex
}

// LookupAdapter returns the adapter for the given kind from the executor's
// per-instance registry. Returns nil if no adapter is registered for that kind.
func (ex *Executor) LookupAdapter(kind string) Adapter {
	return ex.adapters[kind]
}

// Execute runs all tools in the schema in dependency order.
// needsNativeSync reports whether the schema contains any tool with a native
// method for the given clan. If no tool uses native, index sync is skipped.
func needsNativeSync(s *schema.Schema, clan string) bool {
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

func (ex *Executor) Execute(ctx context.Context, s *schema.Schema, clan string) (*ExecReport, error) {
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
				ex.outputf("  ⚠  sync warning: %v (continuing)\n", err)
				ex.logWarn(ctx, "sync", "status", "warning", "error", err)
			} else {
				ex.logDebug(ctx, "sync", "status", "done")
			}
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

	for _, level := range levels {
		if ex.maxJobs <= 1 {
			// Sequential: exact same behavior as before.
			for _, toolName := range level {
				tool, ok := s.Tools[toolName]
				if !ok {
					continue
				}
				result := ex.executeTool(ctx, tool)
				ex.recordToolResult(ctx, &result, report)
			}
		} else {
			ex.executeLevelParallel(ctx, s, level, report)
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
		ex.writeState(ctx, s, report)
	}

	return report, nil
}

// writeState persists the installation state file after a successful run.
// It loads existing state and merges in the current run's results, preserving
// tools installed by other schemas or earlier runs.
func (ex *Executor) writeState(ctx context.Context, s *schema.Schema, report *ExecReport) {
	if ex.schemaPath == "" {
		ex.logWarn(ctx, "state not persisted: no schema path configured (install may not be trackable)")
		return
	}

	// Load existing state under exclusive lock to prevent TOCTOU races.
	ls, err := state.LoadLocked()
	if err != nil {
		ex.logWarn(ctx, "state lock failed", "error", err)
		return
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
		st.Tools[tr.Tool] = state.ToolState{
			Method:          tr.Method,
			InstalledAt:     time.Now().UTC().Format(time.RFC3339),
			PostinstallDone: tr.PostinstallDone,
			DefinitionHash:  state.DefinitionHash(tool),
			Config:          tr.Config,
		}
	}

	if err := ls.Save(); err != nil {
		ex.logWarn(ctx, "state save failed", "error", err)
	}
}

// hasDangerousMethod checks whether any of the tool's methods have config
// keys that trigger arbitrary code execution (build scripts, etc.).
func (ex *Executor) hasDangerousMethod(tool *schema.Tool) bool {
	for _, m := range tool.Methods {
		// Build script config key is user-supplied and may execute arbitrary code.
		if v, ok := m.Config["build"]; ok {
			if s, ok := v.(string); ok && s != "" {
				return true
			}
		}
	}
	return false
}

func (ex *Executor) runPreinstall(ctx context.Context, tool *schema.Tool) error {
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
func (ex *Executor) executeTool(ctx context.Context, tool *schema.Tool) ToolResult {
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
		has    func(*schema.Tool) bool
		detail string
	}
	checks := []dangerCheck{
		{ex.hasDangerousMethod, "config includes build scripts that may execute arbitrary code"},
		{func(t *schema.Tool) bool { return t.PostInstall != "" }, "has a post-install hook (arbitrary code execution)"},
		{func(t *schema.Tool) bool { return t.PreInstall != "" }, "has a pre-install hook (arbitrary code execution)"},
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
			result.Status = StatusSkippedUnavailable
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

	// Pre-install hook: runs before any install attempt. Failure aborts the tool.
	if tool.PreInstall != "" {
		preCtx, preCancel := context.WithTimeout(toolCtx, ex.methodTimeout)
		if err := ex.runPreinstall(preCtx, tool); err != nil {
			preCancel()
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("pre-install: %v", err)
			result.Duration = time.Since(toolStart).String()
			ex.logWarn(ctx, "preinstall", "tool", tool.Name, "error", err.Error())
			return result
		}
		preCancel()
		result.PreinstallDone = true
	}

	ex.tryMethods(toolCtx, tool, &result, toolStart)
	return result
}

// effectiveMethodOrder returns the method_order effective for a given
// tool, applying per-tool overrides and native manager expansion.
func (ex *Executor) effectiveMethodOrder(tool *schema.Tool) []string {
	return schema.EffectiveMethodOrder(tool, ex.defaultMethodOrder, ex.nativeManagerName)
}

// tryMethods iterates through all methods of a tool, trying each in order.
// It modifies result in place — on success the result is terminal; on
func (ex *Executor) tryMethods(toolCtx context.Context, tool *schema.Tool, result *ToolResult, toolStart time.Time) {
	orderedMethods := schema.OrderMethods(tool.Methods, ex.effectiveMethodOrder(tool))
	for _, method := range orderedMethods {
		select {
		case <-toolCtx.Done():
			result.Status = StatusFailed
			result.Error = fmt.Sprintf("tool timeout (%v) exceeded", ex.toolTimeout)
			ex.logWarn(toolCtx, "tool", "tool", tool.Name, "status", "timeout", "duration", ex.toolTimeout)
			result.Duration = time.Since(toolStart).String()
			return
		default:
		}

		attempt := MethodAttempt{Kind: method.Kind}

		if method.When != nil && !method.When.Match(ex.facts) {
			attempt.Status = "skip_when"
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "skip_when", "requires", fmt.Sprintf("%v", method.When))
			continue
		}

		adapter := ex.LookupAdapter(method.Kind)
		if adapter == nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("no adapter for %q", method.Kind)
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "skip_no_adapter")
			continue
		}

		if !adapter.Available(toolCtx, ex.rn) {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("adapter %q not available", method.Kind)
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "skip_unavailable")
			continue
		}

		if adapter.Check(toolCtx, ex.rn, tool, method) {
			result.Status = StatusAlready
			result.Method = method.Kind
			result.Config = method.Config
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "already_installed")
			result.Duration = time.Since(toolStart).String()
			return
		}

		if ex.dryRun {
			result.Status = StatusWouldInstall
			result.Method = method.Kind
			attempt.Status = "success"
			result.Methods = append(result.Methods, attempt)
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "would_install")
			result.Duration = time.Since(toolStart).String()
			return
		}

		ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "installing")
		runner := ex.rn
		if lr, ok := runner.(*run.LoggingRunner); ok {
			runner = lr.WithContext(run.Context{Tool: tool.Name, Method: method.Kind})
		}

		// method-timeout applies to each individual attempt.
		methodCtx, methodCancel := context.WithTimeout(toolCtx, ex.methodTimeout)
		err := adapter.Install(methodCtx, runner, tool, method)
		methodCancel()

		if err == nil {
			result.Status = StatusInstalled
			result.Method = method.Kind
			result.Config = method.Config
			ex.logDebug(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "installed")
			if tool.PostInstall != "" {
				// Postinstall gets a fresh timeout from the tool-level context,
				// not the cancelled method context.
				postCtx, postCancel := context.WithTimeout(toolCtx, ex.methodTimeout)
				if err := ex.runPostinstall(postCtx, tool); err == nil {
					result.PostinstallDone = true
				}
				postCancel()
			}
			result.Duration = time.Since(toolStart).String()
			return
		}

		attempt.Status = "failed"
		attempt.Error = err.Error()
		result.Methods = append(result.Methods, attempt)
		ex.logWarn(toolCtx, "tool", "tool", tool.Name, "method", method.Kind, "status", "failed", "error", err.Error())
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
	}
	result.Duration = time.Since(toolStart).String()
}

// formatToolResult generates a user-facing status line for a tool execution
// result. It is a package-level function (no Executor dependency) so it can be
// tested independently of the full executor pipeline.

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
		{"✓", "\033[32m"},  // green
		{"✗", "\033[31m"},  // red
		{"–", "\033[33m"},  // yellow
		{"→", "\033[36m"},  // cyan
		{"•", "\033[2m"},   // dim
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

// executeLevelParallel runs all tools in a topological level concurrently,
// limiting concurrency to ex.maxJobs. Results are collected thread-safely
// via recordToolResult.
func (ex *Executor) executeLevelParallel(ctx context.Context, s *schema.Schema, level []string, report *ExecReport) {
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

// ExplainTool evaluates all methods for a single tool WITHOUT installing.
// For each method it reports the status and reason: skip_when (when condition
// didn't match), skip_unavailable (no adapter or binary not on PATH),
// already_installed (Check passed), or would_install (ready to install).

// This is the engine behind `depengine why <tool>`.
func (ex *Executor) ExplainTool(ctx context.Context, tool *schema.Tool, clan string) []MethodAttempt {
	// Resolve native manager for method_order expansion.
	if mgr, ok := native.Lookup(clan); ok {
		ex.nativeManagerName = mgr.Name
	}
	orderedMethods := schema.OrderMethods(tool.Methods, ex.effectiveMethodOrder(tool))
	methods := orderedMethods
	if len(methods) == 0 {
		return []MethodAttempt{{Kind: "", Status: "virtual", Error: "dependency group (no methods declared)"}}
	}
	attempts := make([]MethodAttempt, 0, len(methods))

	for _, method := range methods {
		attempt := MethodAttempt{Kind: method.Kind}

		// Check when condition.
		if method.When != nil && !method.When.Match(ex.facts) {
			attempt.Status = "skip_when"
			attempt.Error = fmt.Sprintf("when condition not met: %+v", method.When)
			attempts = append(attempts, attempt)
			continue
		}

		// Look up adapter for this method kind.
		adapter := ex.LookupAdapter(method.Kind)
		if adapter == nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("no adapter registered for kind %q", method.Kind)
			attempts = append(attempts, attempt)
			continue
		}

		// Check if the adapter is available on this system.
		if !adapter.Available(ctx, ex.rn) {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("adapter %q not available (binary not on PATH)", method.Kind)
			attempts = append(attempts, attempt)
			continue
		}

		// Check if the tool is already installed via this method.
		if adapter.Check(ctx, ex.rn, tool, method) {
			attempt.Status = "already_installed"
			attempt.Error = "check passed — tool appears to be installed"
			attempts = append(attempts, attempt)
			continue
		}

		// Method is ready and would be attempted.
		attempt.Status = "would_install"
		attempts = append(attempts, attempt)
	}

	return attempts
}
func (ex *Executor) runPostinstall(ctx context.Context, tool *schema.Tool) error {
	cmd := strings.TrimSpace(tool.PostInstall)
	if cmd == "" {
		return nil
	}
	ex.outputf("    postinstall: %s\n", cmd)
	ex.logDebug(ctx, "postinstall", "tool", tool.Name, "cmd", cmd)
	// Run through sh -c to support shell syntax (pipes, redirections, quotes).
	res := ex.rn.Run(ctx, "sh", "-c", cmd)
	if res.Err != nil {
		ex.outputf("    ⚠  postinstall: %s (continuing)\n", res.Err.Error())
		ex.logWarn(ctx, "postinstall", "tool", tool.Name, "error", res.Err.Error())
		return res.Err
	}
	if res.ExitCode != 0 {
		err := fmt.Errorf("postinstall exited %d", res.ExitCode)
		ex.outputf("    ⚠  postinstall: exit %d (continuing)\n", res.ExitCode)
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
