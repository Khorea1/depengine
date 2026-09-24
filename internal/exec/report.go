package exec

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/term"
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
	if r.WouldInstall > 0 {
		parts = append(parts, fmt.Sprintf("%d would install", r.WouldInstall))
	}
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

// statusColor returns the ANSI color code for a StatusEnum, matching the
// palette colorizeStatusSymbol uses for the live ✓/✗/–/→ lines so the table
// and the streamed output speak the same visual language.
func statusColor(s StatusEnum) string {
	switch s {
	case StatusInstalled, StatusAlready:
		return "\033[32m" // green
	case StatusFailed:
		return "\033[31m" // red
	case StatusSkippedWhen, StatusSkippedUnavailable:
		return "\033[33m" // yellow
	case StatusWouldInstall:
		return "\033[36m" // cyan
	case StatusVirtual:
		return "\033[2m" // dim
	default:
		return ""
	}
}

// Detail returns a formatted table with per-tool results. Failed tools get
// their error message on an indented line directly under the row — the
// table is the only output shown in --quiet mode, so it needs to carry
// enough to diagnose a failure without re-running with a different flag.
func (r *ExecReport) Detail() string {
	if len(r.Tools) == 0 {
		return "no tools processed\n"
	}

	color := shouldUseColor()
	availWidth := term.Width(os.Stderr)
	toolWidth := 24
	statusWidth := 15
	methodWidth := 15
	if availWidth > 0 {
		// Reserve 4 chars for inter-column spacing
		remaining := availWidth - 4
		toolWidth = remaining * 55 / 100
		statusWidth = remaining * 20 / 100
		methodWidth = remaining - toolWidth - statusWidth
		if toolWidth < 15 {
			toolWidth = 15
		}
		if statusWidth < 10 {
			statusWidth = 10
		}
		if methodWidth < 10 {
			methodWidth = 10
		}
	}

	format := fmt.Sprintf("%%-%ds %%-%ds %%-%ds", toolWidth, statusWidth, methodWidth)
	sep := strings.Repeat("─", toolWidth+statusWidth+methodWidth+2)

	var b strings.Builder
	header := fmt.Sprintf(format, "Tool", "Status", "Via")
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(sep)
	b.WriteByte('\n')

	for _, tr := range r.Tools {
		method := tr.Method
		if tr.Provider != "" {
			method = tr.Provider
		}
		if method == "" {
			method = "—"
		}
		label := statusLabel(tr.Status)
		// Pad the label to statusWidth-1 BEFORE colorizing — ANSI escape
		// codes are zero-width visually but count toward len(), which
		// would otherwise throw off column alignment.
		paddedLabel := label
		if pad := statusWidth - 1 - len(label); pad > 0 {
			paddedLabel = label + strings.Repeat(" ", pad)
		}
		if color {
			if c := statusColor(tr.Status); c != "" {
				paddedLabel = c + paddedLabel + "\033[0m"
			}
		}
		row := fmt.Sprintf("%-*s %s %-*s\n",
			toolWidth, truncate(tr.Tool, toolWidth-1),
			paddedLabel,
			methodWidth, method,
		)
		b.WriteString(row)
		// Error and plan-detail lines live on their own indented row and
		// do not participate in column alignment: never truncate them.
		// Truncating the tail drops exactly the informative suffix
		// (asset filename, version, arch, root-cause) a dry-run must show.
		if tr.Status == StatusFailed && tr.Error != "" {
			errLine := "    ↳ " + tr.Error
			if color {
				errLine = "\033[31m" + errLine + "\033[0m"
			}
			b.WriteString(errLine)
			b.WriteByte('\n')
		}
		for _, detail := range planDetailLines(tr) {
			detailLine := "    ↳ " + detail
			if color {
				detailLine = "\033[2m" + detailLine + "\033[0m"
			}
			b.WriteString(detailLine)
			b.WriteByte('\n')
		}
	}

	b.WriteString(sep)
	b.WriteByte('\n')
	b.WriteString(r.Summary())
	b.WriteByte('\n')
	return b.String()
}

