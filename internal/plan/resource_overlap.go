package plan

// ResourceOverlap describes the overlap evidence for two prospective effects.
// Same means at least one identical declared target, NOT identical write-sets
// or proof of ownership. Disjoint requires independent, exhaustive separation
// evidence; the current target model cannot provide it.
type ResourceOverlap string

const (
	OverlapSame     ResourceOverlap = "same"
	OverlapDisjoint ResourceOverlap = "disjoint"
	OverlapUnknown  ResourceOverlap = "unknown"
)

// CompareProspectiveTargets compares target identities without consulting the
// host. Exact equality is evidence of a shared declared resource slot; any
// other result remains unknown. In particular, distinct canonical paths can
// refer to the same bytes through hardlinks or mounts, and differing kinds may
// represent the same physical resource. A caller must not infer disjointness.
func validProspectiveTarget(t ProspectiveTarget) bool {
	return t.Scope != "" && t.Kind != "" && t.Identity != ""
}

func CompareProspectiveTargets(a, b ProspectiveTarget) ResourceOverlap {
	if !validProspectiveTarget(a) || !validProspectiveTarget(b) {
		return OverlapUnknown
	}
	if a == b {
		return OverlapSame
	}
	return OverlapUnknown
}

// CompareProspectiveEffectEnvelopes compares only the partially known target
// identities of two resolved transitions. It reports Same as soon as one
// declared target is shared, even when both envelopes have unknown effects.
// Without a trusted, exhaustive write-set and separate non-aliasing evidence
// it can never assert Disjoint. This is a read-only diagnostic primitive, not
// an admission decision: it does not authorize any host mutation.
func CompareProspectiveEffectEnvelopes(a, b ProspectiveEffectEnvelope) ResourceOverlap {
	known := make(map[ProspectiveTarget]struct{})
	for _, step := range a.Steps {
		for _, target := range step.Targets {
			if validProspectiveTarget(target) {
				known[target] = struct{}{}
			}
		}
	}
	for _, step := range b.Steps {
		for _, target := range step.Targets {
			if _, found := known[target]; found && validProspectiveTarget(target) {
				return OverlapSame
			}
		}
	}
	return OverlapUnknown
}
