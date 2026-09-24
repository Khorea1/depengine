package graph

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestRenderMermaid(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": {Name: "a", Requires: []string{"b"}},
		"b": {Name: "b", Requires: []string{"c"}},
		"c": {Name: "c"},
	}

	got := RenderMermaid(tools)

	if !strings.HasPrefix(got, "graph TD\n") {
		t.Errorf("RenderMermaid should start with 'graph TD\\n':\n%s", got)
	}
	if !strings.Contains(got, "c --> b") {
		t.Errorf("RenderMermaid missing edge 'c --> b':\n%s", got)
	}
	if !strings.Contains(got, "b --> a") {
		t.Errorf("RenderMermaid missing edge 'b --> a':\n%s", got)
	}
}

func TestRenderDOT(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": {Name: "a", Requires: []string{"b"}},
		"b": {Name: "b", Requires: []string{"c"}},
		"c": {Name: "c"},
	}

	got := RenderDOT(tools)

	if !strings.HasPrefix(got, "digraph depengine {\n") {
		t.Errorf("RenderDOT should start with 'digraph depengine {\\n':\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "}") {
		t.Errorf("RenderDOT should end with '}':\n%s", got)
	}
	if !strings.Contains(got, `"c" -> "b"`) {
		t.Errorf("RenderDOT missing edge '\"c\" -> \"b\"':\n%s", got)
	}
	if !strings.Contains(got, `"b" -> "a"`) {
		t.Errorf("RenderDOT missing edge '\"b\" -> \"a\"':\n%s", got)
	}
}

func TestRenderGraphMethodEdgesRemainActivationOnly(t *testing.T) {
	graph := NewGraph()
	graph.AddNode(Node{ID: "app"})
	graph.AddNode(Node{ID: "curl"})
	graph.AddEdge(Edge{
		From:   "curl",
		To:     "app",
		Kind:   MethodRequire,
		Role:   Activation,
		Method: "http",
	})

	mermaid := RenderMermaidGraph(graph)
	if !strings.Contains(mermaid, "curl -.->|http| app") {
		t.Fatalf("method edge missing from Mermaid output:\n%s", mermaid)
	}

	dot := RenderDOTGraph(graph)
	if !strings.Contains(dot, `"curl" -> "app" [style=dashed,label="http",constraint=false];`) {
		t.Fatalf("method edge must be visible but non-constraining in DOT:\n%s", dot)
	}
}

func TestRenderGraphIncludesGuardMetadata(t *testing.T) {
	graph := NewGraph()
	graph.AddEdge(Edge{
		From:  "unzip",
		To:    "app",
		Kind:  ToolRequire,
		Role:  Scheduling,
		Guard: testGuard("target_family=unix"),
	})

	mermaid := RenderMermaidGraph(graph)
	if !strings.Contains(mermaid, "unzip -.->|target_family=unix| app") {
		t.Fatalf("guard missing from Mermaid output:\n%s", mermaid)
	}

	dot := RenderDOTGraph(graph)
	if !strings.Contains(dot, `"unzip" -> "app" [style=dashed,label="target_family=unix"];`) {
		t.Fatalf("guard missing from DOT output:\n%s", dot)
	}
}

func TestRenderDeclaredConfigGuards(t *testing.T) {
	tools := map[string]*config.Tool{
		"app": {
			Requires: []string{"unzip"},
			RequiresWhen: map[string]*config.Condition{
				"unzip": {TargetFamily: []string{"unix"}},
			},
		},
		"unzip": {},
	}

	got := RenderDOT(tools)
	if !strings.Contains(got, `"unzip" -> "app" [style=dashed,label="target_family in [\"unix\"]"];`) {
		t.Fatalf("declared requires_when guard missing from DOT:\n%s", got)
	}
}

