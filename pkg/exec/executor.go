package exec

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"depengine/pkg/engine"
	"depengine/pkg/graph"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/state"
)

// Executor orchestrates the installation of all tools in a schema.
// Use New() to create, then configure with Option funcs.
type Executor struct {
	clan          string
	rn            run.Runner
	toolTimeout   time.Duration
	methodTimeout time.Duration
	dryRun        bool
	sortBy        SortField
	adapters      map[string]Adapter // per-instance adapter registry
	logger        *slog.Logger       // structured logger; nil = no structured output
	outWriter     io.Writer          // user-facing formatted output; defaults to os.Stderr

	// schema info for state tracking
	schemaPath       string
	schemaModTime    time.Time
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

func New() *Executor {
	return &Executor{
		rn:            run.OSExecRunner{},
		toolTimeout:   5 * time.Minute,
		methodTimeout: 2 * time.Minute,
		adapters:      make(map[string]Adapter),
		outWriter:     os.Stderr,
	}
}

// lookupAdapter returns the adapter for the given kind. It checks the
// per-instance registry first, falling back to the package-level global
// registry for adapters registered via init() (e.g. native adapter).
func (ex *Executor) lookupAdapter(kind string) Adapter {
	if ex.adapters != nil {
		if a, ok := ex.adapters[kind]; ok {
			return a
		}
	}
	return Lookup(kind)
}

// Execute runs all tools in the schema in dependency order.
func (ex *Executor) Execute(ctx context.Context, s *schema.Schema, clan string) (*ExecReport, error) {
	start := time.Now()
	report := &ExecReport{}
	ex.clan = clan

	ex.logDebug("executor", "phase", "init", "clan", clan, "tools", len(s.Tools))

	syncMgr := NewSyncManager(ex.rn, clan)
	if syncMgr.NeedsSync() {
		ex.outputf("  syncing package index...\n")
		ex.logInfo("sync", "status", "syncing")
		if err := syncMgr.Sync(ctx); err != nil {
			ex.outputf("  ⚠  sync warning: %v (continuing)\n", err)
			ex.logWarn("sync", "status", "warning", "error", err)
		} else {
			ex.logInfo("sync", "status", "done")
		}
	}

	levels, err := graph.Sort(s.Tools, graph.WithLogger(ex.logger))
	if err != nil {
		return nil, fmt.Errorf("dependency resolution: %w", err)
	}

	ex.logDebug("executor", "phase", "graph", "levels", len(levels))
	// Log dependency levels in debug mode (even without --dry-run).
	if ex.logger != nil && ex.logger.Enabled(ctx, slog.LevelDebug) {
		for i, level := range levels {
			ex.logDebug("graph", "level", i, "tools", strings.Join(level, ", "))
		}
	}

	if ex.dryRun {
		ex.outputf("  dependency order (%d levels):\n", len(levels))
		for i, level := range levels {
			ex.outputf("    level %d: %s\n", i, strings.Join(level, ", "))
		}
	}

	for _, level := range levels {
		for _, toolName := range level {
			tool, ok := s.Tools[toolName]
			if !ok {
				continue
			}
			result := ex.executeTool(ctx, tool)
			report.Tools = append(report.Tools, result)
			switch result.Status {
			case StatusInstalled:
				report.Success++
				ex.outputf("  ✓ %s: installed via %s\n", toolName, result.Method)
				ex.logDebug("tool", "tool", toolName, "method", result.Method, "status", "installed", "duration", result.Duration)
			case StatusAlready:
				report.Already++
				ex.outputf("  ✓ %s: already installed (%s)\n", toolName, result.Method)
				ex.logDebug("tool", "tool", toolName, "method", result.Method, "status", "already", "duration", result.Duration)
			case StatusSkippedWhen:
				report.Skipped++
				ex.outputf("  – %s: skipped (when condition)\n", toolName)
				ex.logDebug("tool", "tool", toolName, "status", "skipped_when")
			case StatusSkippedUnavailable:
				report.Skipped++
				ex.outputf("  – %s: skipped (no method available)\n", toolName)
				ex.logDebug("tool", "tool", toolName, "status", "skipped_unavailable")
			case StatusWouldInstall:
				ex.outputf("  → %s: would install via %s (dry-run)\n", toolName, result.Method)
				ex.logDebug("tool", "tool", toolName, "method", result.Method, "status", "would_install")
			case StatusFailed:
				report.Failed++
				ex.outputf("  ✗ %s: failed (%s)\n", toolName, result.Error)
				ex.logWarn("tool", "tool", toolName, "status", "failed", "error", result.Error, "duration", result.Duration)
			}
		}
	}

	if ex.sortBy != "" {
		report.SortBy(ex.sortBy)
	}

	report.Duration = time.Since(start)
	ex.logInfo("executor", "phase", "done",
		"success", report.Success,
		"failed", report.Failed,
		"skipped", report.Skipped,
		"already", report.Already,
		"duration", report.Duration.String())
	if ex.logger != nil && ex.logger.Enabled(ctx, slog.LevelDebug) {
		ex.logDebug("executor", "phase", "report", "json", report.JSON())
	}

	ex.writeState(s, report)

	return report, nil
}

// writeState persists the installation state file after a successful run.
func (ex *Executor) writeState(s *schema.Schema, report *ExecReport) {
	if ex.schemaPath == "" {
		return
	}
	st := &state.State{
		Version:          1,
		SchemaPath:       ex.schemaPath,
		SchemaModifiedAt: ex.schemaModTime.UTC().Format(time.RFC3339),
		Tools:            make(map[string]state.ToolState, len(report.Tools)),
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
			AdapterKind:     tr.Method,
			InstalledAt:     time.Now().UTC().Format(time.RFC3339),
			PostinstallDone: tr.PostinstallDone,
			DefinitionHash:  state.DefinitionHash(tool),
			Config:          tr.Config,
		}
	}

	lock, err := state.Lock()
	if err != nil {
		ex.logWarn("state lock failed", "error", err)
		return
	}
	defer lock.Close()

	if err := state.Save(st); err != nil {
		ex.logWarn("state save failed", "error", err)
	}
 }

