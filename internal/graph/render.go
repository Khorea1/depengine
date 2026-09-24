package graph

import (
	"fmt"
	"sort"
	"strings"
)

type renderNode interface {
	DeclaredTool
}

// RenderMermaid returns a Mermaid flowchart string from the tool dependency map.
//
// This compatibility entry point builds the declared graph IR first so Mermaid,
// DOT, and future renderers consume the same semantic edge model.
func RenderMermaid[T renderNode](tools map[string]T) string {
	return RenderMermaidGraph(BuildDeclaredGraph(tools))
}

// RenderMermaidGraph renders a typed graph as a Mermaid flowchart.
//
// Arrows point from dependencies to the tools that require them, showing
// installation precedence. Method-scoped activation edges remain dashed.
func RenderMermaidGraph(graph Graph) string {
	var b strings.Builder
	b.WriteString("graph TD\n")

	for _, edge := range graph.Canonicalize().Edges {
		label := edgeLabel(edge)
		guard := guardLabel(edge.Guard)
		switch {
		case edge.Kind == MethodRequire:
			if label == "" {
				fmt.Fprintf(&b, "  %s -.-> %s\n", edge.From, edge.To)
			} else {
				fmt.Fprintf(&b, "  %s -.->|%s| %s\n", edge.From, label, edge.To)
			}
		case guard != "":
			fmt.Fprintf(&b, "  %s -.->|%s| %s\n", edge.From, guard, edge.To)
		default:
			fmt.Fprintf(&b, "  %s --> %s\n", edge.From, edge.To)
		}
	}

	return b.String()
}

// RenderDOT returns a Graphviz DOT format string from the tool dependency map.
func RenderDOT[T renderNode](tools map[string]T) string {
	return RenderDOTGraph(BuildDeclaredGraph(tools))
}

// RenderDOTGraph renders a typed graph in Graphviz DOT format.
//
// Activation-only method edges use constraint=false: they are visible semantic
// relations but must not influence scheduling/rank layout.
func RenderDOTGraph(graph Graph) string {
	var b strings.Builder
	b.WriteString("digraph depengine {\n")

	for _, edge := range graph.Canonicalize().Edges {
		label := edgeLabel(edge)
		guard := guardLabel(edge.Guard)
		switch {
		case edge.Kind == MethodRequire:
			fmt.Fprintf(&b, "  %q -> %q [style=dashed", edge.From, edge.To)
			if label != "" {
				fmt.Fprintf(&b, ",label=%q", label)
			}
			b.WriteString(",constraint=false];\n")
		case guard != "":
			fmt.Fprintf(&b, "  %q -> %q [style=dashed,label=%q];\n", edge.From, edge.To, guard)
		default:
			fmt.Fprintf(&b, "  %q -> %q;\n", edge.From, edge.To)
		}
	}

	b.WriteString("}\n")
	return b.String()
}

// RenderText returns the current level-oriented human-readable representation.
//
// The compatibility entry point builds the declared graph IR first so tags and
// method-scoped relations come from the same model as DOT and Mermaid.
func RenderText[T renderNode](levels [][]string, tools map[string]T) string {
	return RenderTextGraph(levels, BuildDeclaredGraph(tools))
}

// RenderTextGraph renders topological levels plus annotated declared relations
// from a typed graph.
func RenderTextGraph(levels [][]string, graph Graph) string {
	graph = graph.Canonicalize()

	var b strings.Builder
	for i, level := range levels {
		sorted := append([]string(nil), level...)
		sort.Strings(sorted)
		for j, name := range sorted {
			if node, ok := graph.Nodes[name]; ok && len(node.Tags) > 0 {
				sorted[j] = name + " (" + strings.Join(node.Tags, ",") + ")"
			}
		}
		fmt.Fprintf(&b, "level %d: %s\n", i, strings.Join(sorted, ", "))
	}

	for _, edge := range graph.Edges {
		switch {
		case edge.Kind == MethodRequire:
			fmt.Fprintf(&b, "conditional: %s -[%s]-> %s\n", edge.From, edgeLabel(edge), edge.To)
		case edge.Guard != nil:
			fmt.Fprintf(&b, "conditional: %s -[%s]-> %s\n", edge.From, guardLabel(edge.Guard), edge.To)
		}
	}
	return b.String()
}

func edgeLabel(edge Edge) string {
	guard := guardLabel(edge.Guard)
	if edge.Method == "" {
		return guard
	}
	if guard == "" {
		return edge.Method
	}
	return edge.Method + "; " + guard
}
