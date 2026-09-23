package methodkind

import (
	"net/url"

	"github.com/Khorea1/depengine/internal/plan"
)

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
	if len(p.Secrets) > 0 {
		required |= CapabilityAuth
	}
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
	missing := required &^ c.Capabilities
	if supportsSharedSourceAuth(p) {
		missing &^= CapabilityAuth
	}
	return missing, nil
}

// supportsSharedSourceAuth recognizes the one credential transport owned by
// the executor source layer. Other secret references stay fail-closed.
func supportsSharedSourceAuth(p plan.ResolvedInstallPlan) bool {
	if len(p.Secrets) == 0 {
		for _, source := range p.Sources {
			if source.SecretRef != nil {
				return false
			}
		}
		return false
	}
	seen := make(map[plan.SecretReference]bool, len(p.Secrets))
	for _, source := range p.Sources {
		if source.SecretRef == nil {
			continue
		}
		if source.Role != plan.SourceHostConfiguration || (source.Kind != "brew-tap" && source.Kind != "scoop-bucket") {
			return false
		}
		u, err := url.Parse(source.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		seen[*source.SecretRef] = true
	}
	for _, ref := range p.Secrets {
		if !seen[ref] {
			return false
		}
	}
	return true
}
