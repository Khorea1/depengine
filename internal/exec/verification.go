package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// VerifyResolvedCandidate observes the target selected by a resolved plan and
// reconciles that observation against its desired identity.
func (ex *Executor) VerifyResolvedCandidate(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (plan.VerificationResult, error) {
	ctx = run.WithOmittedEnv(ctx, methodSecretEnvNames(method)...)
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
	desired := projectVerificationIdentity(method.Kind, resolved.Identity)
	verification := plan.Reconcile(desired, observation)
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
func (ex *Executor) ResolveAndVerifyCandidate(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, clan string) (*plan.ResolvedInstallPlan, plan.VerificationResult, error) {
	return ex.ResolveAndVerifyCandidateAtVersion(ctx, tool, method, "", clan)
}

// ResolveAndVerifyCandidateAtVersion resolves a candidate through the canonical
// resolver, optionally projects an exact desired version onto the resolved
// identity, validates that projection, and verifies exactly that target.
//
// The version projection belongs here rather than in command workflows so
// status, upgrade, and future lock consumers cannot accidentally verify a
// different identity from the one they resolved.
func (ex *Executor) ResolveAndVerifyCandidateAtVersion(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, desiredVersion, clan string) (*plan.ResolvedInstallPlan, plan.VerificationResult, error) {
	effective, resolved, err := ex.resolveEffectiveCandidatePlan(ctx, tool, method, clan)
	if err != nil {
		return nil, plan.VerificationResult{}, err
	}
	if desiredVersion != "" {
		projected := resolved.Clone()
		projected.Identity.Version = desiredVersion
		if err := projected.Validate(); err != nil {
			return nil, plan.VerificationResult{}, fmt.Errorf("project desired version %q: %w", desiredVersion, err)
		}
		resolved = &projected
	}
	verification, err := ex.VerifyResolvedCandidate(ctx, tool, effective, resolved)
	return resolved, verification, err
}

// resolveEffectiveCandidatePlan resolves one selected candidate through the
// executor's canonical path and returns the effective method actually used
// alongside its resolved plan.
//
// The effective method is the specialized copy when distributions apply, or
// the declared candidate itself otherwise (same pointer, zero behavior
// delta). Callers that verify, identify, or install must use the effective
// method: the declared candidate may describe a different payload (for
// example the base entrypoints instead of the Windows override). Kind,
// Label, and the candidate's ordinal in Tool.Methods stay those of the
// declared candidate: specialization clones without renaming or reordering.
//
// Pure with respect to the schema: specialization never mutates the declared
// candidate and performs no network calls.
func (ex *Executor) resolveEffectiveCandidatePlan(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, clan string) (*config.MethodCandidate, *plan.ResolvedInstallPlan, error) {
	if tool == nil || method == nil {
		return nil, nil, fmt.Errorf("tool and method are required")
	}
	// State-restored candidates do not persist host-specific selection metadata.
	// Rehydrate a local candidate copy so resolution does not mutate caller-owned state.
	if ex.facts != nil && method.Config != nil &&
		((method.TargetArch == "" && ex.facts.TargetArch != "") || (method.TargetOS == "" && ex.facts.OS != "")) {
		if _, hasRepo := method.Config["repo"]; hasRepo {
			resolvedMethod := *method
			if resolvedMethod.TargetArch == "" {
				resolvedMethod.TargetArch = ex.facts.TargetArch
			}
			if resolvedMethod.TargetOS == "" {
				resolvedMethod.TargetOS = ex.facts.OS
			}
			method = &resolvedMethod
		}
	}
	ctx = run.WithOmittedEnv(ctx, methodSecretEnvNames(method)...)
	// Canonical distribution gate: status, upgrade, remove, and undo resolve
	// through here. Specialize before the first effective plan so every
	// consumer resolves the same payload; ineligibility fails here (callers
	// iterate methods themselves) and ambiguity aborts loudly.
	specialized, skipStatus, specErr := ex.specializeMethodForHost(method)
	if specErr != nil {
		return nil, nil, specErr
	}
	if skipStatus != "" {
		return nil, nil, fmt.Errorf("candidate %q has no distribution matching this host", method.Kind)
	}
	method = specialized
	intent, mismatch := candidatePlanIntent(tool, method)
	if mismatch != "" {
		return nil, nil, fmt.Errorf("%s", mismatch)
	}
	if intent == nil {
		return nil, nil, fmt.Errorf("candidate %q has no resolvable plan", method.Kind)
	}
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return nil, nil, fmt.Errorf("no adapter registered for %q", method.Kind)
	}
	intent = ex.hostResolvedPlanIntent(method, intent, clan)
	resolved, err := ex.resolveCandidatePlan(ctx, tool, method, adapter, intent, displayMethodKind(method))
	if err != nil {
		return nil, nil, err
	}
	return method, resolved, nil
}

// ResolveCandidatePlan resolves one selected candidate through the executor's
// canonical path without probing the host. It is a wrapper over
// resolveEffectiveCandidatePlan preserving the historical API; callers that
// also verify must prefer ResolveAndVerifyCandidateAtVersion so verification
// observes the effective candidate.
func (ex *Executor) ResolveCandidatePlan(ctx context.Context, tool *config.Tool, method *config.MethodCandidate, clan string) (*plan.ResolvedInstallPlan, error) {
	_, resolved, err := ex.resolveEffectiveCandidatePlan(ctx, tool, method, clan)
	return resolved, err
}
