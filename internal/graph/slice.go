package graph

import (
	"fmt"
	"sort"
)

// Direction selects which relations a slice follows from its roots.
//
// Edges are directed dependency -> dependent, so DirectionDeps walks edges
// backwards (predecessors) and DirectionDependents walks them forwards
// (successors).
type Direction uint8

const (
	// DirectionDeps follows what the roots depend on. Together with an
	// unbounded depth it is the dependency closure that `--only` has always
	// produced.
	DirectionDeps Direction = iota
	// DirectionDependents follows what depends on the roots.
	DirectionDependents
	// DirectionBoth is the union of the two traversals. It is not an
	// undirected walk: siblings that merely share a dependency with a root are
	// not included.
	DirectionBoth
)

// Unbounded is the depth that removes the traversal limit.
const Unbounded = -1

// String returns the CLI spelling of the direction.
func (d Direction) String() string {
	switch d {
	case DirectionDeps:
		return "deps"
	case DirectionDependents:
		return "dependents"
	case DirectionBoth:
		return "both"
	default:
		return fmt.Sprintf("Direction(%d)", uint8(d))
	}
}

// ParseDirection converts a CLI value into a Direction.
func ParseDirection(value string) (Direction, error) {
	switch value {
	case "deps":
		return DirectionDeps, nil
	case "dependents":
		return DirectionDependents, nil
	case "both":
		return DirectionBoth, nil
	default:
		return 0, fmt.Errorf("unknown direction %q (valid: deps, dependents, both)", value)
	}
}

// Slice returns the subgraph reachable from roots in the given direction.
//
// Depth counts edges from the nearest root: depth 0 keeps only the roots,
// depth 1 adds their direct neighbours, and Unbounded walks to the end. Every
// semantic edge participates in the walk regardless of kind, role, or state,
// matching the traversal used by tool filtering (tool-level and method-level
// requirements alike).
//
// The result is the induced subgraph: every node reached, plus every semantic
// edge whose two endpoints were both reached, including multiedges and edges
// between two nodes that sit at the depth boundary. Slicing therefore never
// changes the meaning of an edge and never fabricates one.
//
// Slice must run against the complete graph. Pre-filtering the schema would
// hide successors, which dependent traversal needs.
func (g Graph) Slice(roots []string, direction Direction, depth int) (Graph, error) {
	if depth < Unbounded {
		return Graph{}, fmt.Errorf("invalid depth %d: must be >= 0, or %d for unbounded", depth, Unbounded)
	}
	if direction > DirectionBoth {
		return Graph{}, fmt.Errorf("invalid direction %s", direction)
	}
	for _, root := range roots {
		if _, ok := g.Nodes[root]; !ok {
			return Graph{}, fmt.Errorf("unknown tool %q", root)
		}
	}

	predecessors := make(map[string][]string, len(g.Nodes))
	successors := make(map[string][]string, len(g.Nodes))
	for _, edge := range g.Edges {
		predecessors[edge.To] = append(predecessors[edge.To], edge.From)
		successors[edge.From] = append(successors[edge.From], edge.To)
	}

	keep := make(map[string]struct{}, len(roots))
	if direction == DirectionDeps || direction == DirectionBoth {
		reach(keep, roots, predecessors, depth)
	}
	if direction == DirectionDependents || direction == DirectionBoth {
		reach(keep, roots, successors, depth)
	}

	out := NewGraph()
	for id := range keep {
		if node, ok := g.Nodes[id]; ok {
			out.AddNode(node)
		}
	}
	for _, edge := range g.Edges {
		_, fromKept := out.Nodes[edge.From]
		_, toKept := out.Nodes[edge.To]
		if fromKept && toKept {
			out.AddEdge(edge)
		}
	}
	return out.Canonicalize(), nil
}

// reach adds every node within depth hops of roots, following adjacency, to
// keep. Breadth-first order makes the recorded distance the shortest one, so a
// node reachable by both a short and a long path is judged by the short path.
func reach(keep map[string]struct{}, roots []string, adjacency map[string][]string, depth int) {
	sorted := append([]string(nil), roots...)
	sort.Strings(sorted)

	distance := make(map[string]int, len(sorted))
	queue := make([]string, 0, len(sorted))
	for _, root := range sorted {
		if _, seen := distance[root]; seen {
			continue
		}
		distance[root] = 0
		queue = append(queue, root)
	}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		keep[node] = struct{}{}
		if depth != Unbounded && distance[node] >= depth {
			continue
		}
		for _, next := range adjacency[node] {
			if _, seen := distance[next]; seen {
				continue
			}
			distance[next] = distance[node] + 1
			queue = append(queue, next)
		}
	}
}
