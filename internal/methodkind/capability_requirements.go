package methodkind

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
)

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
	missing := required &^ c.Capabilities
	if supportsSharedSourceAuth(p) {
		missing &^= CapabilityAuth
	}
	return missing, nil
}

// CheckRequirements validates that the contract can honor the complete
// adapter-neutral candidate intent. Authentication mismatch is classified
// separately so planners can explain that a candidate is structurally usable
// but lacks the required secure credential transport rather than silently
// weakening the request or falling through to a downloader chosen by host
// tooling.
func (c Contract) CheckRequirements(p plan.ResolvedInstallPlan, requirements CandidateRequirements) error {
	missing, err := c.MissingRequirements(p, requirements)
	if err != nil {
		return err
	}
	if missing == 0 {
		return nil
	}
	class := plan.ErrorUnsupportedCapability
	if missing&CapabilityAuth != 0 {
		class = plan.ErrorAuthRequirement
	}
	return &plan.PlannerError{
		Class: class,
		Op:    "candidate capabilities",
		Err:   fmt.Errorf("method %q is missing required capabilities: %s", c.Kind, strings.Join(CapabilityNames(missing), ", ")),
	}
}
