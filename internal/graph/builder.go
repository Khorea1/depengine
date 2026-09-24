package graph

import (
	"fmt"
	"sort"
)

// DeclaredTool is the minimal host-independent projection required to build
// the declared graph IR.
//
// It intentionally reuses graph-facing methods already implemented by the
// config model, keeping this package independent from internal/config.
type DeclaredTool interface {
	GraphDependencies() []string
	GraphTags() []string
	GraphConditionalDependencies() map[string][]string
}

type dependencyOnlyTool interface {
	GraphDependencyOnly() bool
}

// dependencyWalker is an optional richer boundary. Implementations can retain
// concrete guard values without making internal/graph depend on their package.
type dependencyWalker interface {
	GraphWalkDependencies(func(dependency string, guard fmt.Stringer))
}

// methodDependencyWalker preserves candidate-level method relations, including
// guards, without collapsing candidates through a map.
type methodDependencyWalker interface {
	GraphWalkMethodDependencies(func(candidate int, method string, dependencies []string, guard fmt.Stringer))
}

// BuildDeclaredGraph creates the host-independent declared dependency graph.
//
// The map key is the canonical tool ID, matching Sort and the existing
// renderers. Tool-level requirements are scheduling edges. Method-scoped
// requirements are activation edges and remain semantic multiedges.
//
// Rich walkers are used when the source model provides them so declared guards
// and candidate identity survive in the IR. The older dependency methods remain
// as a compatibility fallback for other graph callers.
func BuildDeclaredGraph[T DeclaredTool](tools map[string]T) Graph {
	graph := NewGraph()

	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tool := tools[name]
		dependencyOnly := false
		if metadata, ok := any(tool).(dependencyOnlyTool); ok {
			dependencyOnly = metadata.GraphDependencyOnly()
		}
		graph.AddNode(Node{
			ID:             name,
			Tags:           tool.GraphTags(),
			DependencyOnly: dependencyOnly,
		})
	}

	for _, name := range names {
		tool := tools[name]

		if walker, ok := any(tool).(dependencyWalker); ok {
			walker.GraphWalkDependencies(func(dependency string, guard fmt.Stringer) {
				graph.AddEdge(Edge{
					From:  dependency,
					To:    name,
					Kind:  ToolRequire,
					Role:  Scheduling,
					Guard: guard,
				})
			})
		} else {
			for _, dependency := range tool.GraphDependencies() {
				graph.AddEdge(Edge{
					From: dependency,
					To:   name,
					Kind: ToolRequire,
					Role: Scheduling,
				})
			}
		}

		if walker, ok := any(tool).(methodDependencyWalker); ok {
			walker.GraphWalkMethodDependencies(func(candidate int, method string, dependencies []string, guard fmt.Stringer) {
				for _, dependency := range dependencies {
					graph.AddEdge(Edge{
						From:           dependency,
						To:             name,
						Kind:           MethodRequire,
						Role:           Activation,
						Guard:          guard,
						Method:         method,
						Candidate:      candidate,
						CandidateKnown: true,
					})
				}
			})
			continue
		}

		conditional := tool.GraphConditionalDependencies()
		methods := make([]string, 0, len(conditional))
		for method := range conditional {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			for _, dependency := range conditional[method] {
				graph.AddEdge(Edge{
					From:   dependency,
					To:     name,
					Kind:   MethodRequire,
					Role:   Activation,
					Method: method,
				})
			}
		}
	}

	return graph.Canonicalize()
}
