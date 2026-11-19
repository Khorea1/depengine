package state

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ClaimResource identifies an observed physical resource or an explicitly
// modeled exclusive capability. A logical tool name is not a resource key.
// Callers must normalize the scope and identity (including paths) and obtain
// them from trustworthy adapter observations before using this model.
type ClaimResource struct {
	Scope    string
	Kind     string
	Identity string
}

// InstallationClaim describes a schema's desired use of an observed resource.
// Target is a concrete resolved installation fingerprint, not a version range,
// a schema hash, or proof that depengine installed the resource.
type InstallationClaim struct {
	Schema    string
	Tool      string
	Target    string
	Resources []ClaimResource
}

// ClaimDisposition describes a possible state change. A disposition is NOT a
// permission to mutate the host: a caller must still verify installation
// ownership, current observations, and the appropriate transaction locks.
type ClaimDisposition string

const (
	ClaimCreate     ClaimDisposition = "create"
	ClaimUnchanged  ClaimDisposition = "unchanged"
	ClaimShare      ClaimDisposition = "share"
	ClaimTransition ClaimDisposition = "transition"
)

var ErrClaimConflict = errors.New("incompatible global resource claim")

const (
	claimIncomplete  = "claim requires schema, tool, concrete target and observed resources"
	legacyIncomplete = "incomplete legacy resource key"
)

// compareResource orders resource keys so diagnostics and overlap scans never
// depend on map iteration order or on the order a caller listed its resources.
func compareResource(a, b ClaimResource) int {
	if c := strings.Compare(a.Scope, b.Scope); c != 0 {
		return c
	}
	if c := strings.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return strings.Compare(a.Identity, b.Identity)
}

// validateClaim returns the claim's resource keys sorted and deduplicated.
// Duplicates are rejected rather than collapsed: repeating a key is not
// evidence of two distinct effects.
func validateClaim(c InstallationClaim) ([]ClaimResource, error) {
	if c.Schema == "" || c.Tool == "" || c.Target == "" || len(c.Resources) == 0 {
		return nil, errors.New(claimIncomplete)
	}
	resources := make([]ClaimResource, len(c.Resources))
	copy(resources, c.Resources)
	for _, resource := range resources {
		if resource.Scope == "" || resource.Kind == "" || resource.Identity == "" {
			return nil, fmt.Errorf("claim %q/%q has an incomplete resource key", c.Schema, c.Tool)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return compareResource(resources[i], resources[j]) < 0 })
	for i := 1; i < len(resources); i++ {
		if resources[i] == resources[i-1] {
			return nil, fmt.Errorf("claim %q/%q duplicates resource %+v", c.Schema, c.Tool, resources[i])
		}
	}
	return resources, nil
}

func sortResources(resources []ClaimResource) {
	sort.Slice(resources, func(i, j int) bool { return compareResource(resources[i], resources[j]) < 0 })
}

// resourceOverlap reports the lexicographically first key present in both
// sorted sets. Both operands must be sorted and duplicate-free.
func resourceOverlap(a, b []ClaimResource) (ClaimResource, bool) {
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch c := compareResource(a[i], b[j]); {
		case c == 0:
			return a[i], true
		case c < 0:
			i++
		default:
			j++
		}
	}
	return ClaimResource{}, false
}

func sameResourceSet(a, b []ClaimResource) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func legacyConflict(resource ClaimResource) error {
	return fmt.Errorf("%w: resource %+v has unknown legacy ownership", ErrClaimConflict, resource)
}

type claimKey struct{ schema, tool string }

// ClaimLedger is a validated, reusable index over the claims a host already
// records plus the resources whose owner is unknown. Building it once lets a
// caller evaluate many candidate claims (for example one per fallback method)
// without revalidating every resource on each request.
//
// The ledger is read-only after construction and holds its own copy of the
// input, so later caller mutation cannot desynchronize the index from the
// claims. It is a planning primitive only: it never reads the host, never
// grants ownership, and never authorizes a mutation. State v5 does not persist
// these claims, and installation/removal paths do not invoke it.
type ClaimLedger struct {
	claims  []InstallationClaim
	owners  map[ClaimResource][]int
	logical map[claimKey]int
	legacy  []ClaimResource
}

