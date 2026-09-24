package graph

import "testing"

type testGuard string

func (g testGuard) String() string { return string(g) }

func TestSchedulingProjectionKeepsOnlySchedulingEdges(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "a"})
	g.AddNode(Node{ID: "b"})
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "a", To: "b", Kind: MethodRequire, Role: Activation, Method: "http"})

	projected := g.SchedulingProjection()
	if len(projected.Edges) != 1 {
		t.Fatalf("expected one scheduling edge, got %d", len(projected.Edges))
	}
	if projected.Edges[0].Kind != ToolRequire {
		t.Fatalf("expected tool requirement edge")
	}
}

func TestGraphPreservesSemanticMultiedges(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "a", To: "b", Kind: MethodRequire, Role: Activation})

	if len(g.Edges) != 2 {
		t.Fatalf("expected semantic multiedges to be preserved")
	}
}

func TestCanonicalizeOrdersCompleteEdgeSemantics(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "a", To: "b", Kind: MethodRequire, Role: Activation, Method: "source", Guard: testGuard("linux")})
	g.AddEdge(Edge{From: "a", To: "b", Kind: MethodRequire, Role: Activation, Method: "http", Guard: testGuard("unix")})
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})

	got := g.Canonicalize().Edges
	if len(got) != 3 {
		t.Fatalf("expected three edges, got %d", len(got))
	}
	if got[0].Kind != ToolRequire {
		t.Fatalf("tool edge should sort before method edges: %#v", got)
	}
	if got[1].Method != "http" || got[2].Method != "source" {
		t.Fatalf("method metadata must participate in canonical ordering: %#v", got)
	}
}

func TestCanonicalizeUsesGuardLabelAsTieBreaker(t *testing.T) {
	g := NewGraph()
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("z")})
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("a")})

	got := g.Canonicalize().Edges
	if guardLabel(got[0].Guard) != "a" || guardLabel(got[1].Guard) != "z" {
		t.Fatalf("guards were not canonicalized deterministically: %#v", got)
	}
}

func TestCanonicalizeCopiesNodeTags(t *testing.T) {
	tags := []string{"cli"}
	g := NewGraph()
	g.AddNode(Node{ID: "app", Tags: tags})
	tags[0] = "mutated"

	if got := g.Nodes["app"].Tags[0]; got != "cli" {
		t.Fatalf("AddNode retained caller slice: %q", got)
	}

	canonical := g.Canonicalize()
	gNode := g.Nodes["app"]
	gNode.Tags[0] = "changed"
	g.Nodes["app"] = gNode
	if got := canonical.Nodes["app"].Tags[0]; got != "cli" {
		t.Fatalf("Canonicalize retained source slice: %q", got)
	}
}
