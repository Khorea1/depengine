package exec

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
)

// CandidatePlanIntent exposes the executor's static planning boundary to
// composition-root commands that must validate one already-selected candidate
// before performing a destructive transition. It performs no host probes or
// mutations and returns the typed planner error for every capability mismatch
// the normal executor would skip.
func CandidatePlanIntent(tool *config.Tool, method *config.MethodCandidate) (*plan.ResolvedInstallPlan, error) {
	intent, err := candidatePlanIntentErr(tool, method)
	if err != nil {
		return intent, err
	}
	if intent == nil {
		if method == nil {
			return nil, fmt.Errorf("method candidate is required")
		}
		return nil, fmt.Errorf("unknown method kind %q", method.Kind)
	}
	return intent, nil
}

// candidatePlanIntent is the shared static planning boundary for execution and
// explain. It deliberately performs no host probes or mutations. The mismatch
// text is the typed planner error rendered as a string, so reasons shown by
// `why`, dry-run, and execution reports carry the same stable error class.
func candidatePlanIntent(tool *config.Tool, method *config.MethodCandidate) (*plan.ResolvedInstallPlan, string) {
	intent, err := candidatePlanIntentErr(tool, method)
	// Static planner APIs retain typed references, but execution reports must
	// not expose even the environment-variable name used to locate a secret.
	if intent != nil && method != nil && method.Kind == "github" {
		intent.Secrets = nil
	}
	if err != nil {
		return intent, err.Error()
	}
	if intent == nil {
		return nil, ""
	}
	return intent, ""
}

// candidatePlanIntentErr resolves the static plan intent and enforces the
// adapter-neutral capability boundary. Capability mismatches — including
// authentication requirements — surface as typed *plan.PlannerError values
// (auth_requirement vs unsupported_capability) from the single
// CheckRequirements helper, so planning failures carry a stable,
// machine-readable class everywhere instead of only in methodkind unit tests.
func candidatePlanIntentErr(tool *config.Tool, method *config.MethodCandidate) (*plan.ResolvedInstallPlan, error) {
	if method == nil {
		return nil, nil
	}
	if _, ok := methodkind.Lookup(method.Kind); !ok {
		return nil, nil
	}
	return planner.BuildValidatedCandidateIntent(tool, method, methodkind.CandidateRequirements{})
}
