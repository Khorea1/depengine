// Package formatversion centralizes the independent on-disk format versions
// used by depengine. Manifest, lock, and state formats intentionally evolve
// independently; none of these numbers is the depengine binary version.
package formatversion

import "fmt"

// Kind identifies a versioned depengine document family.
type Kind string

const (
	Manifest Kind = "manifest"
	Lock     Kind = "lock"
	State    Kind = "state"
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
	CurrentManifestVersion = 1
	CurrentLockVersion     = 1
	// State v4 records whether a tracked tool has durable root intent. That bit,
	// together with prerequisite ownership/refcounts, is required before remove
	// can safely garbage-collect depengine-created lazy prerequisites. v3 files
	// predate that invariant and are rejected rather than guessed/migrated.
	// State v3 also introduced exact preparation plans beside active WAL journals.
	CurrentStateVersion = 4
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
	case Lock:
		return Policy{Kind: Lock, Current: CurrentLockVersion, Stability: PreFreeze}, nil
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
