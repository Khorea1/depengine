// Package graph resolves tool dependency ordering.
//
// The executor uses Sort to determine installation order: tools with no
// scheduling dependencies come first (level 0), then tools whose scheduling
// dependencies are all satisfied, and so on. Cycles are detected and reported
// with the involved tool names.
package graph

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

type node interface {
	GraphDependencies() []string
}

// SortOption configures Sort behavior.
type SortOption func(*sortConfig)

type sortConfig struct {
	logger *slog.Logger
}

// WithLogger sets a logger for debug output during sort.
func WithLogger(l *slog.Logger) SortOption {
	return func(c *sortConfig) {
		c.logger = l
	}
}

// Sort is the compatibility entry point for callers that only expose
// GraphDependencies. It translates that boundary into scheduling edges and
// delegates ordering to SortGraph.
func Sort[T node](tools map[string]T, opts ...SortOption) ([][]string, error) {
	graph := NewGraph()
	for name, tool := range tools {
		graph.AddNode(Node{ID: name})
		for _, dependency := range tool.GraphDependencies() {
			graph.AddEdge(Edge{
				From: dependency,
				To:   name,
				Kind: ToolRequire,
				Role: Scheduling,
			})
		}
	}
	return SortGraph(graph, opts...)
}

// SortGraph returns graph nodes in topological order grouped by dependency
// depth. Only Scheduling edges participate; Activation edges are intentionally
// ignored for ordering and cycle detection.
//
// Uses a single Kahn's algorithm pass for both cycle detection and level
// computation. Returns an error if a scheduling cycle is detected (CycleError)
// or if a scheduling dependency points at a missing node.
func SortGraph(graph Graph, opts ...SortOption) ([][]string, error) {
	if len(graph.Nodes) == 0 {
		return [][]string{}, nil
	}

	cfg := &sortConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	scheduling := graph.SchedulingProjection()

	inDegree := make(map[string]int, len(scheduling.Nodes))
	children := make(map[string][]string, len(scheduling.Nodes))
	dependencies := make(map[string][]string, len(scheduling.Nodes))
	for name := range scheduling.Nodes {
		inDegree[name] = 0
	}

	for _, edge := range scheduling.Edges {
		if _, ok := scheduling.Nodes[edge.To]; !ok {
			return nil, fmt.Errorf("graph: dependency edge targets %q, which is not in graph", edge.To)
		}
		if _, ok := scheduling.Nodes[edge.From]; !ok {
			return nil, fmt.Errorf("graph: tool %q requires %q, which is not in schema", edge.To, edge.From)
		}
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

	// ready holds the nodes whose prerequisites are all scheduled. Draining one
	// frontier per level keeps the cost linear in nodes and edges; rescanning
	// every remaining node per level made deep graphs pay O(nodes × depth) map
	// lookups. The level sets are identical: at the start of an iteration a node
	// is in ready exactly when it has no unscheduled prerequisite.
	ready := make([]string, 0, len(scheduling.Nodes))
	for name := range scheduling.Nodes {
		if inDegree[name] == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)

	levels := [][]string{}
	scheduled := 0
	for len(ready) > 0 {
		level := ready
		ready = make([]string, 0, len(level))
		sort.Strings(level)
		scheduled += len(level)

		for _, name := range level {
			for _, child := range children[name] {
				inDegree[child]--
				if inDegree[child] == 0 {
					ready = append(ready, child)
				}
			}
		}
		if cfg.logger != nil {
			cfg.logger.Debug("graph", "level", len(levels), "tools", strings.Join(level, ", "))
		}
		levels = append(levels, level)
	}

	if scheduled < len(scheduling.Nodes) {
		// Only reached on a cycle: rebuild the unscheduled set so the reported
		// cycle stays identical to the previous scan-based implementation.
		remaining := make(map[string]bool, len(scheduling.Nodes))
		for name := range scheduling.Nodes {
			remaining[name] = true
		}
		for _, level := range levels {
			for _, name := range level {
				delete(remaining, name)
			}
		}
		return nil, &CycleError{Cycle: extractCycle(remaining, dependencies)}
	}

	return levels, nil
}

// extractCycle finds a scheduling cycle among the remaining nodes by following
// prerequisite edges until a node repeats. Inputs are sorted before choosing
// the next prerequisite, keeping the reported cycle deterministic.
func extractCycle(remaining map[string]bool, dependencies map[string][]string) []string {
	starts := make([]string, 0, len(remaining))
	for name := range remaining {
		starts = append(starts, name)
	}
	sort.Strings(starts)

	for _, name := range starts {
		path := []string{}
		pathSet := map[string]int{}
		cur := name
		for {
			if idx, ok := pathSet[cur]; ok {
				return append([]string(nil), path[idx:]...)
			}
			pathSet[cur] = len(path)
			path = append(path, cur)

			next := ""
			for _, dependency := range dependencies[cur] {
				if remaining[dependency] {
					next = dependency
					break
				}
			}
			if next == "" {
				break
			}
			cur = next
		}
	}

	return []string{"unknown cycle"}
}

// CycleError is returned when a dependency cycle is detected.
type CycleError struct {
	Cycle []string // tools in the cycle, in dependency order
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("dependency cycle detected: %s", strings.Join(e.Cycle, " → "))
}
