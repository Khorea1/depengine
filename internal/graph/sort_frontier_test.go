package graph

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// referenceLevels is the previous scan-based layering kept as an oracle: every
// iteration collects all remaining nodes with in-degree zero. The frontier
// implementation must produce identical levels and cycle reports.
func referenceLevels(g Graph) ([][]string, []string) {
	scheduling := g.SchedulingProjection()
	inDegree := make(map[string]int, len(scheduling.Nodes))
	children := make(map[string][]string, len(scheduling.Nodes))
	dependencies := make(map[string][]string, len(scheduling.Nodes))
	for name := range scheduling.Nodes {
		inDegree[name] = 0
	}
	for _, edge := range scheduling.Edges {
		inDegree[edge.To]++
		children[edge.From] = append(children[edge.From], edge.To)
		dependencies[edge.To] = append(dependencies[edge.To], edge.From)
	}
	for name := range children {
		sort.Strings(children[name])
	}
	for name := range dependencies {
		sort.Strings(dependencies[name])
	}

	remaining := make(map[string]bool, len(scheduling.Nodes))
	for name := range scheduling.Nodes {
		remaining[name] = true
	}

	var levels [][]string
	for len(remaining) > 0 {
		var level []string
		for name := range remaining {
			if inDegree[name] == 0 {
				level = append(level, name)
			}
		}
		sort.Strings(level)
		if len(level) == 0 {
			return levels, extractCycle(remaining, dependencies)
		}
		for _, name := range level {
			delete(remaining, name)
			for _, child := range children[name] {
				inDegree[child]--
			}
		}
		levels = append(levels, level)
	}
	return levels, nil
}

// deterministicDAG builds a deterministic acyclic fixture without a random source:
// the dependency count and offset come from the node index and seed, so every
// run compares the same shapes.
func deterministicDAG(seed int64, nodes int) Graph {
	g := NewGraph()
	for i := 0; i < nodes; i++ {
		g.AddNode(Node{ID: fmt.Sprintf("t%03d", i)})
	}
	for i := 0; i < nodes; i++ {
		deps := (i*7 + int(seed)) % 4
		for d := 0; d < deps; d++ {
			j := i + 1 + (i*(d+3)+int(seed))%6
			if j >= nodes {
				continue
			}
			g.AddEdge(Edge{
				From: fmt.Sprintf("t%03d", i),
				To:   fmt.Sprintf("t%03d", j),
				Kind: ToolRequire,
				Role: Scheduling,
			})
		}
	}
	return g
}

func TestSortGraphMatchesScanLayering(t *testing.T) {
	for _, nodes := range []int{1, 2, 7, 40, 250} {
		for seed := int64(1); seed <= 3; seed++ {
			g := deterministicDAG(seed*int64(nodes), nodes)
			want, wantCycle := referenceLevels(g)
			if wantCycle != nil {
				t.Fatalf("reference fixture unexpectedly cycled: %v", wantCycle)
			}
			got, err := SortGraph(g)
			if err != nil {
				t.Fatalf("nodes=%d seed=%d: SortGraph: %v", nodes, seed, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes=%d seed=%d: levels differ\n got: %v\nwant: %v", nodes, seed, got, want)
			}
		}
	}
}

func TestSortGraphReportsSameCycleAsScanLayering(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"a", "b", "c", "d"} {
		g.AddNode(Node{ID: id})
	}
	// a -> b -> c -> a is a cycle; d depends on a and is otherwise schedulable.
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "b", To: "c", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "c", To: "a", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "d", To: "a", Kind: ToolRequire, Role: Scheduling})

	_, wantCycle := referenceLevels(g)
	_, err := SortGraph(g)
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	var cycleErr *CycleError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("error = %T, want *CycleError", err)
	}
	if !reflect.DeepEqual(cycleErr.Cycle, wantCycle) {
		t.Fatalf("reported cycle = %v, want %v", cycleErr.Cycle, wantCycle)
	}
}
