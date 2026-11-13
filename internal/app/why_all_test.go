package app

import (
	"strings"
	"testing"
)

func TestWhyAllSummaryReportsRejectedCandidates(t *testing.T) {
	report := &whyReport{
		Tool: "nvim",
		Candidates: []whyCandidate{
			{Kind: "native", Candidate: 0, Status: "skip_when", ReasonCode: "when_not_matched"},
			{Kind: "github", Candidate: 1, Status: "skip_unavailable", ReasonCode: "adapter_unavailable"},
			{Kind: "cargo", Candidate: 2, Status: "would_install"},
		},
		Selected: &whySelectedMethod{
			Kind: "cargo", Candidate: 2, Status: "would_install",
			Reason: "first candidate that would install",
		},
	}
	selected, reason := whyAllSummary(report)
	if selected != "cargo #2" {
		t.Fatalf("selected = %q, want cargo #2", selected)
	}
	if !strings.Contains(reason, "2 earlier candidate(s) rejected") {
		t.Fatalf("reason = %q, want the rejection count", reason)
	}
}

func TestWhyAllSummaryReportsFirstFailureCode(t *testing.T) {
	report := &whyReport{
		Tool: "nvim",
		Candidates: []whyCandidate{
			{Kind: "native", Candidate: 0, Status: "skip_no_distribution", ReasonCode: "no_distribution"},
		},
	}
	selected, reason := whyAllSummary(report)
	if selected != "none" {
		t.Fatalf("selected = %q, want none", selected)
	}
	if !strings.Contains(reason, "no_distribution") {
		t.Fatalf("reason = %q, want the reason code", reason)
	}
}

func TestWhyAllArgumentContract(t *testing.T) {
	cmd := newWhyCmd()
	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, []string{"nvim"}); err == nil {
		t.Fatal("--all with a tool argument should fail")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Fatalf("--all without a tool argument: %v", err)
	}

	if err := cmd.Flags().Set("all", "false"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Fatal("a tool argument should be required without --all")
	}
	if err := cmd.Args(cmd, []string{"nvim"}); err != nil {
		t.Fatalf("single tool argument: %v", err)
	}
}
