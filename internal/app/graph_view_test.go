package app

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/platform"
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

func TestValidateGraphProjectionOptions(t *testing.T) {
	if err := validateGraphProjectionOptions(graph.DeclaredView, false); err != nil {
		t.Fatalf("declared view without inactive diagnostics: %v", err)
	}
	if err := validateGraphProjectionOptions(graph.EffectiveView, true); err != nil {
		t.Fatalf("effective view rejected inactive diagnostics: %v", err)
	}
	if err := validateGraphProjectionOptions(graph.ResolvedView, true); err != nil {
		t.Fatalf("resolved view rejected inactive diagnostics: %v", err)
	}
	if err := validateGraphProjectionOptions(graph.DeclaredView, true); err == nil {
		t.Fatal("declared view accepted --show-inactive")
	}
}

func TestMatchGraphGuardUsesConfigConditionSemantics(t *testing.T) {
	facts := &platform.Facts{OS: "linux", TargetFamily: "unix"}
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

func TestGraphProjectionRequirementsAreDemandDriven(t *testing.T) {
	g := graph.NewGraph()
	g.AddEdge(graph.Edge{From: "base", To: "app", Kind: graph.ToolRequire, Role: graph.Scheduling})
	needsGuards, candidateTools := graphProjectionRequirements(g, graph.EffectiveView)
	if needsGuards || len(candidateTools) != 0 {
		t.Fatalf("unguarded effective requirements = guards:%v candidates:%v", needsGuards, candidateTools)
	}

	guarded := &config.Condition{OS: []string{"linux"}}
	g.AddEdge(graph.Edge{From: "platform", To: "app", Kind: graph.ToolRequire, Role: graph.Scheduling, Guard: guarded})
	g.AddEdge(graph.Edge{
		From:           "curl",
		To:             "app",
		Kind:           graph.MethodRequire,
		Role:           graph.Activation,
		Candidate:      0,
		CandidateKnown: true,
	})

	needsGuards, candidateTools = graphProjectionRequirements(g, graph.ResolvedView)
	if !needsGuards {
		t.Fatal("guarded resolved graph did not request host facts")
	}
	if _, ok := candidateTools["app"]; !ok || len(candidateTools) != 1 {
		t.Fatalf("candidate tools = %v, want only app", candidateTools)
	}
}

func TestResolvedGraphCandidatesPropagatesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	schema := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": {Name: "app"},
		},
	}
	_, err := resolvedGraphCandidates(ctx, schema, &platform.Facts{}, map[string]struct{}{"app": {}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resolvedGraphCandidates error = %v, want context.Canceled", err)
	}
}

func TestValidateGraphSliceOptions(t *testing.T) {
	if err := validateGraphSliceOptions("", graph.DirectionDeps, graph.Unbounded); err != nil {
		t.Fatalf("defaults without --only: %v", err)
	}
	if err := validateGraphSliceOptions("app", graph.DirectionBoth, 2); err != nil {
		t.Fatalf("slice with --only: %v", err)
	}
	if err := validateGraphSliceOptions("", graph.DirectionDependents, graph.Unbounded); err == nil {
		t.Fatal("--direction without --only accepted")
	}
	if err := validateGraphSliceOptions("", graph.DirectionDeps, 1); err == nil {
		t.Fatal("--depth without --only accepted")
	}
	if err := validateGraphSliceOptions("app", graph.DirectionDeps, -2); err == nil {
		t.Fatal("depth below -1 accepted")
	}
}

func TestGraphSliceRequestedKeepsHistoricalDefault(t *testing.T) {
	if graphSliceRequested(graph.DirectionDeps, graph.Unbounded) {
		t.Fatal("default direction/depth must keep the schema-level --only closure")
	}
	if !graphSliceRequested(graph.DirectionDeps, 0) || !graphSliceRequested(graph.DirectionBoth, graph.Unbounded) {
		t.Fatal("non-default direction or depth must slice the IR")
	}
}

func TestSliceDeclaredGraphUsesCompleteGraph(t *testing.T) {
	tools := map[string]*config.Tool{
		"app":    {Name: "app", Requires: []string{"lib"}},
		"lib":    {Name: "lib", Requires: []string{"base"}},
		"base":   {Name: "base", DependencyOnly: true},
		"plugin": {Name: "plugin", Requires: []string{"app"}},
		"other":  {Name: "other"},
	}

	got, ok, err := sliceDeclaredGraph(tools, "app", "", "", graph.DirectionDependents, graph.Unbounded)
	if err != nil || !ok {
		t.Fatalf("dependents slice: ok=%v err=%v", ok, err)
	}
	if _, present := got.Nodes["plugin"]; !present {
		t.Fatal("dependent traversal lost successors: plugin missing")
	}
	if _, present := got.Nodes["lib"]; present {
		t.Fatal("dependents slice leaked a dependency")
	}

	got, ok, err = sliceDeclaredGraph(tools, "plugin", "", "", graph.DirectionDeps, 1)
	if err != nil || !ok {
		t.Fatalf("bounded deps slice: ok=%v err=%v", ok, err)
	}
	if _, present := got.Nodes["app"]; !present {
		t.Fatal("depth 1 missed the direct dependency")
	}
	if _, present := got.Nodes["lib"]; present {
		t.Fatal("depth 1 reached a transitive dependency")
	}
	for _, edge := range got.Edges {
		if _, present := got.Nodes[edge.From]; !present {
			t.Fatalf("dangling edge from %q", edge.From)
		}
	}

	got, ok, err = sliceDeclaredGraph(tools, "base", "", "", graph.DirectionDependents, graph.Unbounded)
	if err != nil || !ok {
		t.Fatalf("dependency_only root: ok=%v err=%v", ok, err)
	}
	if len(got.Nodes) != 4 {
		t.Fatalf("dependents of base = %d nodes, want 4 (base, lib, app, plugin)", len(got.Nodes))
	}
}

func TestSliceDeclaredGraphRootEligibility(t *testing.T) {
	tools := map[string]*config.Tool{
		"app": {Name: "app", Tags: []string{"dev"}},
		"cli": {Name: "cli", Requires: []string{"app"}},
	}
	if _, ok, err := sliceDeclaredGraph(tools, "missing", "", "", graph.DirectionBoth, graph.Unbounded); ok || err != nil {
		t.Fatalf("unknown root: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := sliceDeclaredGraph(tools, "app", "app", "", graph.DirectionBoth, graph.Unbounded); ok {
		t.Fatal("skipped root was sliced")
	}
	if _, ok, _ := sliceDeclaredGraph(tools, "app", "", "ops", graph.DirectionBoth, graph.Unbounded); ok {
		t.Fatal("root outside --profile was sliced")
	}
	got, ok, err := sliceDeclaredGraph(tools, "app", "cli", "dev", graph.DirectionDependents, graph.Unbounded)
	if err != nil || !ok {
		t.Fatalf("root eligible: ok=%v err=%v", ok, err)
	}
	if _, present := got.Nodes["cli"]; !present {
		t.Fatal("--skip removed a traversed dependent; it must only gate the root")
	}
}
