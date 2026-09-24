package app

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
)

type unsupportedGraphGuard struct{}

func (unsupportedGraphGuard) String() string { return "unsupported" }

func TestParseGraphView(t *testing.T) {
	tests := []struct {
		value string
		want  graph.GraphView
	}{
		{"declared", graph.DeclaredView},
		{"effective", graph.EffectiveView},
		{"resolved", graph.ResolvedView},
	}
	for _, tt := range tests {
		got, err := parseGraphView(tt.value)
		if err != nil {
			t.Fatalf("parseGraphView(%q): %v", tt.value, err)
		}
		if got != tt.want {
			t.Fatalf("parseGraphView(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
	if _, err := parseGraphView("host"); err == nil {
		t.Fatal("parseGraphView accepted unknown view")
	}
}

func TestMatchGraphGuardUsesConfigConditionSemantics(t *testing.T) {
	facts := &engine.Facts{OS: "linux", TargetFamily: "unix"}
	active, err := matchGraphGuard(&config.Condition{
		OS:           []string{"linux"},
		TargetFamily: []string{"unix"},
	}, facts)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("matching config condition projected inactive")
	}

	active, err = matchGraphGuard(&config.Condition{OS: []string{"windows"}}, facts)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("non-matching config condition projected active")
	}

	if _, err := matchGraphGuard(unsupportedGraphGuard{}, facts); err == nil {
		t.Fatal("unsupported graph guard type did not fail closed")
	}
}

func TestSelectedGraphCandidateUsesFirstSelectableAttempt(t *testing.T) {
	candidate, ok := selectedGraphCandidate([]exec.MethodAttempt{
		{Status: "skip_when", Candidate: 0, CandidateKnown: true},
		{Status: "would_install", Candidate: 2, CandidateKnown: true},
		{Status: "already_installed", Candidate: 3, CandidateKnown: true},
	})
	if !ok || candidate != 2 {
		t.Fatalf("selected candidate = (%d, %v), want (2, true)", candidate, ok)
	}
}

func TestSelectedGraphCandidateStopsAtSynthesizedWinner(t *testing.T) {
	candidate, ok := selectedGraphCandidate([]exec.MethodAttempt{
		{Status: "would_install", CandidateKnown: false},
		{Status: "would_install", Candidate: 1, CandidateKnown: true},
	})
	if ok {
		t.Fatalf("selected declared candidate %d after synthesized winner", candidate)
	}
}
