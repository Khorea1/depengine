package methodkind

// Capability describes a semantic feature a method can honor. Capabilities
// are planner-facing metadata: they answer whether a candidate can represent
// requested intent without weakening it or branching on method names.
type Capability uint64

const (
	CapabilityExactVersion Capability = 1 << iota
	CapabilityChannel
	CapabilityImmutableIdentity
	CapabilityArbitraryCode
	CapabilitySourceSelection
	CapabilityArchitecture
	CapabilityScope
	CapabilityRevision
	CapabilityEnvironmentTarget
	CapabilityVersionConstraint
	CapabilityMutableTag
	CapabilitySourceMutation
	CapabilitySourceTrust
	CapabilityAuth
	CapabilityCheck
	CapabilityRemove
	CapabilityUpgrade
	CapabilityImmutableLock
	CapabilityLocalArtifact
)

// Supports reports whether every requested capability is declared by the
// contract. A zero request is always satisfied.
func (c Contract) Supports(requested Capability) bool {
	return c.Capabilities&requested == requested
}
