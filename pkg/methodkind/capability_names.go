package methodkind

var capabilityNames = []struct {
	cap  Capability
	name string
}{
	{CapabilityExactVersion, "exact-version"},
	{CapabilityChannel, "channel"},
	{CapabilityImmutableIdentity, "immutable-identity"},
	{CapabilityArbitraryCode, "arbitrary-code"},
	{CapabilitySourceSelection, "source-selection"},
	{CapabilityArchitecture, "architecture"},
	{CapabilityScope, "scope"},
	{CapabilityRevision, "revision"},
	{CapabilityEnvironmentTarget, "environment-profile"},
	{CapabilityVersionConstraint, "version-constraint"},
	{CapabilityMutableTag, "mutable-tag"},
	{CapabilitySourceMutation, "source-mutation"},
	{CapabilitySourceTrust, "source-trust"},
	{CapabilityAuth, "auth"},
	{CapabilityCheck, "check"},
	{CapabilityRemove, "remove"},
	{CapabilityUpgrade, "upgrade"},
	{CapabilityImmutableLock, "immutable-lock"},
	{CapabilityLocalArtifact, "local-artifact"},
}

// CapabilityNames returns stable human-readable names for a capability mask.
func CapabilityNames(capabilities Capability) []string {
	out := make([]string, 0, len(capabilityNames))
	for _, item := range capabilityNames {
		if capabilities&item.cap != 0 {
			out = append(out, item.name)
		}
	}
	return out
}
