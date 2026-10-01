// Package formatversion centralizes version policies for document families that
// currently have an exact, single-version pre-freeze reader contract. The
// depengine.lock envelope is intentionally not modeled here: internal/lock
// accepts both envelope v1 and v2 during the explicit universal-lock migration.
// None of these numbers is the depengine binary version.
package formatversion

import "fmt"

// Kind identifies a versioned depengine document family.
type Kind string

const (
	Manifest       Kind = "manifest"
	LockProjection Kind = "lock projection"
	State          Kind = "state"
)

// Stability describes whether a public compatibility promise has been made
// for a document family. PreFreeze means the current format is intentionally
// breakable while the v1 semantic freeze gate remains open.
type Stability string

const (
	PreFreeze Stability = "pre_freeze"
	Frozen    Stability = "frozen"
)

const (
	CurrentManifestVersion       = 1
	CurrentLockProjectionVersion = 1
	// State v5 adds durable replacement transactions around destructive upgrades.
	// State v4 introduced root intent with prerequisite ownership/refcounts; v3
	// introduced exact preparation plans beside active WAL journals. Older files
	// are rejected rather than guessed or silently migrated.
	CurrentStateVersion = 5
)

// Policy is the compatibility contract for one document family.
//
// Version is a format/grammar major, not a product release. During pre-freeze
// development, readers accept only Current. Once a family is frozen, breaking
// changes require a new version and an explicit reader/migration decision; a
// writer must never silently reinterpret an unknown version.
type Policy struct {
	Kind      Kind
	Current   int
	Stability Stability
}

// PolicyFor returns the current compatibility policy for a document family.
func PolicyFor(kind Kind) (Policy, error) {
	switch kind {
	case Manifest:
		return Policy{Kind: Manifest, Current: CurrentManifestVersion, Stability: PreFreeze}, nil
	case LockProjection:
		return Policy{Kind: LockProjection, Current: CurrentLockProjectionVersion, Stability: PreFreeze}, nil
	case State:
		return Policy{Kind: State, Current: CurrentStateVersion, Stability: PreFreeze}, nil
	default:
		return Policy{}, fmt.Errorf("unknown format kind %q", kind)
	}
}

// ValidateReadVersion applies the current fail-closed reader policy. It is
// deliberately exact while the formats remain pre-freeze: there is no legacy
// parser dispatch or implicit migration yet.
func ValidateReadVersion(kind Kind, version int) error {
	policy, err := PolicyFor(kind)
	if err != nil {
		return err
	}
	if version != policy.Current {
		return fmt.Errorf("unsupported %s format version %d (supported: %d)", kind, version, policy.Current)
	}
	return nil
}
