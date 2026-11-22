package state

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

// The reference model below is an intentionally naive re-derivation of the
// documented claim semantics: linear scans over every prior claim, no index,
// no sorting, no shared helpers with the production code. It exists to catch
// an index that is fast and self-consistent but agrees with the wrong rules.

type referenceClaim struct {
	schema  string
	tool    string
	target  string
	members map[string]struct{}
}

func referenceKey(resource ClaimResource) string {
	return resource.Scope + "\x00" + resource.Kind + "\x00" + resource.Identity
}

func referenceSet(resources []ClaimResource) (map[string]struct{}, bool) {
	members := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		if resource.Scope == "" || resource.Kind == "" || resource.Identity == "" {
			return nil, false
		}
		key := referenceKey(resource)
		if _, duplicate := members[key]; duplicate {
			return nil, false
		}
		members[key] = struct{}{}
	}
	return members, true
}

func referenceOverlaps(a, b map[string]struct{}) bool {
	for key := range a {
		if _, shared := b[key]; shared {
			return true
		}
	}
	return false
}

func referencePlan(existing []InstallationClaim, legacy []ClaimResource, proposed InstallationClaim) (ClaimDisposition, error) {
	if proposed.Schema == "" || proposed.Tool == "" || proposed.Target == "" || len(proposed.Resources) == 0 {
		return "", errors.New("incomplete proposal")
	}
	wanted, ok := referenceSet(proposed.Resources)
	if !ok {
		return "", errors.New("invalid proposed resources")
	}

	// Legacy keys are a set for this check: the implementation deduplicates
	// them, so a repeated key must not turn an unknown-ownership conflict into
	// a validation error.
	unassigned := make(map[string]struct{}, len(legacy))
	for _, resource := range legacy {
		if resource.Scope == "" || resource.Kind == "" || resource.Identity == "" {
			return "", errors.New("incomplete legacy key")
		}
		unassigned[referenceKey(resource)] = struct{}{}
	}
	if referenceOverlaps(wanted, unassigned) {
		return "", fmt.Errorf("%w: unassigned legacy resource", ErrClaimConflict)
	}

	claims := make([]referenceClaim, 0, len(existing))
	for _, claim := range existing {
		members, ok := referenceSet(claim.Resources)
		if !ok || claim.Schema == "" || claim.Tool == "" || claim.Target == "" || len(claim.Resources) == 0 {
			return "", errors.New("invalid existing claim")
		}
		claims = append(claims, referenceClaim{schema: claim.Schema, tool: claim.Tool, target: claim.Target, members: members})
	}

	previous := -1
	for i, claim := range claims {
		if claim.schema == proposed.Schema && claim.tool == proposed.Tool {
			if previous >= 0 {
				return "", errors.New("duplicate logical claim")
			}
			previous = i
		}
	}

	shared := false
	for i, claim := range claims {
		if i == previous || !referenceOverlaps(wanted, claim.members) {
			continue
		}
		// Overlap under an equal target is shareable only when the complete
		// observed sets match; a superset or a different same-size set is a
		// conflicting effect, not a share.
		if claim.target != proposed.Target || !sameMembers(claim.members, wanted) {
			return "", fmt.Errorf("%w: resource already claimed", ErrClaimConflict)
		}
		shared = true
	}

	if previous < 0 {
		if shared {
			return ClaimShare, nil
		}
		return ClaimCreate, nil
	}

	prior := claims[previous]
	if proposed.Target == prior.target && sameMembers(prior.members, wanted) {
		return ClaimUnchanged, nil
	}
	for i, claim := range claims {
		if i == previous {
			continue
		}
		if referenceOverlaps(prior.members, claim.members) {
			return "", fmt.Errorf("%w: cannot transition a shared resource", ErrClaimConflict)
		}
	}
	return ClaimTransition, nil
}

