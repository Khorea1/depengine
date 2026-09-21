package graph

import (
	"fmt"
	"sort"
	"strings"
)

type renderNode interface {
	node
	GraphTags() []string
	GraphConditionalDependencies() map[string][]string
}

// RenderMermaid returns a Mermaid flowchart string from the tool dependency
// map. It produces:
//
//	graph TD
//	  dep --> tool
//	  ...
//
// Arrows point from dependencies to the tools that require them,
// showing the installation order (dependencies first).
func RenderMermaid[T renderNode](tools map[string]T) string {
	var b strings.Builder
	b.WriteString("graph TD\n")

	edges := collectEdges(tools)
	for _, e := range edges {
		if e.conditional {
			fmt.Fprintf(&b, "  %s -.->|%s| %s\n", e.dep, e.method, e.tool)
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", e.dep, e.tool)
		}
	}

	return b.String()
}

// RenderDOT returns a Graphviz DOT format string:
//
//	digraph depengine {
//	  "tool_a" -> "tool_b";
//	  ...
//	}
//
// Arrows point from dependencies to the tools that require them.
func RenderDOT[T renderNode](tools map[string]T) string {
	var b strings.Builder
	b.WriteString("digraph depengine {\n")

	edges := collectEdges(tools)
	for _, e := range edges {
		if e.conditional {
			fmt.Fprintf(&b, "  %q -> %q [style=dashed,label=%q];\n", e.dep, e.tool, e.method)
		} else {
			fmt.Fprintf(&b, "  %q -> %q;\n", e.dep, e.tool)
		}
	}

	b.WriteString("}\n")
	return b.String()
}

// RenderText returns a simple text representation:
//
//	level 0: tool_c, tool_d
//	level 1: tool_b
//	level 2: tool_a
//
// Tools within each level are sorted alphabetically.
// When tools map is provided and a tool has tags, they are shown in parentheses.
func RenderText[T renderNode](levels [][]string, tools map[string]T) string {
	var b strings.Builder
	for i, level := range levels {
		sorted := make([]string, len(level))
		copy(sorted, level)
		sort.Strings(sorted)
		for j, name := range sorted {
			if t, ok := tools[name]; ok && len(t.GraphTags()) > 0 {
				sorted[j] = name + " (" + strings.Join(t.GraphTags(), ",") + ")"
			}
		}
		fmt.Fprintf(&b, "level %d: %s\n", i, strings.Join(sorted, ", "))
	}
	for _, edge := range collectEdges(tools) {
		if edge.conditional {
			fmt.Fprintf(&b, "conditional: %s -[%s]-> %s\n", edge.dep, edge.method, edge.tool)
		}
	}
	return b.String()
}

type edge struct {
	tool        string // tool that has the dependency
	dep         string // the dependency
	conditional bool
	method      string
}

// collectEdges builds a sorted list of edges from the tools map.
// Each edge represents "tool requires dep", rendered as dep --> tool.
func collectEdges[T renderNode](tools map[string]T) []edge {
	var edges []edge
	for name, tool := range tools {
		for _, dep := range tool.GraphDependencies() {
			edges = append(edges, edge{tool: name, dep: dep})
		}
		for method, dependencies := range tool.GraphConditionalDependencies() {
			for _, dependency := range dependencies {
				edges = append(edges, edge{tool: name, dep: dependency, conditional: true, method: method})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].tool != edges[j].tool {
			return edges[i].tool < edges[j].tool
		}
		if edges[i].dep != edges[j].dep {
			return edges[i].dep < edges[j].dep
		}
		if edges[i].conditional != edges[j].conditional {
			return !edges[i].conditional
		}
		return edges[i].method < edges[j].method
	})
	return edges
}
