package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

// ExecuteResolvedUpgradeCandidate executes exactly one previously resolved upgrade target.
// It never falls back or resolves again, and requires the tracked state captured during discovery.
func (ex *Executor) ExecuteResolvedUpgradeCandidate(ctx context.Context, schema *config.Schema, clan string, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan, expectedPrevious state.ToolState) (ToolResult, error) {
	expectedPrevious = cloneExpectedPreviousState(expectedPrevious)
	if schema == nil || tool == nil || method == nil || resolved == nil {
		return ToolResult{}, fmt.Errorf("schema, tool, method, and resolved plan are required")
	}
	if schema.Tools[tool.Name] != tool {
		return ToolResult{}, fmt.Errorf("tool %q is not the schema's candidate", tool.Name)
	}
	if err := resolved.Validate(); err != nil {
		return ToolResult{}, fmt.Errorf("invalid resolved plan: %w", err)
	}
	if resolved.Tool.Name != tool.Name || resolved.Candidate.Method != method.Kind {
		return ToolResult{}, fmt.Errorf("resolved plan does not match tool %q and method %q", tool.Name, method.Kind)
	}
	matched := false
	for _, candidate := range tool.Methods {
		if candidate == method {
			matched = true
			break
		}
	}
	if !matched {
		return ToolResult{}, fmt.Errorf("method %q is not a candidate of tool %q", method.Kind, tool.Name)
	}

	start := time.Now()
	housekeepingCtx := run.WithOmittedEnv(ctx, schemaSecretEnvNames(schema)...)
	rc := ex.newRunContext(housekeepingCtx, schema, clan)
	stop, err := ex.initializeRun(housekeepingCtx, rc)
	if err != nil {
		return ToolResult{}, err
	}
	if stop != nil {
		defer stop()
	}
	if err := ex.recoverAndRecord(housekeepingCtx, rc); err != nil {
		return ToolResult{}, err
	}
	if !ex.allowArbitraryCode && ex.hasDangerousMethod(rc, tool) {
		return ToolResult{}, fmt.Errorf("tool %q requires --allow-arbitrary-code", tool.Name)
	}
	toolCtx := omitToolSecretEnvironment(housekeepingCtx, tool)
	if ex.toolTimeout > 0 {
		var cancel context.CancelFunc
		toolCtx, cancel = context.WithTimeout(toolCtx, ex.toolTimeout)
		defer cancel()
	}
	for _, dependency := range tool.EffectiveRequires(ex.facts) {
		dependencyResult, dependencyErr := ex.executeDependency(toolCtx, rc, dependency)
		if dependencyErr != nil || !dependencySucceeded(dependencyResult) {
			reason := dependencyResult.Error
			if dependencyErr != nil {
				reason = dependencyErr.Error()
			}
			result := ToolResult{Tool: tool.Name, Status: StatusFailed, Error: fmt.Sprintf("requires failed dependency: %s (%s)", dependency, reason)}
			rc.report.Tools = append(rc.report.Tools, result)
			if _, err := ex.finishRun(ctx, schema, rc.report, start); err != nil {
				return result, err
			}
			return result, nil
		}
	}
	result := ToolResult{Tool: tool.Name}
	seed := &candidateResolutionSeed{method: method, resolved: resolved, requireUpgrade: true, expectedPrevious: &expectedPrevious}
	if !ex.attemptMethod(toolCtx, rc, tool, method, &result, start, seed) {
		ex.finishExhausted(&result, method.Kind, start)
	}
	rc.report.Tools = append(rc.report.Tools, result)
	if _, err := ex.finishRun(ctx, schema, rc.report, start); err != nil {
		return result, err
	}
	return result, nil
}
