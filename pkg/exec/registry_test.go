package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

// mockAdapter is a minimal adapter for testing the registry.
type mockAdapter struct {
	kindValue string
}

func (m *mockAdapter) Kind() string                               { return m.kindValue }
func (m *mockAdapter) Available(context.Context, run.Runner) bool { return true }
func (m *mockAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return false
}
func (m *mockAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func TestRegisterAndLookup(t *testing.T) {
	// Save and restore global registry.
	saved := adapters
	adapters = map[string]Adapter{}
	defer func() { adapters = saved }()

	m := &mockAdapter{kindValue: "test-kind"}
	Register(m)

	got := Lookup("test-kind")
	if got == nil {
		t.Fatal("Lookup returned nil for registered adapter")
	}
	if got.Kind() != "test-kind" {
		t.Fatalf("Lookup returned kind %q, want %q", got.Kind(), "test-kind")
	}

	// Looking up an unregistered kind returns nil.
	if unreg := Lookup("nope"); unreg != nil {
		t.Fatalf("Lookup for unregistered kind should be nil, got %v", unreg)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	saved := adapters
	adapters = map[string]Adapter{}
	defer func() { adapters = saved }()

	Register(&mockAdapter{kindValue: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate register, got none")
		}
	}()

	Register(&mockAdapter{kindValue: "dup"})
}

func TestRegisteredKinds(t *testing.T) {
	saved := adapters
	adapters = map[string]Adapter{}
	defer func() { adapters = saved }()

	Register(&mockAdapter{kindValue: "a"})
	Register(&mockAdapter{kindValue: "b"})

	kinds := RegisteredKinds()
	if len(kinds) != 2 {
		t.Fatalf("expected 2 kinds, got %d: %v", len(kinds), kinds)
	}

	seen := map[string]bool{}
	for _, k := range kinds {
		seen[k] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("RegisteredKinds missing kinds: got %v", kinds)
	}
}

func TestReplace(t *testing.T) {
	saved := adapters
	adapters = map[string]Adapter{}
	defer func() { adapters = saved }()

	original := &mockAdapter{kindValue: "repl"}
	Register(original)

	if got := Lookup("repl"); got == nil {
		t.Fatal("Lookup returned nil after Register")
	}

	// Replace with a new adapter of the same kind (should not panic).
	replacement := &mockAdapter{kindValue: "repl"}
	Replace(replacement)

	got := Lookup("repl")
	if got == nil {
		t.Fatal("Lookup returned nil after Replace")
	}
	// Register with the same kind would panic, proving Replace didn't panic.
}

func TestReplaceOnUnregisteredKind(t *testing.T) {
	saved := adapters
	adapters = map[string]Adapter{}
	defer func() { adapters = saved }()

	// Replace on a kind that was never registered should work (silent insert).
	a := &mockAdapter{kindValue: "new-kind"}
	Replace(a)

	if got := Lookup("new-kind"); got == nil {
		t.Fatal("Replace should insert when kind is not yet registered")
	}
}
