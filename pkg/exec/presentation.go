package exec

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ShouldUseColor reports whether ANSI color codes should be emitted for the
// current process's stderr.
func ShouldUseColor() bool {
	return shouldUseColor()
}

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
		report.WouldInstall++
		ex.logDebug(ctx, "tool", "tool", result.Tool, "method", result.Method, "status", "would_install")
	case StatusVirtual:
		ex.logDebug(ctx, "tool", "tool", result.Tool, "status", "virtual")
	}
	if !ex.quiet {
		ex.outputf("%s", ex.colorizeStatusSymbol(formatToolResult(result.Tool, result.Status, result.Method, result.Error)))
		if result.RebootRequired {
			ex.outputf("    reboot required to complete %s\n", result.Tool)
		}
	}
}

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
