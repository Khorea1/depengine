package plan

import "fmt"

// EffectCompleteness expresses how completely a concrete operation's potential
// host write-set is known. It is NOT an authorization or a claim of ownership.
// In particular, an empty observation does not establish EffectExact.
type EffectCompleteness string

const (
	EffectExact   EffectCompleteness = "exact"
	EffectBounded EffectCompleteness = "bounded"
	EffectUnknown EffectCompleteness = "unknown"
)

// CombineEffectCompleteness is monotonic: an unknown step makes the whole
// transaction unknown, and a bounded step prevents an exact transaction.
// Invalid or missing classifications are errors, never treated as exact.
func CombineEffectCompleteness(classes ...EffectCompleteness) (EffectCompleteness, error) {
	if len(classes) == 0 {
		return EffectUnknown, nil
	}
	result := EffectExact
	for _, class := range classes {
		switch class {
		case EffectExact:
		case EffectBounded:
			if result == EffectExact {
				result = EffectBounded
			}
		case EffectUnknown:
			result = EffectUnknown
		default:
			return "", fmt.Errorf("invalid effect completeness %q", class)
		}
	}
	return result, nil
}

// ProspectiveTarget is a possible physical destination inferred from the
// concrete candidate and current host. A target is partial evidence, NOT a
// complete write-set, an observed resource, or an ownership/permission claim.
// Its identity must be revalidated inside a future host transaction before
// being used to arbitrate conflicting installations.
type ProspectiveTarget struct {
	Scope    string `json:"scope"`
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}

// ProspectiveEffectStep is a conservative, credential-free description of a
// potentially mutating phase. Index is the index within the matching plan
// collection (not an adapter command or an installation identity). Conditional
// records lazy, rollback, or when/post-hook conditionality. The phase must
// still be considered for admission; enumeration is not execution order.
type ProspectiveEffectStep struct {
	Phase        string
	Index        int
	Conditional  bool
	Completeness EffectCompleteness
	// Targets may name known destinations even while Completeness is unknown.
	// Never interpret a nonempty target list as exhaustive.
	Targets []ProspectiveTarget `json:"targets,omitempty"`
}

// ProspectiveEffectEnvelope is a read-only inventory of potentially mutating
// phases for one resolved candidate and one transition. An adapter may attach
// partial prospective targets, but the envelope is not an exhaustive physical
// write-set and does not establish ownership or authorize installation.
type ProspectiveEffectEnvelope struct {
	Tool         string
	Method       string
	Transition   TransitionKind
	Completeness EffectCompleteness
	Steps        []ProspectiveEffectStep
}

// DescribeProspectiveEffects enumerates the phases that may mutate the host.
// Until adapter-specific, independently validated effect evidence exists, all
// phases are UNKNOWN. This deliberately does not interpret Observed resources,
// OwnedPaths, a lock digest, command previews or names as a prospective write-set.
//
// Hooks with un-evaluated host conditions are included conservatively. A
// fallback candidate requires a fresh call on its own resolved plan; neither
// this function nor its output changes candidate identity or resolves sources.
func DescribeProspectiveEffects(resolved *ResolvedInstallPlan, transition TransitionKind) (ProspectiveEffectEnvelope, error) {
	if resolved == nil {
		return ProspectiveEffectEnvelope{}, fmt.Errorf("prospective effects: nil resolved plan")
	}
	if !transition.Valid() {
		return ProspectiveEffectEnvelope{}, fmt.Errorf("prospective effects: invalid transition %q", transition)
	}
	if err := resolved.Validate(); err != nil {
		return ProspectiveEffectEnvelope{}, fmt.Errorf("prospective effects: %w", err)
	}

	envelope := ProspectiveEffectEnvelope{
		Tool: resolved.Tool.Name, Method: resolved.Candidate.Method,
		Transition: transition, Completeness: EffectUnknown,
	}
	add := func(phase string, index int, conditional bool) {
		envelope.Steps = append(envelope.Steps, ProspectiveEffectStep{
			Phase: phase, Index: index, Conditional: conditional,
			Completeness: EffectUnknown,
		})
	}
	// Removal does not run the chosen install candidate's artifact, preparation,
	// prerequisite or source-setup phases. Removal and cleanup still have
	// unknown effects, represented by the opaque primary_remove action.
	if transition != TransitionRemove {
		// Reached prerequisites are lazy. Neither their method nor the resource
		// written by their own installer is proved by the parent plan.
		for i := range resolved.Prerequisites {
			add("prerequisite", i, true)
		}
		for i := range resolved.Sources {
			// Even when a source is only declared, its adapter may prepare local
			// indexes or host repositories. We cannot prove this read-only here.
			add("source", i, true)
		}
		for i := range resolved.Artifacts {
			add("artifact", i, true)
		}
		if resolved.Preparation != nil {
			for i, mutation := range resolved.Preparation.Prepare {
				add("prepare", i, false)
				if mutation.Rollback != nil {
					add("rollback", i, true)
				}
			}
			for i := range resolved.Preparation.Commit {
				add("commit", i, false)
			}
		}
		for i, operation := range resolved.SourceMutations {
			if operation.Effect == EffectMutation {
				add("source_mutation", i, false)
			}
		}
	}
	for i, hook := range resolved.Hooks {
		if hook.Transition == transition {
			add("hook_"+string(hook.Timing), i, hook.When != nil || hook.Timing == HookAfter)
		}
	}
	if transition != TransitionRemove {
		for i := range resolved.Ensures {
			add("ensure", i, true)
		}
		for i, operation := range resolved.Operations {
			if operation.Effect == EffectMutation {
				add("operation", i, false)
			}
		}
	}
	// InstallResolved and Remove are opaque adapter actions even when the
	// resolved plan contains only read-only operations or no operations.
	// Compensations and external post-install actions are likewise not
	// implied to be exhaustive by the plan's declarative operations.
	add("primary_"+string(transition), 0, false)
	return envelope, nil
}