func sameMembers(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func randomResourceSubset(rng *rand.Rand, universe []ClaimResource, used map[string]struct{}, want int) []ClaimResource {
	chosen := make([]ClaimResource, 0, want)
	for _, index := range rng.Perm(len(universe)) {
		if len(chosen) == want {
			break
		}
		resource := universe[index]
		if _, taken := used[referenceKey(resource)]; taken {
			continue
		}
		chosen = append(chosen, resource)
	}
	return chosen
}

// TestClaimLedgerMatchesReferenceModel is a deterministic differential test:
// random but reproducible host views are judged both by the indexed ledger and
// by the naive model above. Any disagreement means the index encodes a rule
// the documentation does not.
func TestClaimLedgerMatchesReferenceModel(t *testing.T) {
	rng := rand.New(rand.NewSource(20261119)) // #nosec G404 -- fixed-seed test corpus; reproducibility matters, secrecy does not.
	var universe []ClaimResource
	for _, scope := range []string{"system", "user:1000"} {
		for _, kind := range []string{"package", "path"} {
			for _, identity := range []string{"a", "b", "c", "d", "e"} {
				universe = append(universe, ClaimResource{Scope: scope, Kind: kind, Identity: identity})
			}
		}
	}
	targets := []string{"sha256:t1", "sha256:t2", "sha256:t3"}

	accepted, rejected, decisions := 0, 0, 0
	for round := 0; round < 300; round++ {
		used := map[string]struct{}{}
		claims := make([]InstallationClaim, 0, 5)
		for i, count := 0, 1+rng.Intn(4); i < count; i++ {
			resources := randomResourceSubset(rng, universe, used, 1+rng.Intn(3))
			if len(resources) == 0 {
				continue
			}
			for _, resource := range resources {
				used[referenceKey(resource)] = struct{}{}
			}
			claim := InstallationClaim{
				Schema:    fmt.Sprintf("schema-%d", rng.Intn(2)),
				Tool:      fmt.Sprintf("tool-%d", i),
				Target:    targets[rng.Intn(len(targets))],
				Resources: resources,
			}
			claims = append(claims, claim)
			if rng.Intn(4) == 0 {
				// A second logical claim over an identical view must be
				// shareable, which exercises the share/unchanged paths.
				clone := claim
				clone.Schema = fmt.Sprintf("clone-%d", i)
				claims = append(claims, clone)
			}
		}
		rng.Shuffle(len(claims), func(i, j int) { claims[i], claims[j] = claims[j], claims[i] })

		var legacy []ClaimResource
		if rng.Intn(2) == 0 {
			legacy = []ClaimResource{universe[rng.Intn(len(universe))]}
		}
		ledger, err := LoadClaimLedger(claims, legacy)
		if err != nil {
			rejected++
			continue
		}
		accepted++

		for p := 0; p < 8; p++ {
			proposal := InstallationClaim{
				Schema:    fmt.Sprintf("schema-%d", rng.Intn(3)),
				Tool:      fmt.Sprintf("tool-%d", rng.Intn(5)),
				Target:    targets[rng.Intn(len(targets))],
				Resources: nil,
			}
			switch rng.Intn(4) {
			case 0:
				if len(claims) > 0 {
					base := claims[rng.Intn(len(claims))]
					proposal.Schema, proposal.Tool = base.Schema, base.Tool
					proposal.Resources = append([]ClaimResource(nil), base.Resources...)
					if rng.Intn(2) == 0 {
						proposal.Target = "sha256:retargeted"
					}
				}
			default:
				proposal.Resources = randomResourceSubset(rng, universe, map[string]struct{}{}, rng.Intn(4))
			}

			wantDisposition, wantErr := referencePlan(claims, legacy, proposal)
			gotDisposition, gotErr := ledger.Plan(proposal)
			decisions++
			conflictMismatch := wantErr != nil && errors.Is(gotErr, ErrClaimConflict) != errors.Is(wantErr, ErrClaimConflict)
			if wantDisposition != gotDisposition || (wantErr == nil) != (gotErr == nil) || conflictMismatch {
				t.Fatalf("round %d proposal %d (%+v): ledger = %q, %v; reference = %q, %v\nclaims=%+v legacy=%+v",
					round, p, proposal, gotDisposition, gotErr, wantDisposition, wantErr, claims, legacy)
			}

			// The stateless entry point must agree with the reused index: the
			// index is an optimization, not a second set of rules.
			if statelessDisposition, statelessErr := PlanInstallationClaim(claims, legacy, proposal); statelessDisposition != gotDisposition || (statelessErr == nil) != (gotErr == nil) {
				t.Fatalf("round %d proposal %d: index = %q, %v; stateless = %q, %v", round, p, gotDisposition, gotErr, statelessDisposition, statelessErr)
			}
		}
	}
	if accepted < 200 || decisions < 1500 {
		t.Fatalf("differential coverage too thin: accepted=%d rejected=%d decisions=%d", accepted, rejected, decisions)
	}
	t.Logf("compared %d decisions across %d accepted and %d rejected ledgers", decisions, accepted, rejected)
}