func TestRenderText(t *testing.T) {
	levels := [][]string{{"c"}, {"b"}, {"a"}}
	tools := map[string]*config.Tool{}

	got := RenderText(levels, tools)

	if !strings.Contains(got, "level 0: c\n") {
		t.Errorf("RenderText missing 'level 0: c\\n':\n%s", got)
	}
	if !strings.Contains(got, "level 1: b\n") {
		t.Errorf("RenderText missing 'level 1: b\\n':\n%s", got)
	}
	if !strings.Contains(got, "level 2: a\n") {
		t.Errorf("RenderText missing 'level 2: a\\n':\n%s", got)
	}
}

func TestRenderTextUsesGraphNodeTags(t *testing.T) {
	graph := NewGraph()
	graph.AddNode(Node{ID: "app", Tags: []string{"cli", "dev"}})

	got := RenderTextGraph([][]string{{"app"}}, graph)
	if got != "level 0: app (cli,dev)\n" {
		t.Fatalf("unexpected tagged text output: %q", got)
	}
}

func TestRenderTextShowsGuardedToolDependencies(t *testing.T) {
	tools := map[string]*config.Tool{
		"app": {
			Requires: []string{"unzip"},
			RequiresWhen: map[string]*config.Condition{
				"unzip": {TargetFamily: []string{"unix"}},
			},
		},
		"unzip": {},
	}

	graph := BuildDeclaredGraph(tools)
	levels, err := SortGraph(graph)
	if err != nil {
		t.Fatalf("SortGraph: %v", err)
	}
	got := RenderTextGraph(levels, graph)
	if !strings.Contains(got, `conditional: unzip -[target_family in ["unix"]]-> app`) {
		t.Fatalf("guarded scheduling edge missing from text output:\n%s", got)
	}
}

func TestRenderTextSortsLevels(t *testing.T) {
	levels := [][]string{{"z", "a", "m"}}
	want := "level 0: a, m, z\n"
	got := RenderText(levels, map[string]*config.Tool{})
	if got != want {
		t.Errorf("RenderText should sort tools within level:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderTextEmpty(t *testing.T) {
	got := RenderText[*config.Tool](nil, nil)
	if got != "" {
		t.Errorf("RenderText(nil) should be empty, got: %q", got)
	}
	got = RenderText([][]string{}, map[string]*config.Tool{})
	if got != "" {
		t.Errorf("RenderText([][]string{}) should be empty, got: %q", got)
	}
}

func TestRenderNoEdges(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": {Name: "a"},
		"b": {Name: "b"},
	}

	mermaid := RenderMermaid(tools)
	if mermaid != "graph TD\n" {
		t.Errorf("RenderMermaid with no deps should only have header:\n%s", mermaid)
	}

	dot := RenderDOT(tools)
	if dot != "digraph depengine {\n}\n" {
		t.Errorf("RenderDOT with no deps should only have wrapper:\n%s", dot)
	}
}

func TestRenderDeterministic(t *testing.T) {
	tools := map[string]*config.Tool{
		"z": {Name: "z", Requires: []string{"a", "m"}},
		"a": {Name: "a", Requires: []string{"b"}},
		"b": {Name: "b"},
		"m": {Name: "m", Requires: []string{"b"}},
	}
	levels := [][]string{{"b"}, {"a", "m"}, {"z"}}

	m1 := RenderMermaid(tools)
	m2 := RenderMermaid(tools)
	if m1 != m2 {
		t.Errorf("RenderMermaid not deterministic:\n%s\nvs\n%s", m1, m2)
	}

	d1 := RenderDOT(tools)
	d2 := RenderDOT(tools)
	if d1 != d2 {
		t.Errorf("RenderDOT not deterministic:\n%s\nvs\n%s", d1, d2)
	}

	t1 := RenderText(levels, tools)
	t2 := RenderText(levels, tools)
	if t1 != t2 {
		t.Errorf("RenderText not deterministic:\n%s\nvs\n%s", t1, t2)
	}
}
