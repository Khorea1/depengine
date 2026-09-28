package app

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/state"
)

func TestDisambiguatedWhyNamesKeepsUniqueLabels(t *testing.T) {
	attempts := []exec.MethodAttempt{
		{Kind: "http", Label: "primary", Candidate: 0, CandidateKnown: true},
		{Kind: "git", Label: "", Candidate: 1, CandidateKnown: true},
	}
	got := disambiguatedWhyNames(attempts)
	want := []string{"primary (http)", "git"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("disambiguatedWhyNames = %q, want %q", got, want)
	}
}

func TestDisambiguatedWhyNamesSuffixesCollidingKinds(t *testing.T) {
	attempts := []exec.MethodAttempt{
		{Kind: "http", Label: "", Candidate: 0, CandidateKnown: true},
		{Kind: "http", Label: "", Candidate: 1, CandidateKnown: true},
	}
	got := disambiguatedWhyNames(attempts)
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("colliding candidates must render distinctly, got %q", got)
	}
	if !strings.Contains(got[0], "#0") || !strings.Contains(got[1], "#1") {
		t.Fatalf("colliding candidates must carry ordinals, got %q", got)
	}
}

func TestAmbiguousWhyWarningFlagsUnlabeledDuplicates(t *testing.T) {
	attempts := []exec.MethodAttempt{
		{Kind: "http", Label: "", Candidate: 0, CandidateKnown: true},
		{Kind: "http", Label: "", Candidate: 1, CandidateKnown: true},
	}
	warning := ambiguousWhyWarning("demo", attempts)
	if !strings.Contains(warning, "ambiguous") {
		t.Fatalf("warning = %q, want ambiguity guidance", warning)
	}
	if !strings.Contains(warning, "#0") || !strings.Contains(warning, "#1") {
		t.Fatalf("warning = %q, want candidate ordinals", warning)
	}
}

func TestAmbiguousWhyWarningEmptyForUniqueKinds(t *testing.T) {
	attempts := []exec.MethodAttempt{
		{Kind: "http", Label: "", Candidate: 0, CandidateKnown: true},
		{Kind: "git", Label: "", Candidate: 1, CandidateKnown: true},
	}
	if warning := ambiguousWhyWarning("demo", attempts); warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
}

func TestFindStateMethodCandidateAmbiguousListsCandidates(t *testing.T) {
	tool := &config.Tool{Methods: []*config.MethodCandidate{
		{Kind: "http", Label: "primary"},
		{Kind: "http", Label: "mirror"},
	}}
	_, err := findStateMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"})
	if err == nil {
		t.Fatal("expected ambiguous error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous") {
		t.Fatalf("error = %q, want ambiguous", msg)
	}
	if !strings.Contains(msg, `"primary"`) || !strings.Contains(msg, `"mirror"`) {
		t.Fatalf("error = %q, want candidate labels", msg)
	}
	if !strings.Contains(msg, "#0") || !strings.Contains(msg, "#1") {
		t.Fatalf("error = %q, want candidate ordinals", msg)
	}
}


func TestAmbiguousWhyWarningEmptyForUniqueLabelsWithinKind(t *testing.T) {
	attempts := []exec.MethodAttempt{
		{Kind: "http", Label: "primary", Candidate: 0, CandidateKnown: true},
		{Kind: "http", Label: "mirror", Candidate: 1, CandidateKnown: true},
	}
	if warning := ambiguousWhyWarning("demo", attempts); warning != "" {
		t.Fatalf("warning = %q, want empty for uniquely labeled candidates", warning)
	}
}

func TestAmbiguousWhyWarningFlagsDuplicateLabels(t *testing.T) {
	attempts := []exec.MethodAttempt{
		{Kind: "http", Label: "mirror", Candidate: 0, CandidateKnown: true},
		{Kind: "http", Label: "mirror", Candidate: 1, CandidateKnown: true},
	}
	warning := ambiguousWhyWarning("demo", attempts)
	if !strings.Contains(warning, "duplicate label") || !strings.Contains(warning, `"mirror"`) {
		t.Fatalf("warning = %q, want duplicate-label guidance", warning)
	}
}
