package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

// mockAdapter is a minimal AdapterV2 for testing the registry.
type mockAdapter struct {
	v2TestStub
	kindValue string
}

var _ AdapterV2 = (*mockAdapter)(nil)

func (*mockAdapter) Available(context.Context, run.Runner) bool { return true }
func (m *mockAdapter) Kind() string                             { return m.kindValue }

func TestRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	m := &mockAdapter{kindValue: "test-kind"}
	r.Register(m)

	got := r.Lookup("test-kind")
	if got == nil {
		t.Fatal("Lookup returned nil for registered adapter")
	}
	if got.Kind() != "test-kind" {
		t.Fatalf("Lookup returned kind %q, want %q", got.Kind(), "test-kind")
	}

	// Looking up an unregistered kind returns nil.
	if unreg := r.Lookup("nope"); unreg != nil {
		t.Fatalf("Lookup for unregistered kind should be nil, got %v", unreg)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{kindValue: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate register, got none")
		}
	}()

	r.Register(&mockAdapter{kindValue: "dup"})
}

func TestRegisteredKinds(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{kindValue: "a"})
	r.Register(&mockAdapter{kindValue: "b"})

	kinds := r.Kinds()
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
	r := NewRegistry()
	original := &mockAdapter{kindValue: "repl"}
	r.Register(original)

	if got := r.Lookup("repl"); got == nil {
		t.Fatal("Lookup returned nil after Register")
	}

	// Replace with a new adapter of the same kind (should not panic).
	replacement := &mockAdapter{kindValue: "repl"}
	r.Replace(replacement)

	got := r.Lookup("repl")
	if got == nil {
		t.Fatal("Lookup returned nil after Replace")
	}
	// Register with the same kind would panic, proving Replace didn't panic.
}

func TestReplaceOnUnregisteredKind(t *testing.T) {
	r := NewRegistry()

	// Replace on a kind that was never registered should work (silent insert).
	a := &mockAdapter{kindValue: "new-kind"}
	r.Replace(a)

	if got := r.Lookup("new-kind"); got == nil {
		t.Fatal("Replace should insert when kind is not yet registered")
	}
}

func TestRegistriesAreIndependent(t *testing.T) {
	a, b := NewRegistry(), NewRegistry()
	a.Register(&mockAdapter{kindValue: "only-a"})
	if got := b.Lookup("only-a"); got != nil {
		t.Fatalf("fresh registry sees another instance's adapter: %v", got)
	}
}
