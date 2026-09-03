package exec

import (
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/run"
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
	diagnose           bool // show internal decision-making detail (graph levels, etc.) (--diagnose)
	sortBy             SortField
	adapters           map[string]Adapter // per-instance adapter registry
	logger             *slog.Logger       // structured logger; nil = no structured output
	outWriter          io.Writer          // user-facing formatted output; defaults to os.Stderr
	maxJobs            int                // max concurrent tools; 0 or 1 = sequential (default)
	allowArbitraryCode bool               // if false, warn about dangerous methods (build scripts, etc.)

	batchTimeout time.Duration // per-batch timeout, scaled by package count

	defaultMethodOrder []string // from config.Defaults.MethodOrder; default = config.DefaultMethodOrder
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

// WithBatchTimeout sets the timeout for a batch native install command.
// If not set, defaults to methodTimeout * candidate count.
func WithBatchTimeout(d time.Duration) Option {
	return func(ex *Executor) {
		if d > 0 {
			ex.batchTimeout = d
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

// WithDiagnose enables internal decision-making detail in the output (e.g.
// the dependency-graph level listing during --dry-run). This is extra detail
// aimed at debugging depengine itself, not the "what will happen to my
// system" question a normal run answers — so it's opt-in via --diagnose
// rather than shown by default.
func WithDiagnose() Option {
	return func(e *Executor) {
		e.diagnose = true
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
		defaultMethodOrder: config.DefaultMethodOrder,
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

// probeRunner returns ex.rn tagged as a probe for the given tool/method, so
// that Available()/Check() calls — which routinely "fail" (exit non-zero)
// simply to report "not installed yet" / "not on PATH" — log at DEBUG
// instead of WARN. Real install attempts go through ex.rn untagged (or
// tagged Probe: false) and keep full WARN visibility on failure. Falls back
// to ex.rn unchanged if it isn't a *run.LoggingRunner (e.g. tests using
// run.FakeRunner directly).
func (ex *Executor) probeRunner(tool, method string) run.Runner {
	if lr, ok := ex.rn.(*run.LoggingRunner); ok {
		return lr.WithContext(run.Context{Tool: tool, Method: method, Probe: true})
	}
	return ex.rn
}

// DefaultMethodOrder returns the effective default method order for the
// executor. Used by callers that need to resolve method ordering outside
// the normal Execute path (e.g. upgrade).
func (ex *Executor) DefaultMethodOrder() []string {
	return ex.defaultMethodOrder
}

// NativeManagerName returns the resolved native package manager name
// (e.g. "apt", "pacman"). Empty when no clan is set.
func (ex *Executor) NativeManagerName() string {
	return ex.nativeManagerName
}
