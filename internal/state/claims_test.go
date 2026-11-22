package state

import (
	"errors"
	"testing"
)

func TestPlanInstallationClaimMultipleSchemas(t *testing.T) {
	path := ClaimResource{Scope: "user:1000", Kind: "path", Identity: "/home/user/.cargo/bin/rustc"}
	other := ClaimResource{Scope: "system", Kind: "path", Identity: "/usr/bin/rustc"}
	rustA := InstallationClaim{Schema: "A/schema.toml", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{path}}
	rustB := InstallationClaim{Schema: "B/schema.toml", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{path}}
	rustNew := InstallationClaim{Schema: "B/schema.toml", Tool: "rust", Target: "rustup:1.81", Resources: []ClaimResource{path}}
	rustup := InstallationClaim{Schema: "B/schema.toml", Tool: "rustup", Target: "pacman:rustup-1.80", Resources: []ClaimResource{path}}
	independent := InstallationClaim{Schema: "B/schema.toml", Tool: "rustup", Target: "pacman:rustup-1.80", Resources: []ClaimResource{other}}
	cases := []struct {
		name     string
		existing []InstallationClaim
		legacy   []ClaimResource
		wanted   InstallationClaim
		want     ClaimDisposition
		conflict bool
	}{
		{"first registration", nil, nil, rustA, ClaimCreate, false},
		{"idempotent", []InstallationClaim{rustA}, nil, rustA, ClaimUnchanged, false},
		{"same installed identity across schemas", []InstallationClaim{rustA}, nil, rustB, ClaimShare, false},
		{"conflicting versions share physical target", []InstallationClaim{rustA}, nil, rustNew, "", true},
		{"rust and rustup share physical target", []InstallationClaim{rustA}, nil, rustup, "", true},
		{"same names but distinct physical targets", []InstallationClaim{rustA}, nil, independent, ClaimCreate, false},
		{"legacy owner unknown", nil, []ClaimResource{path}, rustB, "", true},
		{"legacy owner still unknown on repeated claim", []InstallationClaim{rustA}, []ClaimResource{path}, rustA, "", true},
		{"replace when single claimant", []InstallationClaim{rustA}, nil, InstallationClaim{Schema: rustA.Schema, Tool: rustA.Tool, Target: "rustup:1.81", Resources: []ClaimResource{path}}, ClaimTransition, false},
		{"replace when another schema also claims resource", []InstallationClaim{rustA, rustB}, nil, InstallationClaim{Schema: rustA.Schema, Tool: rustA.Tool, Target: "rustup:1.81", Resources: []ClaimResource{path}}, "", true},
		{"replace when other project claims old resource only", []InstallationClaim{rustA, rustB}, nil, InstallationClaim{Schema: rustA.Schema, Tool: rustA.Tool, Target: "pacman:1.81", Resources: []ClaimResource{other}}, "", true},
		{"reject partially overlapping same target", []InstallationClaim{rustA}, nil, InstallationClaim{Schema: rustB.Schema, Tool: rustB.Tool, Target: rustA.Target, Resources: []ClaimResource{path, other}}, "", true},
		{"same path in separate filesystem namespace", []InstallationClaim{rustA}, nil, InstallationClaim{Schema: rustB.Schema, Tool: rustB.Tool, Target: rustNew.Target, Resources: []ClaimResource{{Scope: "container:isolated", Kind: "path", Identity: path.Identity}}}, ClaimCreate, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlanInstallationClaim(tc.existing, tc.legacy, tc.wanted)
			if tc.conflict {
				if !errors.Is(err, ErrClaimConflict) {
					t.Fatalf("error = %v, want claim conflict", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("result = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestPlanInstallationClaimInvalidEvidence(t *testing.T) {
	valid := InstallationClaim{Schema: "A", Tool: "rust", Target: "sha256:foo", Resources: []ClaimResource{{Scope: "system", Kind: "package", Identity: "pacman:rust"}}}
	for _, tc := range []struct {
		name   string
		modify func(*InstallationClaim)
	}{
		{"schema", func(c *InstallationClaim) { c.Schema = "" }},
		{"target", func(c *InstallationClaim) { c.Target = "" }},
		{"unknown resources", func(c *InstallationClaim) { c.Resources = nil }},
		{"incomplete key", func(c *InstallationClaim) { c.Resources[0].Scope = "" }},
		{"duplicate resources", func(c *InstallationClaim) { c.Resources = append(c.Resources, c.Resources[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			candidate.Resources = append([]ClaimResource(nil), valid.Resources...)
			tc.modify(&candidate)
			if _, err := PlanInstallationClaim(nil, nil, candidate); err == nil {
				t.Fatal("accepted incomplete or duplicate evidence")
			}
		})
	}
}

func BenchmarkPlanInstallationClaim(b *testing.B) {
	res := ClaimResource{Scope: "user:1000", Kind: "path", Identity: "/home/user/.cargo/bin/rustc"}
	for _, n := range []int{1, 10, 100} {
		b.Run(map[int]string{1: "1-schema", 10: "10-schemas", 100: "100-schemas"}[n], func(b *testing.B) {
			claims := make([]InstallationClaim, n)
			for i := range claims {
				claims[i] = InstallationClaim{Schema: string(rune('a' + i)), Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{res}}
			}
			wanted := InstallationClaim{Schema: "new-schema", Tool: "rust", Target: "rustup:1.80", Resources: []ClaimResource{res}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := PlanInstallationClaim(claims, nil, wanted)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A persisted claims ledger may be damaged or contain mutually incompatible
// imports. Unrelated new requests must not be accepted against that ledger:
// a future executor must fix/recover it first, not infer permission to mutate.
func TestPlanInstallationClaimRejectsInconsistentExistingLedger(t *testing.T) {
	shared := ClaimResource{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/rustc"}
	other := ClaimResource{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/go"}
	unrelated := ClaimResource{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/cargo"}
	a := InstallationClaim{Schema: "A", Tool: "rust", Target: "sha256:a", Resources: []ClaimResource{shared}}
	b := InstallationClaim{Schema: "B", Tool: "rustup", Target: "sha256:b", Resources: []ClaimResource{shared}}
	c := InstallationClaim{Schema: "C", Tool: "go", Target: "sha256:c", Resources: []ClaimResource{other}}
	for _, tc := range []struct {
		name     string
		existing []InstallationClaim
		legacy   []ClaimResource
		conflict bool
	}{
		{"different target on shared slot", []InstallationClaim{a, b}, nil, true},
		{"duplicate logical claim", []InstallationClaim{a, a}, nil, false},
		{"partial overlap", []InstallationClaim{a, {Schema: "B", Tool: "rustup", Target: a.Target, Resources: []ClaimResource{shared, unrelated}}}, nil, true},
		{"compatible sharing", []InstallationClaim{a, {Schema: "B", Tool: "rustup", Target: a.Target, Resources: []ClaimResource{shared}}}, nil, false},
		{"existing claim overlaps legacy unassigned", []InstallationClaim{a}, []ClaimResource{shared}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlanInstallationClaim(tc.existing, tc.legacy, c)
			if tc.conflict {
				if !errors.Is(err, ErrClaimConflict) {
					t.Fatalf("result = %s, %v; want global claim conflict", got, err)
				}
			} else if tc.name == "duplicate logical claim" {
				if err == nil {
					t.Fatalf("duplicate ledger accepted: %s", got)
				}
			} else if err != nil || got != ClaimCreate {
				t.Fatalf("result = %s, %v; want create", got, err)
			}
		})
	}
}
