// Package exectest provides test helpers for pkg/exec: a mock adapter and
// a helper to build minimal schemas inline.
package exectest

import (
	"context"
	"fmt"
	"testing"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/graph"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

// MockAdapter is a fully configurable adapter for testing the executor.
// Each call is recorded in Calls for assertion.
type MockAdapter struct {
	KindValue     string
	AvailableFunc func() bool
	CheckFunc     func(tool string) bool
	InstallFunc   func(tool string) error
	Calls         []MockCall
}

// MockCall records one adapter invocation.
type MockCall struct {
	Method string // "Available" | "Check" | "Install"
	Tool   string
}

// MockSchema returns a minimal schema for the given tool names.
// Each tool gets a single native method with pkg == tool name.
func MockSchema(tools ...string) *config.Schema {
	s := &config.Schema{
		Defaults: config.Defaults{
			Manager:     "native",
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*config.Tool{},
	}
	for _, name := range tools {
		s.Tools[name] = &config.Tool{
			Name:     name,
			IsSimple: true,
			Methods: []*config.MethodCandidate{
				{
					Kind:   "native",
					Config: map[string]any{"pkg": name},
				},
			},
		}
	}
	return s
}

// MockSchemaWithMethod creates a schema where each tool has a single method
// of the specified kind. Useful for testing specific method types.
func MockSchemaWithMethod(kind string, tools ...string) *config.Schema {
	s := &config.Schema{
		Defaults: config.Defaults{
			Manager:     kind,
			MethodOrder: []string{kind},
		},
		Tools: map[string]*config.Tool{},
	}
	for _, name := range tools {
		s.Tools[name] = &config.Tool{
			Name: name,
			Methods: []*config.MethodCandidate{
				{
					Kind:   kind,
					Config: map[string]any{"pkg": name},
				},
			},
		}
	}
	return s
}

// MockTool returns a tool initialized for testing.
func MockTool(name string, methods ...*config.MethodCandidate) *config.Tool {
	return &config.Tool{
		Name:    name,
		Methods: methods,
	}
}

// MockMethod returns a method candidate for testing.
func MockMethod(kind string, when *config.Condition) *config.MethodCandidate {
	return &config.MethodCandidate{
		Kind:   kind,
		When:   when,
		Config: map[string]any{"pkg": "test-" + kind},
	}
}

// --- MockAdapter implements exec.Adapter ---

func (m *MockAdapter) Kind() string { return m.KindValue }

func (m *MockAdapter) Available(_ context.Context, _ run.Runner) bool {
	m.Calls = append(m.Calls, MockCall{Method: "Available"})
	if m.AvailableFunc != nil {
		return m.AvailableFunc()
	}
	return true
}

func (m *MockAdapter) Check(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) bool {
	m.Calls = append(m.Calls, MockCall{Method: "Check", Tool: tool.Name})
	if m.CheckFunc != nil {
		return m.CheckFunc(tool.Name)
	}
	return false
}

func (m *MockAdapter) Install(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) error {
	m.Calls = append(m.Calls, MockCall{Method: "Install", Tool: tool.Name})
	if m.InstallFunc != nil {
		return m.InstallFunc(tool.Name)
	}
	return nil
}

// Ensure MockAdapter implements exec.Adapter at compile time.
var _ exec.Adapter = (*MockAdapter)(nil)

// --- Schema helpers ---

// WithRequires adds a dependency to a tool in the schema.
func WithRequires(s *config.Schema, toolName string, requires ...string) *config.Schema {
	if t, ok := s.Tools[toolName]; ok {
		t.Requires = append(t.Requires, requires...)
	}
	return s
}

// WithPostInstall adds a postinstall script to a tool.
func WithPostInstall(s *config.Schema, toolName, script string) *config.Schema {
	if t, ok := s.Tools[toolName]; ok {
		t.PostInstall = script
	}
	return s
}

// MustSort is a test helper that calls graph.Sort and panics on error.
func MustSort(tools map[string]*config.Tool) [][]string {
	levels, err := graph.Sort(tools)
	if err != nil {
		panic(fmt.Sprintf("graph.Sort: %v", err))
	}
	return levels
}

// TestAdapterConformance verifies that an adapter satisfies the basic
// invariants of the exec.Adapter interface. Every adapter package should
// call this from its own test:
//
//	func TestConformance(t *testing.T) {
//	    exectest.TestAdapterConformance(t, myadapter.New())
//	}
func TestAdapterConformance(t *testing.T, a exec.Adapter) {
	t.Helper()

	t.Run("Kind_stable", func(t *testing.T) {
		k1 := a.Kind()
		if k1 == "" {
			t.Error("Kind() must not return empty string")
		}
		k2 := a.Kind()
		if k1 != k2 {
			t.Errorf("Kind() changed between calls: %q -> %q", k1, k2)
		}
	})

	t.Run("Available_not_nil_runner", func(t *testing.T) {
		ctx := context.Background()
		fr := &run.FakeRunner{}
		// Should never panic regardless of environment.
		_ = a.Available(ctx, fr)
	})

	t.Run("Check_no_panic", func(t *testing.T) {
		ctx := context.Background()
		fr := &run.FakeRunner{}
		tool := &config.Tool{Name: "nonexistent-conformance-check"}
		mc := &config.MethodCandidate{Kind: a.Kind(), Config: map[string]any{}}
		// Check must never panic with an unknown tool and empty config.
		_ = a.Check(ctx, fr, tool, mc)
	})

	t.Run("Install_empty_config_returns_error", func(t *testing.T) {
		ctx := context.Background()
		tool := &config.Tool{Name: "nonexistent-conformance-install"}
		mc := &config.MethodCandidate{Kind: a.Kind(), Config: map[string]any{}}
		err := a.Install(ctx, &run.FakeRunner{}, tool, mc)
		if err == nil {
			t.Error("Install with empty config should return an error")
		}
	})

	t.Run("Install_nil_runner_returns_error", func(t *testing.T) {
		ctx := context.Background()
		tool := &config.Tool{Name: "nonexistent-conformance-install-nil"}
		mc := &config.MethodCandidate{Kind: a.Kind(), Config: map[string]any{}}
		err := a.Install(ctx, nil, tool, mc)
		if err == nil {
			t.Error("Install with nil runner should return an error")
		}
	})

	t.Run("RegisteredKinds_contains_kind", func(t *testing.T) {
		found := false
		for _, k := range exec.RegisteredKinds() {
			if k == a.Kind() {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Kind %q not in exec.RegisteredKinds() — may need exec.Register() call", a.Kind())
		}
	})
}
