package graph

import (
	"strings"
	"testing"

	"depengine/pkg/schema"
)

func TestRenderMermaid(t *testing.T) {
	tools := map[string]*schema.Tool{
		"a": {Name: "a", Requires: []string{"b"}},
		"b": {Name: "b", Requires: []string{"c"}},
		"c": {Name: "c"},
	}
	levels := [][]string{{"c"}, {"b"}, {"a"}}

	got := RenderMermaid(levels, tools)

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
	tools := map[string]*schema.Tool{
		"a": {Name: "a", Requires: []string{"b"}},
		"b": {Name: "b", Requires: []string{"c"}},
		"c": {Name: "c"},
	}
	levels := [][]string{{"c"}, {"b"}, {"a"}}

	got := RenderDOT(levels, tools)

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

func TestRenderText(t *testing.T) {
	levels := [][]string{{"c"}, {"b"}, {"a"}}
	tools := map[string]*schema.Tool{}

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

func TestRenderTextSortsLevels(t *testing.T) {
	// Tools within a level must be sorted alphabetically.
	levels := [][]string{{"z", "a", "m"}}
	want := "level 0: a, m, z\n"
	got := RenderText(levels, map[string]*schema.Tool{})
	if got != want {
		t.Errorf("RenderText should sort tools within level:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRenderTextEmpty(t *testing.T) {
	got := RenderText(nil, nil)
	if got != "" {
		t.Errorf("RenderText(nil) should be empty, got: %q", got)
	}
	got = RenderText([][]string{}, map[string]*schema.Tool{})
	if got != "" {
		t.Errorf("RenderText([][]string{}) should be empty, got: %q", got)
	}
}

func TestRenderNoEdges(t *testing.T) {
	// Two tools with no dependencies — no edges expected.
	tools := map[string]*schema.Tool{
		"a": {Name: "a"},
		"b": {Name: "b"},
	}
	levels := [][]string{{"a", "b"}}

	mermaid := RenderMermaid(levels, tools)
	if mermaid != "graph TD\n" {
		t.Errorf("RenderMermaid with no deps should only have header:\n%s", mermaid)
	}

	dot := RenderDOT(levels, tools)
	if dot != "digraph depengine {\n}\n" {
		t.Errorf("RenderDOT with no deps should only have wrapper:\n%s", dot)
	}
}

func TestRenderDeterministic(t *testing.T) {
	// Same input must produce identical output every time.
	tools := map[string]*schema.Tool{
		"z": {Name: "z", Requires: []string{"a", "m"}},
		"a": {Name: "a", Requires: []string{"b"}},
		"b": {Name: "b"},
		"m": {Name: "m", Requires: []string{"b"}},
	}
	levels := [][]string{{"b"}, {"a", "m"}, {"z"}}

	m1 := RenderMermaid(levels, tools)
	m2 := RenderMermaid(levels, tools)
	if m1 != m2 {
		t.Errorf("RenderMermaid not deterministic:\n%s\nvs\n%s", m1, m2)
	}

	d1 := RenderDOT(levels, tools)
	d2 := RenderDOT(levels, tools)
	if d1 != d2 {
		t.Errorf("RenderDOT not deterministic:\n%s\nvs\n%s", d1, d2)
	}

	t1 := RenderText(levels, tools)
	t2 := RenderText(levels, tools)
	if t1 != t2 {
		t.Errorf("RenderText not deterministic:\n%s\nvs\n%s", t1, t2)
	}
}
