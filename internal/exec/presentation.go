package exec

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
)

// shouldUseColor reports whether ANSI color codes should be emitted for the
// current process's stderr.
func shouldUseColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (ex *Executor) colorizeStatusSymbol(line string) string {
	if !ex.color {
		return line
	}
	colors := []struct {
		symbol string
		ansi   string
	}{
		{"✓", "\033[32m"},
		{"✗", "\033[31m"},
		{"–", "\033[33m"},
		{"→", "\033[36m"},
		{"•", "\033[2m"},
	}
	for _, color := range colors {
		if strings.Contains(line, color.symbol) {
			return strings.Replace(line, color.symbol, color.ansi+color.symbol+"\033[0m", 1)
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
		return fmt.Sprintf("  → %s: commit: would install via %s (dry-run)\n", tool, method)
	case StatusVirtual:
		return fmt.Sprintf("  • %s: dependency group\n", tool)
	case StatusFailed:
		return fmt.Sprintf("  ✗ %s: failed (%s)\n", tool, errMsg)
	default:
		return fmt.Sprintf("  ? %s: %v\n", tool, status)
	}
}

func (ex *Executor) recordToolResult(ctx context.Context, rc *runContext, result *ToolResult) {
	if result.Provider == "" {
		result.Provider = ex.providerForMethodKind(result.MethodKind, rc.nativeManagerName)
	}
	presentedMethod := result.Method
	if result.Provider != "" {
		presentedMethod = result.Provider
	}
	report := rc.report
	report.mu.Lock()
	defer report.mu.Unlock()
	report.addResultLocked(*result)
	switch result.Status {
	case StatusInstalled:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "installed", "duration", result.Duration)
	case StatusAlready:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "already", "duration", result.Duration)
	case StatusSkippedWhen:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "skipped_when")
	case StatusSkippedUnavailable:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "skipped_unavailable")
	case StatusFailed:
		ex.logWarn(ctx, "tool", "tool", result.Tool, "status", "failed", "error", result.Error, "duration", result.Duration)
	case StatusWouldInstall:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "would_install")
	case StatusVirtual:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "virtual")
	}
	if !ex.quiet {
		ex.outputf("%s", ex.colorizeStatusSymbol(formatToolResult(result.Tool, result.Status, presentedMethod, result.Error)))
		if ex.dryRun && result.Status == StatusWouldInstall {
			for _, line := range operationPreviewLines(result.PlanIntent) {
				ex.outputf("    %s\n", line)
			}
		}
		if result.RebootRequired {
			ex.outputf("    reboot required to complete %s\n", result.Tool)
		}
	}
}

// operationPreviewLines renders what the resolved plan says a dry-run would
// execute. Only steps whose adapter declared a preview argv or an elevation
// decision are shown; everything else stays an explicit unknown in JSON rather
// than being invented here.
func operationPreviewLines(p *plan.ResolvedInstallPlan) []string {
	if p == nil {
		return nil
	}
	var lines []string
	for _, op := range p.Operations {
		if len(op.Preview) == 0 && op.Elevation == "" {
			continue
		}
		line := "step " + op.Kind
		if len(op.Preview) > 0 {
			line += ": " + formatPreviewArgv(op.Preview)
		}
		switch op.Elevation.Effective() {
		case plan.ElevationRequired:
			line += " (elevation: required)"
		case plan.ElevationNone:
			line += " (elevation: none)"
		}
		lines = append(lines, line)
	}
	return lines
}

func formatPreviewArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, " \t\"'") {
			parts[i] = strconv.Quote(arg)
			continue
		}
		parts[i] = arg
	}
	return strings.Join(parts, " ")
}

func (ex *Executor) recordBlockedTool(ctx context.Context, rc *runContext, toolName string) {
	ex.recordToolResult(ctx, rc, &ToolResult{
		Tool:   toolName,
		Status: StatusFailed,
		Error:  "requires --allow-arbitrary-code (tool has arbitrary code execution capability)",
	})
}
