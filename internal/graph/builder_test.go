package graph

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestBuildDeclaredGraphFromConfigTools(t *testing.T) {
	toolGuard := &config.Condition{TargetFamily: []string{"unix"}}
	methodGuard := &config.Condition{OS: []string{"linux"}}
	tools := map[string]*config.Tool{
		"app": {
			Name:           "app",
			Tags:           []string{"cli"},
			DependencyOnly: true,
			Requires:       []string{"git"},
			RequiresWhen:   map[string]*config.Condition{"git": toolGuard},
			Methods: []*config.MethodCandidate{
				{Kind: "http", Requires: []string{"curl"}, When: methodGuard},
			},
		},
		"curl": {Name: "curl"},
		"git":  {Name: "git"},
	}

	graph := BuildDeclaredGraph(tools)

	if len(graph.Nodes) != 3 {
		t.Fatalf("expected three nodes, got %d", len(graph.Nodes))
	}
	app := graph.Nodes["app"]
	if !app.DependencyOnly {
		t.Fatal("expected dependency-only metadata to be preserved")
	}
	if len(app.Tags) != 1 || app.Tags[0] != "cli" {
		t.Fatalf("expected tags to be preserved, got %v", app.Tags)
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected tool and method edges, got %d", len(graph.Edges))
	}

	var scheduling, activation *Edge
	for i := range graph.Edges {
		edge := &graph.Edges[i]
		switch edge.Kind {
		case ToolRequire:
			scheduling = edge
		case MethodRequire:
			activation = edge
		}
	}
	if scheduling == nil || scheduling.From != "git" || scheduling.To != "app" || scheduling.Role != Scheduling {
		t.Fatalf("unexpected scheduling edge: %#v", scheduling)
	}
	if scheduling.Guard != toolGuard {
		t.Fatalf("tool guard was not preserved losslessly: %#v", scheduling.Guard)
	}
	if got := guardLabel(scheduling.Guard); got != `target_family in ["unix"]` {
		t.Fatalf("unexpected tool guard label: %q", got)
	}

	if activation == nil || activation.From != "curl" || activation.To != "app" || activation.Role != Activation || activation.Method != "http" {
		t.Fatalf("unexpected activation edge: %#v", activation)
	}
	if activation.Guard != methodGuard {
		t.Fatalf("method guard was not preserved losslessly: %#v", activation.Guard)
	}
	if got := guardLabel(activation.Guard); got != `os in ["linux"]` {
		t.Fatalf("unexpected method guard label: %q", got)
	}
}

func TestBuildDeclaredGraphPreservesSemanticMultiedges(t *testing.T) {
	tools := map[string]*config.Tool{
		"app": {
			Name:     "app",
			Requires: []string{"git"},
			Methods: []*config.MethodCandidate{
				{Kind: "source", Requires: []string{"git"}},
			},
		},
		"git": {Name: "git"},
	}

	graph := BuildDeclaredGraph(tools)
	if len(graph.Edges) != 2 {
		t.Fatalf("expected parallel semantic edges, got %d", len(graph.Edges))
	}
	if graph.Edges[0].From != "git" || graph.Edges[1].From != "git" ||
		graph.Edges[0].To != "app" || graph.Edges[1].To != "app" {
		t.Fatalf("expected both edges to connect git -> app: %#v", graph.Edges)
	}
	if graph.Edges[0].Kind == graph.Edges[1].Kind {
		t.Fatalf("expected distinct semantic kinds: %#v", graph.Edges)
	}
}

func TestBuildDeclaredGraphKeepsCandidatesWithSameKindDistinct(t *testing.T) {
	linux := &config.Condition{OS: []string{"linux"}}
	darwin := &config.Condition{OS: []string{"darwin"}}
	tools := map[string]*config.Tool{
		"app": {
			Methods: []*config.MethodCandidate{
				{Kind: "http", Requires: []string{"curl"}, When: linux},
				{Kind: "http", Requires: []string{"wget"}, When: darwin},
			},
		},
		"curl": {},
		"wget": {},
	}

	graph := BuildDeclaredGraph(tools)
	if len(graph.Edges) != 2 {
		t.Fatalf("expected both candidate edges, got %#v", graph.Edges)
	}

	guards := map[string]Guard{}
	candidates := map[string]int{}
	for _, edge := range graph.Edges {
		guards[edge.From] = edge.Guard
		candidates[edge.From] = edge.Candidate
	}
	if guards["curl"] != linux || guards["wget"] != darwin {
		t.Fatalf("candidate guards were collapsed: %#v", graph.Edges)
	}
	if candidates["curl"] != 0 || candidates["wget"] != 1 || !graph.Edges[0].CandidateKnown || !graph.Edges[1].CandidateKnown {
		t.Fatalf("exact candidate identities were not preserved: %#v", graph.Edges)
	}
}

func TestBuildDeclaredGraphCopiesTags(t *testing.T) {
	tool := &config.Tool{Name: "app", Tags: []string{"cli"}}
	graph := BuildDeclaredGraph(map[string]*config.Tool{"app": tool})

	tool.Tags[0] = "mutated"
	if got := graph.Nodes["app"].Tags[0]; got != "cli" {
		t.Fatalf("graph tags alias config model: got %q", got)
	}
}
