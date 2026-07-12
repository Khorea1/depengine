package exec

import (
	"sync"
	"time"
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
)

// ToolResult is the outcome for ONE tool after execution.
type ToolResult struct {
	Tool     string
	Status   StatusEnum
	Method   string          // method that succeeded (or last one that failed)
	Error    string          // populated only if StatusFailed
	Methods  []MethodAttempt // history of attempts (for --verbose)
	Duration string          // human-readable duration (e.g. "3.2s"), set by executor

	// PostinstallDone is true if a postinstall script was successfully run.
	PostinstallDone bool

	// Config stores the method's configuration (e.g., pkg override).
	Config map[string]any
}

// MethodAttempt records one method attempt for a tool.
type MethodAttempt struct {
	Kind   string
	Status string // "skip_when" | "skip_unavailable" | "skip_already" | "success" | "failed"
	Error  string // populated only if failed
}

// ExecReport is the complete execution summary produced by the executor.
type ExecReport struct {
	mu sync.Mutex // protects concurrent access during parallel execution

	Tools    []ToolResult
	Success  int
	Failed   int
	Skipped  int
	Already  int
	Duration time.Duration
}
