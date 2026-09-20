package methodkind

import (
	"fmt"

	"github.com/Khorea1/depengine/pkg/plan"
)

// SourceCapabilities derives requirements from typed source identity.
// Selection, mutation, trust, and authentication remain independent.
func SourceCapabilities(sources []plan.SourceReference) (Capability, error) {
	var required Capability
	for i, source := range sources {
		if err := source.Validate(); err != nil {
			return 0, fmt.Errorf("source %d: %w", i, err)
		}
		required |= CapabilitySourceSelection
		if source.Role == plan.SourceHostConfiguration {
			required |= CapabilitySourceMutation
		}
		if source.Trust != nil {
			required |= CapabilitySourceTrust
		}
		if source.SecretRef != nil {
			required |= CapabilityAuth
		}
	}
	return required, nil
}
