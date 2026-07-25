package graph

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
)

func tool(name string, requires ...string) *config.Tool {
	return &config.Tool{
		Name:     name,
		Requires: requires,
	}
}

func TestSortLinearDependency(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": tool("a", "b"),
		"b": tool("b", "c"),
		"c": tool("c"),
	}
	levels, err := Sort(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("expected 3 levels, got %d: %v", len(levels), levels)
	}
	// Level 0: c (no deps)
	if len(levels[0]) != 1 || levels[0][0] != "c" {
		t.Fatalf("level 0 should be [c], got %v", levels[0])
	}
	// Level 1: b (depends on c)
	if len(levels[1]) != 1 || levels[1][0] != "b" {
		t.Fatalf("level 1 should be [b], got %v", levels[1])
	}
	// Level 2: a (depends on b)
	if len(levels[2]) != 1 || levels[2][0] != "a" {
		t.Fatalf("level 2 should be [a], got %v", levels[2])
	}
}

func TestSortDAG(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": tool("a", "b", "c"),
		"b": tool("b", "d"),
		"c": tool("c", "d"),
		"d": tool("d"),
	}
	levels, err := Sort(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) < 2 {
		t.Fatalf("expected at least 2 levels, got %d: %v", len(levels), levels)
	}
	// Level 0: d
	if len(levels[0]) != 1 || levels[0][0] != "d" {
		t.Fatalf("level 0 should be [d], got %v", levels[0])
	}
	// Level 1: b and c (deps satisfied by d)
	level1 := make(map[string]bool)
	for _, t := range levels[1] {
		level1[t] = true
	}
	if !level1["b"] || !level1["c"] {
		t.Fatalf("level 1 should contain b and c, got %v", levels[1])
	}
	// Last level: a
	last := levels[len(levels)-1]
	if len(last) != 1 || last[0] != "a" {
		t.Fatalf("last level should be [a], got %v", last)
	}
}

func TestSortNoDeps(t *testing.T) {
	tools := map[string]*config.Tool{
		"x": tool("x"),
		"y": tool("y"),
		"z": tool("z"),
	}
	levels, err := Sort(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 1 {
		t.Fatalf("expected 1 level, got %d: %v", len(levels), levels)
	}
	if len(levels[0]) != 3 {
		t.Fatalf("expected 3 tools in level 0, got %d", len(levels[0]))
	}
}

func TestSortCycle(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": tool("a", "b"),
		"b": tool("b", "c"),
		"c": tool("c", "a"),
	}
	_, err := Sort(tools)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	_, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected *CycleError, got %T: %v", err, err)
	}
	// Error message should name the tools in the cycle.
	msg := err.Error()
	if len(msg) == 0 {
		t.Fatal("empty error message")
	}
}

func TestSortSelfCycle(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": tool("a", "a"),
	}
	_, err := Sort(tools)
	if err == nil {
		t.Fatal("expected cycle error for self-dependency, got nil")
	}
	_, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected *CycleError, got %T: %v", err, err)
	}
}

func TestSortMissingDependency(t *testing.T) {
	tools := map[string]*config.Tool{
		"a": tool("a", "nonexistent"),
	}
	_, err := Sort(tools)
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}
}

func TestSortEmptyReturnsEmpty(t *testing.T) {
	levels, err := Sort(map[string]*config.Tool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(levels) != 0 {
		t.Fatalf("expected 0 levels for empty input, got %d", len(levels))
	}
}
