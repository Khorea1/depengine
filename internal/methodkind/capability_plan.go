package methodkind

import "github.com/Khorea1/depengine/internal/plan"

// PlanCapabilities derives cross-cutting semantic requirements from a
// ResolvedInstallPlan.
func PlanCapabilities(p plan.ResolvedInstallPlan) (Capability, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	required, err := identityCapabilities(p.Identity)
	if err != nil {
		return 0, err
	}
	required |= artifactCapabilities(p.Artifacts)
	sourceCaps, err := SourceCapabilities(p.Sources)
	if err != nil {
		return 0, err
	}
	required |= sourceCaps
	if operationsHaveArbitraryCode(planOperations(p)) {
		required |= CapabilityArbitraryCode
	}
	return required, nil
}

// MissingPlanCapabilities reports capabilities required by an already-resolved
// semantic plan that the method contract does not declare.
func (c Contract) MissingPlanCapabilities(p plan.ResolvedInstallPlan) (Capability, error) {
	required, err := PlanCapabilities(p)
	if err != nil {
		return 0, err
	}
	return required &^ c.Capabilities, nil
}
