package graph

import (
	"strings"
	"testing"
)

func TestRenderersIdentifyInactiveEdgesWithoutConstrainingLayout(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "app"})
	g.AddNode(Node{ID: "base"})
	g.AddEdge(Edge{From: "base", To: "app", Kind: ToolRequire, Role: Scheduling, State: InactiveEdge})

	levels, err := SortGraph(g)
	if err != nil {
		t.Fatalf("SortGraph: %v", err)
	}
	if len(levels) != 1 || len(levels[0]) != 2 || levels[0][0] != "app" || levels[0][1] != "base" {
		t.Fatalf("inactive edge affected levels: %#v", levels)
	}

	if got := RenderMermaidGraph(g); !strings.Contains(got, "base -.->|inactive| app") {
		t.Fatalf("inactive edge missing from Mermaid output:\n%s", got)
	}
	if got := RenderDOTGraph(g); !strings.Contains(got, `"base" -> "app" [style=dotted,label="inactive",constraint=false];`) {
		t.Fatalf("inactive edge must be visible and non-constraining in DOT:\n%s", got)
	}
	if got := RenderTextGraph(levels, g); !strings.Contains(got, "inactive: base -> app") {
		t.Fatalf("inactive edge missing from text output:\n%s", got)
	}

	terminal, err := RenderTerminalGraph(g, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	if !strings.Contains(terminal, "base -> app [inactive]") {
		t.Fatalf("inactive edge missing from terminal annotations:\n%s", terminal)
	}
}

func TestInactiveRenderLabelRetainsMethodAndGuardContext(t *testing.T) {
	edge := Edge{
		From:   "curl",
		To:     "app",
		Kind:   MethodRequire,
		Role:   Activation,
		State:  InactiveEdge,
		Method: "http",
		Guard:  testGuard("os = linux"),
	}
	if got, want := edgeRenderLabel(edge), "inactive; http; os = linux"; got != want {
		t.Fatalf("edgeRenderLabel() = %q, want %q", got, want)
	}
}
