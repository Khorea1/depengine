package graph

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzeSplitsComponentsAndIsolates(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"a", "b", "solo", "x", "y"} {
		g.AddNode(Node{ID: id})
	}
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "b", To: "a", Kind: MethodRequire, Role: Activation, Method: "http"})
	g.AddEdge(Edge{From: "x", To: "y", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("os = linux")})

	analysis, err := Analyze(g)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(analysis.Connected) != 2 {
		t.Fatalf("expected 2 components, got %d", len(analysis.Connected))
	}
	first := analysis.Connected[0]
	if !reflect.DeepEqual(first.Nodes, []string{"a", "b"}) {
		t.Errorf("first component nodes = %v", first.Nodes)
	}
	if len(first.Edges) != 2 {
		t.Errorf("first component should keep both semantic edges, got %d", len(first.Edges))
	}
	second := analysis.Connected[1]
	if !reflect.DeepEqual(second.Nodes, []string{"x", "y"}) {
		t.Errorf("second component nodes = %v", second.Nodes)
	}
	if len(second.Edges) != 1 {
		t.Errorf("second component edges = %d", len(second.Edges))
	}
	if !reflect.DeepEqual(analysis.Isolated, []string{"solo"}) {
		t.Errorf("isolated = %v", analysis.Isolated)
	}

	wantLevels := [][]string{{"a", "solo", "x"}, {"b", "y"}}
	if !reflect.DeepEqual(analysis.Levels, wantLevels) {
		t.Errorf("levels = %v, want %v", analysis.Levels, wantLevels)
	}
	for rank, level := range analysis.Levels {
		for _, id := range level {
			if analysis.Ranks[id] != rank {
				t.Errorf("rank[%s] = %d, want %d", id, analysis.Ranks[id], rank)
			}
		}
	}
}

func TestAnalyzeRanksIgnoreActivationEdges(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"a", "b", "c", "d"} {
		g.AddNode(Node{ID: id})
	}
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "b", To: "a", Kind: MethodRequire, Role: Activation, Method: "http"})
	g.AddEdge(Edge{From: "c", To: "d", Kind: MethodRequire, Role: Activation, Method: "http"})
	g.AddEdge(Edge{From: "d", To: "c", Kind: MethodRequire, Role: Activation, Method: "http"})

	analysis, err := Analyze(g)
	if err != nil {
		t.Fatalf("activation edges must not create cycles: %v", err)
	}

	wantRanks := map[string]int{"a": 0, "b": 1, "c": 0, "d": 0}
	for id, want := range wantRanks {
		if analysis.Ranks[id] != want {
			t.Errorf("rank[%s] = %d, want %d", id, analysis.Ranks[id], want)
		}
	}
	// The backward activation edge must not reorder scheduling ranks, and the
	// activation-only pair must still share one weak component.
	if len(analysis.Connected) != 2 {
		t.Fatalf("expected 2 components, got %d", len(analysis.Connected))
	}
}

func TestAnalyzeIsDeterministic(t *testing.T) {
	build := func(reverse bool) Graph {
		edges := [][2]string{{"a", "b"}, {"b", "c"}, {"a", "d"}, {"x", "y"}}
		g := NewGraph()
		for _, id := range []string{"a", "b", "c", "d", "x", "y", "solo"} {
			g.AddNode(Node{ID: id})
		}
		if reverse {
			for i := len(edges) - 1; i >= 0; i-- {
				edge := edges[i]
				g.AddEdge(Edge{From: edge[0], To: edge[1], Kind: ToolRequire, Role: Scheduling})
			}
			return g
		}
		for _, edge := range edges {
			g.AddEdge(Edge{From: edge[0], To: edge[1], Kind: ToolRequire, Role: Scheduling})
		}
		return g
	}

	first, err := Analyze(build(false))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	second, err := Analyze(build(true))
	if err != nil {
		t.Fatalf("Analyze(reversed): %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("insertion order changed the analysis:\n%+v\n%+v", first, second)
	}
}

func TestAnalyzeRejectsDanglingActivationEdge(t *testing.T) {
	// Activation edges are invisible to SortGraph, so Analyze's own endpoint
	// validation is the only guard for them.
	g := NewGraph()
	g.AddNode(Node{ID: "a"})
	g.AddEdge(Edge{From: "a", To: "ghost", Kind: MethodRequire, Role: Activation, Method: "http"})

	_, err := Analyze(g)
	if err == nil {
		t.Fatal("expected an error for an activation edge to a missing node")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the missing node, got: %v", err)
	}
}

func TestAnalyzeRejectsDanglingSchedulingEdge(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "a"})
	g.AddEdge(Edge{From: "a", To: "ghost", Kind: ToolRequire, Role: Scheduling})

	_, err := Analyze(g)
	if err == nil {
		t.Fatal("expected an error for a scheduling edge to a missing node")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the missing node, got: %v", err)
	}
}

func TestAnalyzeRejectsSchedulingCycle(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "a"})
	g.AddNode(Node{ID: "b"})
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "b", To: "a", Kind: ToolRequire, Role: Scheduling})

	_, err := Analyze(g)
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	var cycle *CycleError
	if !errors.As(err, &cycle) {
		t.Errorf("expected a CycleError, got %v", err)
	}
}

func TestAnalyzeOrdersComponentsByLowestNodeID(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"Zebra", "apple", "mango", "pear"} {
		g.AddNode(Node{ID: id})
	}
	g.AddEdge(Edge{From: "Zebra", To: "apple", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "mango", To: "pear", Kind: ToolRequire, Role: Scheduling})

	analysis, err := Analyze(g)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(analysis.Connected) != 2 {
		t.Fatalf("expected 2 components, got %d", len(analysis.Connected))
	}
	// Byte-wise ordering: "Zebra" (uppercase) precedes "mango".
	if got := analysis.Connected[0].Nodes[0]; got != "Zebra" {
		t.Errorf("first component starts with %q, want %q", got, "Zebra")
	}
	if got := analysis.Connected[1].Nodes[0]; got != "mango" {
		t.Errorf("second component starts with %q, want %q", got, "mango")
	}
}
