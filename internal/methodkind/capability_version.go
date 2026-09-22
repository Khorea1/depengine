package methodkind

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/plan"
)

var versionCapabilities = map[plan.VersionMode]Capability{
	plan.VersionLatest:       0,
	plan.VersionExact:        CapabilityExactVersion,
	plan.VersionConstraint:   CapabilityVersionConstraint,
	plan.VersionChannel:      CapabilityChannel,
	plan.VersionGitTag:       CapabilityRevision,
	plan.VersionGitBranch:    CapabilityRevision,
	plan.VersionGitRevision:  CapabilityRevision,
	plan.VersionContainerTag: CapabilityMutableTag,
	plan.VersionDigest:       CapabilityImmutableIdentity,
}

// VersionIntentCapabilities translates shared version intent into semantic
// capabilities without weakening invalid or unknown modes.
func VersionIntentCapabilities(intent plan.VersionIntent) (Capability, error) {
	if err := intent.Validate(); err != nil {
		return 0, err
	}
	capability, ok := versionCapabilities[intent.Mode]
	if !ok {
		return 0, fmt.Errorf("unsupported version mode %q", intent.Mode)
	}
	return capability, nil
}

// MissingVersionCapabilities reports why this contract cannot represent the
// requested version intent without weakening it.
func (c Contract) MissingVersionCapabilities(intent plan.VersionIntent) (Capability, error) {
	required, err := VersionIntentCapabilities(intent)
	if err != nil {
		return 0, err
	}
	return required &^ c.Capabilities, nil
}