// LoadClaimLedger validates and indexes the complete known claim set. Whole
// ledger integrity matters: a damaged or mutually incompatible ledger must
// not authorize an unrelated new claim, so it is rejected here rather than
// ignored on the way to an unrelated decision.
//
// Errors are independent of the order in which claims and resources are
// supplied; the lexicographically smallest conflicting pair is reported.
func LoadClaimLedger(existing []InstallationClaim, legacy []ClaimResource) (*ClaimLedger, error) {
	ledger := &ClaimLedger{
		claims:  make([]InstallationClaim, 0, len(existing)),
		owners:  make(map[ClaimResource][]int, len(existing)),
		logical: make(map[claimKey]int, len(existing)),
	}

	legacy = append([]ClaimResource(nil), legacy...)
	for _, key := range legacy {
		if key.Scope == "" || key.Kind == "" || key.Identity == "" {
			return nil, errors.New(legacyIncomplete)
		}
	}
	sortResources(legacy)
	ledger.legacy = dedupeResources(legacy)

	// Canonical claim order keeps diagnostics stable under permutation.
	order := make([]int, len(existing))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := existing[order[i]], existing[order[j]]
		if c := strings.Compare(a.Schema, b.Schema); c != 0 {
			return c < 0
		}
		return strings.Compare(a.Tool, b.Tool) < 0
	})

	for _, source := range order {
		claim := existing[source]
		keys, err := validateClaim(claim)
		if err != nil {
			return nil, fmt.Errorf("invalid existing claim: %w", err)
		}
		logical := claimKey{claim.Schema, claim.Tool}
		if _, duplicate := ledger.logical[logical]; duplicate {
			return nil, fmt.Errorf("duplicate claim for %q/%q", claim.Schema, claim.Tool)
		}

		claim.Resources = keys
		index := len(ledger.claims)
		ledger.claims = append(ledger.claims, claim)
		for _, resource := range keys {
			if prior := ledger.owners[resource]; len(prior) > 0 {
				owner := ledger.claims[prior[0]]
				if owner.Target != claim.Target || !sameResourceSet(owner.Resources, keys) {
					return nil, fmt.Errorf("%w: existing claims %q/%q and %q/%q disagree on %+v", ErrClaimConflict, owner.Schema, owner.Tool, claim.Schema, claim.Tool, resource)
				}
			}
			ledger.owners[resource] = append(ledger.owners[resource], index)
		}
		ledger.logical[logical] = index
	}

	for _, resource := range ledger.legacy {
		if prior := ledger.owners[resource]; len(prior) > 0 {
			owner := ledger.claims[prior[0]]
			return nil, fmt.Errorf("%w: existing claim %q/%q overlaps legacy-unassigned resource %+v", ErrClaimConflict, owner.Schema, owner.Tool, resource)
		}
	}
	return ledger, nil
}

func dedupeResources(sortedKeys []ClaimResource) []ClaimResource {
	unique := sortedKeys[:0]
	for i, key := range sortedKeys {
		if i == 0 || key != sortedKeys[i-1] {
			unique = append(unique, key)
		}
	}
	return unique
}

// overlappingOwners returns the sorted, unique claim indexes holding at least
// one of the given sorted resource keys.
func (l *ClaimLedger) overlappingOwners(keys []ClaimResource) []int {
	var found []int
	for _, resource := range keys {
		found = append(found, l.owners[resource]...)
	}
	if len(found) < 2 {
		return found
	}
	sort.Ints(found)
	unique := found[:1]
	for _, index := range found[1:] {
		if index != unique[len(unique)-1] {
			unique = append(unique, index)
		}
	}
	return unique
}

// Plan checks one proposed claim against this ledger without modifying it.
//
// It deliberately does not infer claims from State.SchemaPath, which only
// records the last writer. Unknown physical resources must be rejected by
// the caller rather than silently assumed disjoint. Equal targets may be
// shared only if the complete observed resource set matches.
func (l *ClaimLedger) Plan(proposed InstallationClaim) (ClaimDisposition, error) {
	wanted, err := validateClaim(proposed)
	if err != nil {
		return "", err
	}
	return l.plan(wanted, proposed)
}

func (l *ClaimLedger) plan(wanted []ClaimResource, proposed InstallationClaim) (ClaimDisposition, error) {
	if resource, collides := resourceOverlap(wanted, l.legacy); collides {
		return "", legacyConflict(resource)
	}

	previous, hasPrevious := l.logical[claimKey{proposed.Schema, proposed.Tool}]
	shared := false
	for _, index := range l.overlappingOwners(wanted) {
		if hasPrevious && index == previous {
			continue
		}
		claim := l.claims[index]
		resource, collides := resourceOverlap(wanted, claim.Resources)
		if !collides {
			continue
		}
		if proposed.Target != claim.Target || !sameResourceSet(wanted, claim.Resources) {
			return "", fmt.Errorf("%w: %q/%q already claims %+v", ErrClaimConflict, claim.Schema, claim.Tool, resource)
		}
		shared = true
	}

	if !hasPrevious {
		if shared {
			return ClaimShare, nil
		}
		return ClaimCreate, nil
	}

	prior := l.claims[previous]
	priorKeys := prior.Resources
	if resource, collides := resourceOverlap(priorKeys, l.legacy); collides {
		return "", legacyConflict(resource)
	}
	if proposed.Target == prior.Target && sameResourceSet(wanted, priorKeys) {
		return ClaimUnchanged, nil
	}
	for _, index := range l.overlappingOwners(priorKeys) {
		if index == previous {
			continue
		}
		claim := l.claims[index]
		resource, collides := resourceOverlap(priorKeys, claim.Resources)
		if !collides {
			continue
		}
		return "", fmt.Errorf("%w: cannot transition shared resource %+v claimed by %q/%q", ErrClaimConflict, resource, claim.Schema, claim.Tool)
	}
	return ClaimTransition, nil
}

// PlanInstallationClaim checks one proposed claim against known claims and
// *unassigned* legacy resources without modifying either collection.
//
// It is the stateless entry point for a single decision. Callers evaluating
// several proposals against the same host view should reuse a ClaimLedger.
func PlanInstallationClaim(existing []InstallationClaim, legacy []ClaimResource, proposed InstallationClaim) (ClaimDisposition, error) {
	wanted, err := validateClaim(proposed)
	if err != nil {
		return "", err
	}
	ledger, err := LoadClaimLedger(existing, legacy)
	if err != nil {
		return "", err
	}
	return ledger.plan(wanted, proposed)
}