func planDetailLines(tr ToolResult) []string {
	if tr.PlanIntent == nil {
		return nil
	}
	p := tr.PlanIntent
	lines := make([]string, 0, 5+len(p.Artifacts))
	if p.Identity.Package != "" && (tr.Provider != "" || p.Identity.Package != tr.Tool) {
		lines = append(lines, "package: "+p.Identity.Package)
	}
	if p.Identity.Version != "" {
		lines = append(lines, "version: "+p.Identity.Version)
	} else if p.Identity.Revision != "" {
		lines = append(lines, "revision: "+p.Identity.Revision)
	} else if detail := requestedVersionDetail(p.Identity.RequestedVersion); detail != "" {
		lines = append(lines, detail)
	}
	if p.Identity.Source != "" {
		lines = append(lines, "source: "+p.Identity.Source)
	}
	if p.Identity.Registry != "" && p.Identity.Registry != p.Identity.Source {
		lines = append(lines, "registry: "+p.Identity.Registry)
	}
	for _, artifact := range p.Artifacts {
		switch {
		case artifact.URL != "":
			lines = append(lines, "url: "+artifact.URL)
		case artifact.LocalPath != "":
			lines = append(lines, "artifact: "+artifact.LocalPath)
		}
	}
	return lines
}

func requestedVersionDetail(intent *plan.VersionIntent) string {
	if intent == nil {
		return ""
	}
	switch intent.Mode {
	case plan.VersionGitBranch:
		return "branch: " + intent.Value
	case plan.VersionGitTag:
		return "tag: " + intent.Value
	case plan.VersionGitRevision:
		return "revision: " + intent.Value
	case plan.VersionConstraint:
		return "version constraint: " + intent.Value
	case plan.VersionChannel:
		if intent.Channel == nil {
			return ""
		}
		parts := make([]string, 0, 3)
		if intent.Channel.Name != "" {
			parts = append(parts, intent.Channel.Name)
		}
		if intent.Channel.Track != "" {
			parts = append(parts, "track="+intent.Channel.Track)
		}
		if intent.Channel.Risk != "" {
			parts = append(parts, "risk="+intent.Channel.Risk)
		}
		if len(parts) > 0 {
			return "channel: " + strings.Join(parts, ", ")
		}
	}
	return ""
}

// JSON returns the report as a JSON string.
func (r *ExecReport) JSON() string {
	type jsonTool struct {
		Tool       string                    `json:"tool"`
		Status     string                    `json:"status"`
		Method     string                    `json:"method,omitempty"`
		Provider   string                    `json:"provider,omitempty"`
		Error      string                    `json:"error,omitempty"`
		PlanIntent *plan.ResolvedInstallPlan `json:"plan_intent,omitempty"`
	}
	out := struct {
		Tools        []jsonTool `json:"tools"`
		Summary      string     `json:"summary"`
		Success      int        `json:"success"`
		Failed       int        `json:"failed"`
		Skipped      int        `json:"skipped"`
		Already      int        `json:"already"`
		WouldInstall int        `json:"would_install,omitempty"`
		Seconds      float64    `json:"duration_seconds"`
	}{
		Summary:      r.Summary(),
		Success:      r.Success,
		Failed:       r.Failed,
		Skipped:      r.Skipped,
		Already:      r.Already,
		WouldInstall: r.WouldInstall,
		Seconds:      r.Duration.Seconds(),
	}
	for _, tr := range r.Tools {
		out.Tools = append(out.Tools, jsonTool{
			Tool:       tr.Tool,
			Status:     statusLabel(tr.Status),
			Method:     tr.Method,
			Provider:   tr.Provider,
			Error:      tr.Error,
			PlanIntent: tr.PlanIntent,
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
