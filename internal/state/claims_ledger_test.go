package state

import (
	"errors"
	"sync"
	"testing"
)

func permutationsOf[T any](items []T) [][]T {
	if len(items) <= 1 {
		return [][]T{append([]T(nil), items...)}
	}
	var out [][]T
	for i := range items {
		rest := make([]T, 0, len(items)-1)
		rest = append(rest, items[:i]...)
		rest = append(rest, items[i+1:]...)
		for _, tail := range permutationsOf(rest) {
			out = append(out, append([]T{items[i]}, tail...))
		}
	}
	return out
}

// Equivalent host views must produce identical decisions regardless of the
// order in which claims and resource keys are listed. Stable ordering is part
// of the diagnostic contract, not an implementation detail: a permuted ledger
// must not change which claim is reported as the conflict.
func TestClaimLedgerDecisionsAreOrderInvariant(t *testing.T) {
	slot := ClaimResource{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/rustc"}
	other := ClaimResource{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/cargo"}
	godir := ClaimResource{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/go"}
	rust := InstallationClaim{Schema: "A/schema.toml", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{slot, other}}
	rustClone := InstallationClaim{Schema: "B/schema.toml", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{other, slot}}
	goTool := InstallationClaim{Schema: "C/schema.toml", Tool: "go", Target: "go:1.23", Resources: []ClaimResource{godir}}
	ledgerClaims := []InstallationClaim{rust, rustClone, goTool}
	proposals := []InstallationClaim{
		{Schema: "D/schema.toml", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{slot, other}},
		{Schema: "D/schema.toml", Tool: "cargo", Target: "cargo:1.80", Resources: []ClaimResource{other}},
		{Schema: "A/schema.toml", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{other, slot}},
		{Schema: "A/schema.toml", Tool: "rust", Target: "rustup:1.81", Resources: []ClaimResource{other, slot}},
		{Schema: "D/schema.toml", Tool: "go", Target: "go:1.23", Resources: []ClaimResource{godir, slot}},
		{Schema: "C/schema.toml", Tool: "go", Target: "go:1.24", Resources: []ClaimResource{godir}},
	}
	want := []ClaimDisposition{ClaimShare, "", ClaimUnchanged, "", "", ClaimTransition}
	wantConflict := []bool{false, true, false, true, true, false}

	for _, permuted := range permutationsOf(ledgerClaims) {
		ledger, err := LoadClaimLedger(permuted, nil)
		if err != nil {
			t.Fatalf("equivalent ledger rejected: %v", err)
		}
		for i, proposal := range proposals {
			got, err := ledger.Plan(proposal)
			if wantConflict[i] {
				if !errors.Is(err, ErrClaimConflict) {
					t.Fatalf("ledger order %v: proposal %d = %q, %v; want conflict", permuted, i, got, err)
				}
				continue
			}
			if err != nil || got != want[i] {
				t.Fatalf("ledger order %v: proposal %d = %q, %v; want %q", permuted, i, got, err, want[i])
			}
		}
	}
}

func TestClaimLedgerDiagnosticsAreOrderInvariant(t *testing.T) {
	shared := ClaimResource{Scope: "system", Kind: "package", Identity: "pacman:rust"}
	other := ClaimResource{Scope: "system", Kind: "package", Identity: "pacman:go"}
	a := InstallationClaim{Schema: "A", Tool: "rust", Target: "sha256:a", Resources: []ClaimResource{shared}}
	b := InstallationClaim{Schema: "B", Tool: "rustup", Target: "sha256:b", Resources: []ClaimResource{shared}}
	c := InstallationClaim{Schema: "C", Tool: "go", Target: "sha256:c", Resources: []ClaimResource{other}}
	broken := InstallationClaim{Schema: "D", Tool: "", Target: "sha256:d", Resources: []ClaimResource{other}}

	check := func(name, want string, claims []InstallationClaim, legacy []ClaimResource) {
		t.Helper()
		for _, permuted := range permutationsOf(claims) {
			_, err := LoadClaimLedger(permuted, legacy)
			if err == nil {
				t.Fatalf("%s: ledger accepted in order %v", name, permuted)
			}
			if err.Error() != want {
				t.Fatalf("%s: error = %q in order %v, want %q", name, err, permuted, want)
			}
		}
	}

	check("shared slot, different target", `incompatible global resource claim: existing claims "A"/"rust" and "B"/"rustup" disagree on {Scope:system Kind:package Identity:pacman:rust}`, []InstallationClaim{a, b}, nil)
	check("duplicate logical claim", `duplicate claim for "A"/"rust"`, []InstallationClaim{a, a}, nil)
	check("claim overlaps legacy", `incompatible global resource claim: existing claim "A"/"rust" overlaps legacy-unassigned resource {Scope:system Kind:package Identity:pacman:rust}`, []InstallationClaim{a, c}, []ClaimResource{shared})
	check("invalid claim", `invalid existing claim: claim requires schema, tool, concrete target and observed resources`, []InstallationClaim{a, broken}, nil)
}

// A ledger validates once and can answer many requests. Rejected proposals must
// not leave partial state behind, and the decisions must not depend on how many
// requests were evaluated before them.
func TestClaimLedgerIsReusable(t *testing.T) {
	slot := ClaimResource{Scope: "user:1000", Kind: "path", Identity: "/home/user/.cargo/bin/rustc"}
	existing := []InstallationClaim{{Schema: "A", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{slot}}}
	legacy := []ClaimResource{{Scope: "user:1000", Kind: "path", Identity: "/home/user/.local/bin/uv"}}

	ledger, err := LoadClaimLedger(existing, legacy)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		proposal InstallationClaim
		want     ClaimDisposition
		conflict bool
	}{
		{InstallationClaim{Schema: "B", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{slot}}, ClaimShare, false},
		{InstallationClaim{Schema: "B", Tool: "rust", Target: "rustup:1.81", Resources: []ClaimResource{slot}}, "", true},
		{InstallationClaim{Schema: "C", Tool: "uv", Target: "uv:0.4", Resources: []ClaimResource{legacy[0]}}, "", true},
		{InstallationClaim{Schema: "A", Tool: "rust", Target: "rustup:1.81", Resources: []ClaimResource{slot}}, ClaimTransition, false},
	}
	for round := 0; round < 3; round++ {
		for i, step := range steps {
			got, err := ledger.Plan(step.proposal)
			if step.conflict {
				if !errors.Is(err, ErrClaimConflict) {
					t.Fatalf("round %d step %d = %q, %v; want conflict", round, i, got, err)
				}
				continue
			}
			if err != nil || got != step.want {
				t.Fatalf("round %d step %d = %q, %v; want %q", round, i, got, err, step.want)
			}
		}
	}
}

// The index must not observe later edits to the caller's slices: an index that
// silently disagrees with its own claims would authorize the wrong decision.
func TestClaimLedgerOwnsItsInput(t *testing.T) {
	slot := ClaimResource{Scope: "system", Kind: "package", Identity: "pacman:rust"}
	existing := []InstallationClaim{{Schema: "A", Tool: "rust", Target: "sha256:a", Resources: []ClaimResource{slot}}}
	legacy := []ClaimResource{{Scope: "system", Kind: "package", Identity: "pacman:go"}}
	ledger, err := LoadClaimLedger(existing, legacy)
	if err != nil {
		t.Fatal(err)
	}

	empty := InstallationClaim{Schema: "A", Tool: "rust", Target: "sha256:a", Resources: []ClaimResource{slot}}
	goKey := legacy[0]
	extra := ClaimResource{Scope: "system", Kind: "package", Identity: "pacman:extra"}
	existing[0].Target = "sha256:mutated"
	existing[0].Resources[0] = goKey
	legacy[0] = extra

	if got, err := ledger.Plan(empty); err != nil || got != ClaimUnchanged {
		t.Fatalf("plan after caller mutation = %q, %v; want unchanged", got, err)
	}
	if _, err := ledger.Plan(InstallationClaim{Schema: "B", Tool: "go", Target: "sha256:b", Resources: []ClaimResource{goKey}}); !errors.Is(err, ErrClaimConflict) {
		t.Fatalf("legacy view lost after caller mutation: %v", err)
	}
	if got, err := ledger.Plan(InstallationClaim{Schema: "B", Tool: "extra", Target: "sha256:c", Resources: []ClaimResource{extra}}); err != nil || got != ClaimCreate {
		t.Fatalf("ledger adopted caller's replacement legacy key: %q, %v", got, err)
	}
}

// ClaimLedger is documented as read-only after construction, so concurrent
// callers may share one index. This test exists to keep that promise honest
// under the race detector: any hidden scratch buffer or lazily built map in
// Plan would surface here.
func TestClaimLedgerSharedAcrossConcurrentPlans(t *testing.T) {
	slot := ClaimResource{Scope: "system", Kind: "package", Identity: "pacman:rust"}
	other := ClaimResource{Scope: "system", Kind: "package", Identity: "pacman:cargo"}
	ledger, err := LoadClaimLedger([]InstallationClaim{
		{Schema: "A", Tool: "rust", Target: "sha256:a", Resources: []ClaimResource{slot}},
		{Schema: "B", Tool: "cargo", Target: "sha256:b", Resources: []ClaimResource{other}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	proposals := []InstallationClaim{
		{Schema: "A", Tool: "rust", Target: "sha256:a", Resources: []ClaimResource{slot}},
		{Schema: "C", Tool: "shared", Target: "sha256:a", Resources: []ClaimResource{slot}},
		{Schema: "B", Tool: "cargo", Target: "sha256:c", Resources: []ClaimResource{other}},
		{Schema: "D", Tool: "new", Target: "sha256:d", Resources: []ClaimResource{{Scope: "system", Kind: "path", Identity: "/usr/bin/demo"}}},
	}
	// The cargo claim owns its resource alone, so retargeting it is a plain
	// transition; only the rust slot is shared and blocks the other owner.
	want := []ClaimDisposition{ClaimUnchanged, ClaimShare, ClaimTransition, ClaimCreate}

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < 50; round++ {
				for i, proposal := range proposals {
					got, err := ledger.Plan(proposal)
					if want[i] == "" {
						if !errors.Is(err, ErrClaimConflict) {
							t.Errorf("worker %d round %d proposal %d = %q, %v; want conflict", worker, round, i, got, err)
							return
						}
						continue
					}
					if err != nil || got != want[i] {
						t.Errorf("worker %d round %d proposal %d = %q, %v; want %q", worker, round, i, got, err, want[i])
						return
					}
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestLoadClaimLedgerRejectsIncompleteLegacy(t *testing.T) {
	for _, key := range []ClaimResource{
		{Kind: "path", Identity: "/usr/bin/rustc"},
		{Scope: "system", Identity: "/usr/bin/rustc"},
		{Scope: "system", Kind: "path"},
	} {
		if _, err := LoadClaimLedger(nil, []ClaimResource{key}); err == nil {
			t.Fatalf("incomplete legacy key %+v accepted", key)
		}
	}
}

func BenchmarkClaimLedgerPlan(b *testing.B) {
	slot := ClaimResource{Scope: "user:1000", Kind: "path", Identity: "/home/user/.cargo/bin/rustc"}
	for _, n := range []int{1, 10, 100} {
		b.Run(map[int]string{1: "1-schema", 10: "10-schemas", 100: "100-schemas"}[n], func(b *testing.B) {
			claims := make([]InstallationClaim, n)
			for i := range claims {
				claims[i] = InstallationClaim{Schema: string(rune('a' + i)), Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{slot}}
			}
			ledger, err := LoadClaimLedger(claims, nil)
			if err != nil {
				b.Fatal(err)
			}
			wanted := InstallationClaim{Schema: "new-schema", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{slot}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ledger.Plan(wanted); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
