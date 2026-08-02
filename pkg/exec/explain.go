package exec

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/native"
)

// ExplainTool evaluates all methods for a single tool WITHOUT installing.
// For each method it reports the status and reason: skip_when (when condition
// didn't match), skip_unavailable (no adapter or binary not on PATH),
// already_installed (Check passed), or would_install (ready to install).
//
// This is the engine behind `depengine why <tool>`.
func (ex *Executor) ExplainTool(ctx context.Context, tool *config.Tool, clan string) []MethodAttempt {
	// Resolve native manager for method_order expansion.
	if mgr, ok := native.Lookup(clan); ok {
		ex.nativeManagerName = mgr.Name
	}
	orderedMethods := config.OrderMethods(tool.Methods, ex.effectiveMethodOrder(tool))
	methods := orderedMethods
	if len(methods) == 0 {
		return []MethodAttempt{{Kind: "", Status: "virtual", Error: "dependency group (no methods declared)"}}
	}
	attempts := make([]MethodAttempt, 0, len(methods))

	for _, method := range methods {
		displayKind := method.Kind
		if method.Label != "" {
			displayKind = method.Label
		}
		attempt := MethodAttempt{Kind: displayKind}

		// Check when condition.
		if method.When != nil && !method.When.Match(ex.facts) {
			attempt.Status = "skip_when"
			attempt.Error = fmt.Sprintf("when condition not met: %+v", method.When)
			attempts = append(attempts, attempt)
			continue
		}
		adapter := ex.LookupAdapter(method.Kind)
		if adapter == nil {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("no adapter registered for kind %q", displayKind)
			attempts = append(attempts, attempt)
			continue
		}

		// Check if the adapter is available on this system.
		if !adapter.Available(ctx, ex.rn) {
			attempt.Status = "skip_unavailable"
			attempt.Error = fmt.Sprintf("adapter %q not available (binary not on PATH)", displayKind)
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
