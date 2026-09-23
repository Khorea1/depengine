package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// VerifyResolvedCandidate observes the target selected by a resolved plan and
// reconciles that observation against its desired identity.
func (ex *Executor) VerifyResolvedCandidate(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (plan.VerificationResult, error) {
	verification, _, err := ex.verifyResolvedCandidate(ctx, tool, method, resolved)
	return verification, err
}

func (ex *Executor) verifyResolvedCandidate(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (plan.VerificationResult, plan.Observation, error) {
	if tool == nil || method == nil || resolved == nil {
		return plan.VerificationResult{}, plan.Observation{}, fmt.Errorf("tool, method, and resolved plan are required")
	}
	if err := resolved.Validate(); err != nil {
		return plan.VerificationResult{}, plan.Observation{}, fmt.Errorf("invalid resolved plan: %w", err)
	}
	if resolved.Tool.Name != tool.Name {
		return plan.VerificationResult{}, plan.Observation{}, fmt.Errorf("resolved tool %q does not match %q", resolved.Tool.Name, tool.Name)
	}
	if resolved.Candidate.Method != method.Kind {
		return plan.VerificationResult{}, plan.Observation{}, fmt.Errorf("resolved method %q does not match %q", resolved.Candidate.Method, method.Kind)
	}
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return plan.VerificationResult{}, plan.Observation{}, fmt.Errorf("no adapter registered for %q", method.Kind)
	}
	displayKind := displayMethodKind(method)
	observation := ex.observeResolvedCandidate(ctx, tool, method, adapter, resolved, displayKind)
	verification := plan.Reconcile(resolved.Identity, observation)
	if err := verification.Validate(); err != nil {
		return plan.VerificationResult{}, plan.Observation{}, fmt.Errorf("%s: invalid verification result: %w", displayKind, err)
	}
	return verification, observation, nil
}

func verificationDetail(v plan.VerificationResult) string {
	if v.State == plan.StateDrifted {
		parts := make([]string, 0, len(v.Drift))
		for _, drift := range v.Drift {
			parts = append(parts, fmt.Sprintf("%s: desired=%q observed=%q", drift.Field, drift.Desired, drift.Observed))
		}
		return strings.Join(parts, "; ")
	}
	return v.Detail
}

// ResolveAndVerifyCandidate resolves one candidate through the executor's
// canonical resolver, then verifies exactly that resolved target.
func (ex *Executor) ResolveAndVerifyCandidate(ctx context.Context, tool *config.Tool, method *config.MethodCandidate) (*plan.ResolvedInstallPlan, plan.VerificationResult, error) {
	resolved, err := ex.ResolveCandidatePlan(ctx, tool, method)
	if err != nil {
		return nil, plan.VerificationResult{}, err
	}
	verification, err := ex.VerifyResolvedCandidate(ctx, tool, method, resolved)
	return resolved, verification, err
}

// ResolveCandidatePlan resolves one selected candidate through the executor's
// canonical path without probing the host.
func (ex *Executor) ResolveCandidatePlan(ctx context.Context, tool *config.Tool, method *config.MethodCandidate) (*plan.ResolvedInstallPlan, error) {
	if tool == nil || method == nil {
		return nil, fmt.Errorf("tool and method are required")
	}
	intent, mismatch := candidatePlanIntent(tool, method)
	if mismatch != "" {
		return nil, fmt.Errorf("%s", mismatch)
	}
	if intent == nil {
		return nil, fmt.Errorf("candidate %q has no resolvable plan", method.Kind)
	}
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return nil, fmt.Errorf("no adapter registered for %q", method.Kind)
	}
	intent = ex.hostResolvedPlanIntent(method, intent)
	resolved, err := ex.resolveCandidatePlan(ctx, tool, method, adapter, intent, displayMethodKind(method))
	if err != nil {
		return nil, err
	}
	return resolved, nil
}
