package exec

import (
	"sync"
	"time"

	"github.com/Khorea1/depengine/pkg/plan"
)

// SortField controls the sort order of tools in the output report.
type SortField string

const (
	SortByName   SortField = "name"
	SortByStatus SortField = "status"
	SortByMethod SortField = "method"
)

// ParseSortField converts a string to a SortField, returning an empty
// SortField and false if the value is not recognised.
func ParseSortField(s string) (SortField, bool) {
	switch SortField(s) {
	case SortByName, SortByStatus, SortByMethod:
		return SortField(s), true
	default:
		return SortField(""), false
	}
}

// StatusEnum represents the result of processing one tool.
type StatusEnum int

const (
	StatusInstalled          StatusEnum = iota // installed successfully this run
	StatusAlready                              // was already installed (Check passed)
	StatusSkippedWhen                          // skipped because when condition didn't match
	StatusSkippedUnavailable                   // skipped because no adapter was available
	StatusFailed                               // all methods failed
	StatusWouldInstall                         // dry-run: would install
	StatusVirtual                              // tool that serves as a dependency group only (no methods)
)

// ToolResult is the outcome for ONE tool after execution.
type ToolResult struct {
	Tool       string
	Status     StatusEnum
	Method     string          // schema/display method that succeeded (or last one that failed)
	Provider   string          // concrete runtime provider when it differs (e.g. apt/xbps for native)
	MethodKind string          // technical kind (e.g. "http"), not display label
	Error      string          // populated only if StatusFailed
	Methods    []MethodAttempt // history of attempts (for --verbose)
	Duration   string          // human-readable duration (e.g. "3.2s"), set by executor
	// PreinstallDone is true if a pre-install script was successfully run.
	PreinstallDone bool

	// PostinstallDone is true if a postinstall script was successfully run.
	PostinstallDone bool
	RebootRequired  bool

	// InstallCommitted records that the adapter install mutation completed even
	// if a later post-install hook failed. State persistence must still track the
	// host installation and its owned resources so removal/recovery stay sound.
	InstallCommitted bool

	// Config stores the method's configuration (e.g., pkg override).
	Config     map[string]any
	PlanIntent *plan.ResolvedInstallPlan

	// ResourceUses records shared host resources used by a successfully
	// committed candidate and whether depengine created them during this run.
	// It is projected into durable ownership/refcount state by writeState and
	// intentionally omitted from user-facing report JSON.
	ResourceUses []plan.ResourceUse
}

// MethodAttempt records one method attempt for a tool.
type MethodAttempt struct {
	Kind       string
	Status     string                    // "skip_when" | "skip_unavailable" | "skip_policy" | "skip_capability" | "skip_already" | "success" | "failed" | "virtual"
	Error      string                    // explanation/reason for explain and failures
	Intent     map[string]string         // normalized, non-secret identity fields for explain/why
	PlanIntent *plan.ResolvedInstallPlan // static adapter-neutral projection before host resolution
}

// ExecReport is the complete execution summary produced by the executor.
type ExecReport struct {
	mu sync.Mutex // protects concurrent access during parallel execution

	Tools   []ToolResult
	Success int
	Failed  int
	Skipped int
	Already int
	// WouldInstall counts StatusWouldInstall tools (--dry-run only). Kept
	// separate from Success so a real install's "N installed" can never be
	// confused with a dry-run's "N would install".
	WouldInstall int
	Duration     time.Duration
}
