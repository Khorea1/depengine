package exec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SortBy sorts the Tools slice in-place by the given criterion.
func (r *ExecReport) SortBy(field SortField) {
	switch field {
	case SortByName:
		sort.SliceStable(r.Tools, func(i, j int) bool {
			return strings.ToLower(r.Tools[i].Tool) < strings.ToLower(r.Tools[j].Tool)
		})
	case SortByStatus:
		sort.SliceStable(r.Tools, func(i, j int) bool {
			return statusPriority(r.Tools[i].Status) < statusPriority(r.Tools[j].Status)
		})
	case SortByMethod:
		sort.SliceStable(r.Tools, func(i, j int) bool {
			mi, mj := r.Tools[i].Method, r.Tools[j].Method
			if mi == "" {
				return false
			}
			if mj == "" {
				return true
			}
			return strings.ToLower(mi) < strings.ToLower(mj)
		})
	}
}

// statusPriority maps a StatusEnum to a sort key so the most relevant
// statuses (installed, failed) appear first.
func statusPriority(s StatusEnum) int {
	switch s {
	case StatusWouldInstall:
		return 0
	case StatusInstalled:
		return 1
	case StatusAlready:
		return 2
	case StatusFailed:
		return 3
	case StatusSkippedWhen:
		return 4
	case StatusSkippedUnavailable:
		return 5
	case StatusVirtual:
		return 99
	default:
		return 6
	}
}

// Summary returns a one-line summary string.
// Example: "5 installed, 1 failed, 2 skipped, 3 already"
func (r *ExecReport) Summary() string {
	parts := []string{}
	if r.Success > 0 {
		parts = append(parts, fmt.Sprintf("%d installed", r.Success))
	}
	if r.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", r.Failed))
	}
	if r.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", r.Skipped))
	}
	if r.Already > 0 {
		parts = append(parts, fmt.Sprintf("%d already", r.Already))
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return fmt.Sprintf("%s (%v)", strings.Join(parts, ", "), r.Duration.Truncate(time.Millisecond))
}

// statusLabel returns a human-readable label for a StatusEnum.
func statusLabel(s StatusEnum) string {
	switch s {
	case StatusInstalled:
		return "installed"
	case StatusAlready:
		return "already"
	case StatusSkippedWhen:
		return "skipped"
	case StatusSkippedUnavailable:
		return "unavailable"
	case StatusFailed:
		return "failed"
	case StatusWouldInstall:
		return "would install"
	case StatusVirtual:
		return "virtual"
	default:
		return "unknown"
	}
}

const (
	detailFormat = "%-24s %-15s %-15s"
	detailSep    = "────────────────────────────────────────────────────────────"
)

// Detail returns a formatted table with per-tool results.
func (r *ExecReport) Detail() string {
	if len(r.Tools) == 0 {
		return "no tools processed"
	}

	var b strings.Builder
	header := fmt.Sprintf(detailFormat, "Tool", "Status", "Method")
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(detailSep)
	b.WriteByte('\n')

	for _, tr := range r.Tools {
		method := tr.Method
		if method == "" {
			method = "—"
		}

		b.WriteString(fmt.Sprintf(detailFormat+"\n",
			truncate(tr.Tool, 23),
			statusLabel(tr.Status),
			method,
		))
	}

	b.WriteString(detailSep)
	b.WriteByte('\n')
	b.WriteString(r.Summary())
	b.WriteByte('\n')
	return b.String()
}

// JSON returns the report as a JSON string.
func (r *ExecReport) JSON() string {
	type jsonTool struct {
		Tool   string `json:"tool"`
		Status string `json:"status"`
		Method string `json:"method,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	out := struct {
		Tools   []jsonTool `json:"tools"`
		Summary string     `json:"summary"`
		Success int        `json:"success"`
		Failed  int        `json:"failed"`
		Skipped int        `json:"skipped"`
		Already int        `json:"already"`
		Seconds float64    `json:"duration_seconds"`
	}{
		Summary: r.Summary(),
		Success: r.Success,
		Failed:  r.Failed,
		Skipped: r.Skipped,
		Already: r.Already,
		Seconds: r.Duration.Seconds(),
	}
	for _, tr := range r.Tools {
		out.Tools = append(out.Tools, jsonTool{
			Tool:   tr.Tool,
			Status: statusLabel(tr.Status),
			Method: tr.Method,
			Error:  tr.Error,
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
