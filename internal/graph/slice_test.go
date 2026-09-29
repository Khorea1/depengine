package graph

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// sliceFixture builds:
//
//	a -> b -> c -> d      (ToolRequire chain)
//	a -> c                (MethodRequire, http)
//	a -> c                (MethodRequire, source)  parallel multiedge
//	x -> c                (sibling of b: shares dependent c)
//	c -> y                (dependent branch)
func sliceFixture() Graph {
	g := NewGraph()
	for _, id := range []string{"a", "b", "c", "d", "x", "y", "lone"} {
		g.AddNode(Node{ID: id})
	}
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "b", To: "c", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "c", To: "d", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "a", To: "c", Kind: MethodRequire, Role: Activation, Method: "http", Candidate: 0, CandidateKnown: true})
	g.AddEdge(Edge{From: "a", To: "c", Kind: MethodRequire, Role: Activation, Method: "source", Candidate: 1, CandidateKnown: true})
	g.AddEdge(Edge{From: "x", To: "c", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "c", To: "y", Kind: ToolRequire, Role: Scheduling})
	return g.Canonicalize()
}

func nodeIDs(g Graph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func TestSliceDirectionAndDepth(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		direction Direction
		depth     int
		want      []string
		wantEdges int
	}{
		{"deps unbounded is the dependency closure", "c", DirectionDeps, Unbounded, []string{"a", "b", "c", "x"}, 5},
		{"deps depth 1 keeps direct dependencies", "c", DirectionDeps, 1, []string{"a", "b", "c", "x"}, 5},
		{"deps depth 0 keeps only the root", "c", DirectionDeps, 0, []string{"c"}, 0},
		{"dependents unbounded", "c", DirectionDependents, Unbounded, []string{"c", "d", "y"}, 2},
		{"dependents from a reach everything downstream", "a", DirectionDependents, Unbounded, []string{"a", "b", "c", "d", "y"}, 6},
		{"dependents depth 1", "a", DirectionDependents, 1, []string{"a", "b", "c"}, 4},
		{"both is a union, not an undirected walk", "b", DirectionBoth, 1, []string{"a", "b", "c"}, 4},
		{"both unbounded excludes siblings x", "b", DirectionBoth, Unbounded, []string{"a", "b", "c", "d", "y"}, 6},
		{"isolated root", "lone", DirectionBoth, Unbounded, []string{"lone"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sliceFixture().Slice([]string{tt.root}, tt.direction, tt.depth)
			if err != nil {
				t.Fatalf("Slice: %v", err)
			}
			if ids := nodeIDs(got); !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("nodes = %v, want %v", ids, tt.want)
			}
			if len(got.Edges) != tt.wantEdges {
				t.Errorf("edges = %d, want %d: %+v", len(got.Edges), tt.wantEdges, got.Edges)
			}
		})
	}
}

func TestSlicePreservesMultiedgesAndInducesBoundaryEdges(t *testing.T) {
	// depth 1 from a: {a, b, c}. b->c sits between two boundary nodes and was
	// never traversed, but both endpoints are kept, so the edge stays.
	got, err := sliceFixture().Slice([]string{"a"}, DirectionDependents, 1)
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	sawBoundary := false
	for _, e := range got.Edges {
		if e.From == "a" && e.To == "c" && e.Kind == MethodRequire {
			methods = append(methods, e.Method)
		}
		if e.From == "b" && e.To == "c" {
			sawBoundary = true
		}
	}
	sort.Strings(methods)
	if !reflect.DeepEqual(methods, []string{"http", "source"}) {
		t.Errorf("multiedge methods = %v, want [http source]", methods)
	}
	if !sawBoundary {
		t.Error("boundary edge b->c dropped; slice must be the induced subgraph")
	}
}

func TestSliceDeepestDistanceIsShortest(t *testing.T) {
	// a->b->c and a->c: c is distance 1 from a via the direct edge, so
	// depth 1 must include it even though the chain reaches it at 2.
	got, err := sliceFixture().Slice([]string{"a"}, DirectionDependents, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Nodes["c"]; !ok {
		t.Error("c missing at depth 1: shortest path must win")
	}
	if _, ok := got.Nodes["d"]; ok {
		t.Error("d present at depth 1")
	}
}

func TestSliceDoesNotMutateInputAndIsDeterministic(t *testing.T) {
	g := sliceFixture()
	before := len(g.Edges)
	first, err := g.Slice([]string{"b", "a"}, DirectionBoth, Unbounded)
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Slice([]string{"a", "b"}, DirectionBoth, Unbounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != before || len(g.Nodes) != 7 {
		t.Error("input graph mutated")
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("root order changed the result")
	}
}

func TestSliceErrors(t *testing.T) {
	g := sliceFixture()
	if _, err := g.Slice([]string{"nope"}, DirectionDeps, Unbounded); err == nil || !strings.Contains(err.Error(), `unknown tool "nope"`) {
		t.Errorf("unknown root error = %v", err)
	}
	if _, err := g.Slice([]string{"a"}, DirectionDeps, -2); err == nil {
		t.Error("depth -2 accepted")
	}
	if _, err := g.Slice([]string{"a"}, Direction(9), Unbounded); err == nil {
		t.Error("invalid direction accepted")
	}
}

func TestParseDirection(t *testing.T) {
	for _, d := range []Direction{DirectionDeps, DirectionDependents, DirectionBoth} {
		got, err := ParseDirection(d.String())
		if err != nil || got != d {
			t.Errorf("round trip %s: got %v, %v", d, got, err)
		}
	}
	if _, err := ParseDirection("up"); err == nil {
		t.Error("ParseDirection accepted \"up\"")
	}
}
