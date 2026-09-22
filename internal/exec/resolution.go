package exec

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// resolveCandidatePlan is the single read-only resolution point shared by
// dry-run, why, and real install. It performs no source, prerequisite, or
// package mutations.
//
// Contract:
//
//	intent estático
//	    ↓
//	PlanResolver.ResolvePlan(), se existir
//	    ↓
//	plan.ValidateResolution (intent preservation + resolved.Validate)
//
// The returned plan is the concrete executable identity. No layer below the
// executor may resolve GitHub, {latest}, tags, or assets again after this
// point. Compatibility against the concrete plan stays with the caller so
// resolve failures (failed) and host incompatibility (skip_unavailable) keep
// distinct attempt statuses.
func (ex *Executor) resolveCandidatePlan(
	ctx context.Context,
	tool *config.Tool,
	method *config.MethodCandidate,
	adapter Adapter,
	intent *plan.ResolvedInstallPlan,
	displayKind string,
) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, nil
	}
	resolver, ok := adapter.(PlanResolver)
	if !ok {
		return intent, nil
	}
	resolved, err := resolver.ResolvePlan(ctx, ex.probeRunner(tool.Name, displayKind), tool, method, intent)
	if err != nil {
		return intent, fmt.Errorf("%s: resolve plan: %w", displayKind, err)
	}
	if resolved == nil {
		return intent, fmt.Errorf("%s: resolve plan: resolver returned nil plan", displayKind)
	}
	if err := plan.ValidateResolution(*intent, *resolved); err != nil {
		return intent, fmt.Errorf("%s: resolve plan: %w", displayKind, err)
	}
	return resolved, nil
}
