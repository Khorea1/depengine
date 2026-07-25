package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
)

// RenderMermaid returns a Mermaid flowchart string from the topological levels
// and the tool dependency map. It produces:
//
//	graph TD
//	  level0_tool1 --> level1_toolA
//	  level0_tool2 --> level1_toolA
//	  ...
//
// Arrows point from dependencies to the tools that require them,
// showing the installation order (dependencies first).
func RenderMermaid(levels [][]string, tools map[string]*config.Tool) string {
	var b strings.Builder
	b.WriteString("graph TD\n")

	edges := collectEdges(tools)
	for _, e := range edges {
		fmt.Fprintf(&b, "  %s --> %s\n", e.dep, e.tool)
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
func RenderDOT(levels [][]string, tools map[string]*config.Tool) string {
	var b strings.Builder
	b.WriteString("digraph depengine {\n")

	edges := collectEdges(tools)
	for _, e := range edges {
		fmt.Fprintf(&b, "  %q -> %q;\n", e.dep, e.tool)
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
func RenderText(levels [][]string, tools map[string]*config.Tool) string {
	var b strings.Builder
	for i, level := range levels {
		sorted := make([]string, len(level))
		copy(sorted, level)
		sort.Strings(sorted)
		for j, name := range sorted {
			if t, ok := tools[name]; ok && len(t.Tags) > 0 {
				sorted[j] = name + " (" + strings.Join(t.Tags, ",") + ")"
			}
		}
		fmt.Fprintf(&b, "level %d: %s\n", i, strings.Join(sorted, ", "))
	}
	return b.String()
}

type edge struct {
	tool string // tool that has the dependency
	dep  string // the dependency
}

// collectEdges builds a sorted list of edges from the tools map.
// Each edge represents "tool requires dep", rendered as dep --> tool.
func collectEdges(tools map[string]*config.Tool) []edge {
	var edges []edge
	for name, tool := range tools {
		for _, dep := range tool.Requires {
			edges = append(edges, edge{tool: name, dep: dep})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].tool != edges[j].tool {
			return edges[i].tool < edges[j].tool
		}
		return edges[i].dep < edges[j].dep
	})
	return edges
}
