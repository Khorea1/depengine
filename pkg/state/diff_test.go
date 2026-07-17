package state

import (
	"testing"
)

func TestDiffBothEmpty(t *testing.T) {
	a := &State{Tools: map[string]ToolState{}}
	b := &State{Tools: map[string]ToolState{}}
	items := Diff(a, b)
	if len(items) != 0 {
		t.Fatalf("expected empty diff, got %d items", len(items))
	}
}

func TestDiffOnlyInA(t *testing.T) {
	a := &State{Tools: map[string]ToolState{
		"nvim": {Method: "native", DefinitionHash: "aaa"},
	}}
	b := &State{Tools: map[string]ToolState{}}
	items := Diff(a, b)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "nvim" || items[0].Side != "only_a" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	if items[0].MethodA != "native" || items[0].HashA != "aaa" {
		t.Fatalf("unexpected MethodA/HashA: %+v", items[0])
	}
}

func TestDiffOnlyInB(t *testing.T) {
	a := &State{Tools: map[string]ToolState{}}
	b := &State{Tools: map[string]ToolState{
		"bat": {Method: "cargo", DefinitionHash: "bbb"},
	}}
	items := Diff(a, b)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "bat" || items[0].Side != "only_b" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	if items[0].MethodB != "cargo" || items[0].HashB != "bbb" {
		t.Fatalf("unexpected MethodB/HashB: %+v", items[0])
	}
}

func TestDiffSameHash(t *testing.T) {
	a := &State{Tools: map[string]ToolState{
		"nvim": {Method: "native", DefinitionHash: "same"},
	}}
	b := &State{Tools: map[string]ToolState{
		"nvim": {Method: "native", DefinitionHash: "same"},
	}}
	items := Diff(a, b)
	if len(items) != 0 {
		t.Fatalf("expected no diff for matching hashes, got %d items", len(items))
	}
}

func TestDiffDifferentHash(t *testing.T) {
	a := &State{Tools: map[string]ToolState{
		"fd": {Method: "native", DefinitionHash: "hashA"},
	}}
	b := &State{Tools: map[string]ToolState{
		"fd": {Method: "cargo", DefinitionHash: "hashB"},
	}}
	items := Diff(a, b)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "fd" || items[0].Side != "different" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	if items[0].MethodA != "native" || items[0].MethodB != "cargo" {
		t.Fatalf("unexpected methods: %+v", items[0])
	}
	if items[0].HashA != "hashA" || items[0].HashB != "hashB" {
		t.Fatalf("unexpected hashes: %+v", items[0])
	}
}

func TestDiffMixed(t *testing.T) {
	a := &State{Tools: map[string]ToolState{
		"alpha": {Method: "native", DefinitionHash: "h1"},  // only_a
		"gamma": {Method: "cargo", DefinitionHash: "same"}, // match
		"delta": {Method: "native", DefinitionHash: "d1"},  // different
	}}
	b := &State{Tools: map[string]ToolState{
		"beta":  {Method: "pipx", DefinitionHash: "h2"},    // only_b
		"gamma": {Method: "cargo", DefinitionHash: "same"}, // match
		"delta": {Method: "go", DefinitionHash: "d2"},      // different
	}}
	items := Diff(a, b)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Should be sorted by name: alpha, beta, delta.
	want := []string{"alpha", "beta", "delta"}
	sides := []string{"only_a", "only_b", "different"}
	for i, w := range want {
		if items[i].Name != w {
			t.Fatalf("item %d: expected %s, got %s", i, w, items[i].Name)
		}
		if items[i].Side != sides[i] {
			t.Fatalf("item %d: expected side %s, got %s", i, sides[i], items[i].Side)
		}
	}
}

func TestDiffSortedByName(t *testing.T) {
	a := &State{Tools: map[string]ToolState{
		"zlib":  {Method: "native", DefinitionHash: "z"},
		"alpha": {Method: "native", DefinitionHash: "a"},
		"mid":   {Method: "native", DefinitionHash: "m"},
	}}
	b := &State{Tools: map[string]ToolState{}}
	items := Diff(a, b)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Name != "alpha" || items[1].Name != "mid" || items[2].Name != "zlib" {
		t.Fatalf("expected alphabetical order, got %s, %s, %s",
			items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestDiffBothNil(t *testing.T) {
	items := Diff(nil, nil)
	if len(items) != 0 {
		t.Fatalf("expected empty diff for nil inputs, got %d items", len(items))
	}
}

func TestDiffNilStateA(t *testing.T) {
	b := &State{Tools: map[string]ToolState{
		"bat": {Method: "cargo", DefinitionHash: "bbb"},
	}}
	items := Diff(nil, b)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Side != "only_b" {
		t.Fatalf("expected only_b, got %s", items[0].Side)
	}
}

func TestDiffNilStateB(t *testing.T) {
	a := &State{Tools: map[string]ToolState{
		"nvim": {Method: "native", DefinitionHash: "aaa"},
	}}
	items := Diff(a, nil)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Side != "only_a" {
		t.Fatalf("expected only_a, got %s", items[0].Side)
	}
}

func TestDiffNilToolsMap(t *testing.T) {
	a := &State{}
	b := &State{}
	items := Diff(a, b)
	if len(items) != 0 {
		t.Fatalf("expected empty diff for nil Tools maps, got %d items", len(items))
	}
}

func TestDiffOneNilToolsMap(t *testing.T) {
	a := &State{}
	b := &State{Tools: map[string]ToolState{
		"bat": {Method: "cargo", DefinitionHash: "bbb"},
	}}
	items := Diff(a, b)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Side != "only_b" {
		t.Fatalf("expected only_b, got %s", items[0].Side)
	}
}
