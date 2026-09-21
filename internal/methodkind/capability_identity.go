package methodkind

import "github.com/Khorea1/depengine/internal/plan"

func identityCapabilities(identity plan.ResolvedIdentity) (Capability, error) {
	var required Capability
	if identity.RequestedVersion != nil {
		versionCaps, err := VersionIntentCapabilities(*identity.RequestedVersion)
		if err != nil {
			return 0, err
		}
		required |= versionCaps
	}
	if identity.Scope != "" {
		required |= CapabilityScope
	}
	if identity.Environment != nil {
		required |= CapabilityEnvironmentTarget
	}
	if identity.Architecture != "" || identity.Platform != "" {
		required |= CapabilityArchitecture
	}
	return required, nil
}
