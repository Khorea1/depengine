package methodkind

import "github.com/Khorea1/depengine/pkg/plan"

// CandidateRequirements carries requirements that are not intrinsic fields of
// ResolvedInstallPlan.
type CandidateRequirements struct {
	Transition           plan.TransitionKind
	RequireImmutableLock bool
}

// RequiredCapabilities combines semantic plan intent with operation-level
// lifecycle and reproducibility requirements.
func RequiredCapabilities(p plan.ResolvedInstallPlan, requirements CandidateRequirements) (Capability, error) {
	required, err := PlanCapabilities(p)
	if err != nil {
		return 0, err
	}
	if requirements.Transition != "" {
		lifecycle, err := LifecycleCapabilities(requirements.Transition)
		if err != nil {
			return 0, err
		}
		required |= lifecycle
	}
	required |= LockCapabilities(requirements.RequireImmutableLock)
	return required, nil
}

// MissingRequirements is the adapter-neutral capability boundary for a
// resolved candidate plus the operation the caller intends to perform.
func (c Contract) MissingRequirements(p plan.ResolvedInstallPlan, requirements CandidateRequirements) (Capability, error) {
	required, err := RequiredCapabilities(p, requirements)
	if err != nil {
		return 0, err
	}
	return required &^ c.Capabilities, nil
}
