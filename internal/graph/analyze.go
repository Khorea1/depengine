package graph

import (
	"fmt"
	"sort"
)

// Component is a weakly connected group of nodes together with every semantic
// edge between them.
//
// Terminal rendering lays each component out independently: a component whose
// layout does not fit the available width falls back to a compact edge list
// without affecting the components that still fit.
type Component struct {
	Nodes []string // sorted node IDs
	Edges []Edge   // canonical edge order, restricted to this component
}

// Analysis is the layout-facing projection of a graph.
//
// Ranks come from the scheduling projection only, so activation edges stay
// visible inside components without influencing node order.
type Analysis struct {
	Levels    [][]string     // scheduling levels; a level index is a node rank
	Ranks     map[string]int // node ID -> scheduling rank
	Connected []Component    // components with at least one edge
	Isolated  []string       // nodes without any edge, sorted
}

// Analyze computes scheduling ranks and weakly connected components.
//
// Weak connectivity ignores edge direction and role, so grouping sees every
// visible relation while ordering only sees scheduling ones. Components are
// ordered by their lowest node ID and members stay in canonical order, which
// keeps the result deterministic.
func Analyze(g Graph) (Analysis, error) {
	g = g.Canonicalize()

	levels, err := SortGraph(g)
	if err != nil {
		return Analysis{}, err
	}

	for _, edge := range g.Edges {
		if _, ok := g.Nodes[edge.From]; !ok {
			return Analysis{}, fmt.Errorf("graph: edge %q -> %q starts at a node that is not in the graph", edge.From, edge.To)
		}
		if _, ok := g.Nodes[edge.To]; !ok {
			return Analysis{}, fmt.Errorf("graph: edge %q -> %q targets a node that is not in the graph", edge.From, edge.To)
		}
	}

	analysis := Analysis{
		Levels: levels,
		Ranks:  make(map[string]int, len(g.Nodes)),
	}
	for rank, level := range levels {
		for _, id := range level {
			analysis.Ranks[id] = rank
		}
	}

	neighbors := make(map[string][]string, len(g.Nodes))
	for _, edge := range g.Edges {
		neighbors[edge.From] = append(neighbors[edge.From], edge.To)
		neighbors[edge.To] = append(neighbors[edge.To], edge.From)
	}
	for id := range neighbors {
		sort.Strings(neighbors[id])
	}

	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	visited := make(map[string]bool, len(g.Nodes))
	owner := make(map[string]int, len(g.Nodes))
	components := make([]Component, 0, len(ids))
	for _, id := range ids {
		if visited[id] {
			continue
		}
		visited[id] = true
		member := []string{}
		queue := []string{id}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			member = append(member, current)
			for _, next := range neighbors[current] {
				if !visited[next] {
					visited[next] = true
					queue = append(queue, next)
				}
			}
		}
		sort.Strings(member)
		index := len(components)
		for _, memberID := range member {
			owner[memberID] = index
		}
		components = append(components, Component{Nodes: member})
	}

	// Both endpoints of an edge are neighbors, so every edge belongs to
	// exactly one component and canonical order is preserved per component.
	for _, edge := range g.Edges {
		index := owner[edge.From]
		components[index].Edges = append(components[index].Edges, edge)
	}

	for _, component := range components {
		if len(component.Edges) == 0 {
			analysis.Isolated = append(analysis.Isolated, component.Nodes[0])
			continue
		}
		analysis.Connected = append(analysis.Connected, component)
	}

	return analysis, nil
}
