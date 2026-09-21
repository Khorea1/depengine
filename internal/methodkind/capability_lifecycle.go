package methodkind

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/plan"
)

// LifecycleCapabilities translates a requested lifecycle transition into the
// capability a method must advertise. Install is universal; other transitions
// require explicit observation or mutation support.
func LifecycleCapabilities(transition plan.TransitionKind) (Capability, error) {
	switch transition {
	case plan.TransitionInstall:
		return 0, nil
	case plan.TransitionRepair:
		return CapabilityCheck, nil
	case plan.TransitionRemove:
		return CapabilityRemove, nil
	case plan.TransitionUpgrade:
		return CapabilityUpgrade, nil
	default:
		return 0, fmt.Errorf("unsupported lifecycle transition %q", transition)
	}
}

// LockCapabilities returns the capability required when a caller needs an
// immutable lock identity from otherwise mutable install intent.
func LockCapabilities(requireImmutable bool) Capability {
	if requireImmutable {
		return CapabilityImmutableLock
	}
	return 0
}