func (ex *Executor) executeTool(ctx context.Context, tool *schema.Tool) ToolResult {
	toolStart := time.Now()
	result := ToolResult{Tool: tool.Name}
	methods := tool.Methods
	if len(methods) == 0 {
		result.Status = StatusSkippedUnavailable
		result.Error = "no methods declared"
		ex.logDebug("tool", "tool", tool.Name, "status", "no_methods")
		result.Duration = time.Since(toolStart).String()
		return result
	}

	for _, method := range methods {
		attempt := MethodAttempt{Kind: method.Kind}

		if method.When != nil && len(method.When.DistroFamily) > 0 {
			if !engine.MatchesDistroFamily(ex.clan, method.When.DistroFamily) {
				attempt.Status = "skip_when"
				result.Methods = append(result.Methods, attempt)
				ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "skip_when", "requires", fmt.Sprintf("%v", method.When.DistroFamily))
				continue
			}
		}

		adapter := ex.lookupAdapter(method.Kind)
		if adapter == nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("no adapter for %q", method.Kind)
			result.Methods = append(result.Methods, attempt)
			ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "skip_no_adapter")
			continue
		}

		if !adapter.Available(ctx, ex.rn) {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("adapter %q not available", method.Kind)
			result.Methods = append(result.Methods, attempt)
			ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "skip_unavailable")
			continue
		}

		if adapter.Check(ctx, ex.rn, tool, method) {
			result.Status = StatusAlready
		result.Method = method.Kind
		result.Config = method.Config
			ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "already_installed")
			result.Duration = time.Since(toolStart).String()
			return result
		}

		if ex.dryRun {
			result.Status = StatusWouldInstall
			result.Method = method.Kind
			attempt.Status = "success"
			result.Methods = append(result.Methods, attempt)
			ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "would_install")
			result.Duration = time.Since(toolStart).String()
			return result
		}

		ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "installing")
		runner := ex.rn
		if lr, ok := runner.(*run.LoggingRunner); ok {
			runner = lr.WithContext(run.Context{Tool: tool.Name, Method: method.Kind})
		}
		methodCtx, cancel := context.WithTimeout(ctx, ex.methodTimeout)
		err := adapter.Install(methodCtx, runner, tool, method)
		cancel()

		if err == nil {
			result.Status = StatusInstalled
			result.Method = method.Kind
			result.Config = method.Config
			ex.logDebug("tool", "tool", tool.Name, "method", method.Kind, "status", "installed")
			if tool.PostInstall != "" {
				ex.runPostinstall(methodCtx, tool)
				result.PostinstallDone = true
			}
			result.Duration = time.Since(toolStart).String()
			return result
		}

		attempt.Status = "failed"
		attempt.Error = err.Error()
		result.Methods = append(result.Methods, attempt)
		ex.logWarn("tool", "tool", tool.Name, "method", method.Kind, "status", "failed", "error", err.Error())
	}

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
	return result
}

func (ex *Executor) runPostinstall(ctx context.Context, tool *schema.Tool) {
	ex.outputf("    postinstall: %s\n", tool.PostInstall)
	ex.logDebug("postinstall", "tool", tool.Name, "cmd", tool.PostInstall)
	parts := strings.Fields(tool.PostInstall)
	if len(parts) == 0 {
		return
	}
	res := ex.rn.Run(ctx, parts[0], parts[1:]...)
	if res.Err != nil {
		ex.outputf("    ⚠  postinstall: %s (continuing)\n", res.Err.Error())
		ex.logWarn("postinstall", "tool", tool.Name, "error", res.Err.Error())
	} else if res.ExitCode != 0 {
		ex.outputf("    ⚠  postinstall: exit %d (continuing)\n", res.ExitCode)
		ex.logWarn("postinstall", "tool", tool.Name, "exit_code", res.ExitCode)
	} else {
		ex.logDebug("postinstall", "tool", tool.Name, "status", "done")
	}
}

// log emits a structured log entry at the given level, if a logger is set.
func (ex *Executor) log(level slog.Level, msg string, attrs ...any) {
	if ex.logger != nil {
		ex.logger.Log(context.Background(), level, msg, attrs...)
	}
}

func (ex *Executor) logDebug(msg string, attrs ...any) { ex.log(slog.LevelDebug, msg, attrs...) }
func (ex *Executor) logInfo(msg string, attrs ...any)  { ex.log(slog.LevelInfo, msg, attrs...) }
func (ex *Executor) logWarn(msg string, attrs ...any)  { ex.log(slog.LevelWarn, msg, attrs...) }

// outputf formats user-facing output (status lines, sync messages, etc.).
func (ex *Executor) outputf(format string, args ...any) {
	if ex.outWriter != nil {
		fmt.Fprintf(ex.outWriter, format, args...)
	}
}
