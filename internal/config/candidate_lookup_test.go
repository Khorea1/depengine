package config

import "testing"

func TestFindMethodCandidateUsesExactIdentityAndFailsAmbiguousKinds(t *testing.T) {
	primary := &MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &Tool{Name: "tool", Methods: []*MethodCandidate{primary, mirror}}

	got, err := FindMethodCandidate(tool, "http", "mirror")
	if err != nil || got != mirror {
		t.Fatalf("FindMethodCandidate(label=mirror) = %p, %v; want %p", got, err, mirror)
	}
	if _, err := FindMethodCandidate(tool, "http", ""); err == nil {
		t.Fatal("FindMethodCandidate(kind-only) succeeded with duplicate candidates")
	}
	if _, err := FindMethodCandidate(tool, "http", "missing"); err == nil {
		t.Fatal("FindMethodCandidate succeeded for missing label")
	}

	unique := &Tool{Name: "unique", Methods: []*MethodCandidate{{Kind: "npm"}}}
	got, err = FindMethodCandidate(unique, "npm", "")
	if err != nil || got != unique.Methods[0] {
		t.Fatalf("FindMethodCandidate(unique kind) = %p, %v; want %p", got, err, unique.Methods[0])
	}
}
