// Package exectest provides test helpers for internal/exec: a mock adapter and
// a helper to build minimal schemas inline.
package exectest

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// MockAdapter is a fully configurable adapter for testing the executor.
// Each call is recorded in Calls for assertion.
type MockAdapter struct {
	KindValue           string
	AvailableFunc       func() bool
	CheckFunc           func(tool string) bool
	InstallFunc         func(tool string) error
	ResolvePlanFunc     func(tool *config.Tool, method *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error)
	ObserveFunc         func(tool *config.Tool, method *config.MethodCandidate) (plan.Observation, error)
	InstallResolvedFunc func(tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error
	CheckAvailableFunc  func(tool string) bool
	RemoveFunc          func(tool string) error
	CanRemoveValue      bool
	Calls               []MockCall
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

// --- MockAdapter implements exec.AdapterV2 ---

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

// Ensure MockAdapter implements exec.AdapterV2 at compile time.
func (m *MockAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, method *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if m.ResolvePlanFunc != nil {
		return m.ResolvePlanFunc(tool, method, intent)
	}
	if intent == nil {
		return nil, fmt.Errorf("nil plan intent")
	}
	resolved := intent.Clone()
	return &resolved, nil
}
func (m *MockAdapter) Observe(_ context.Context, _ run.Runner, tool *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	if m.ObserveFunc != nil {
		return m.ObserveFunc(tool, method)
	}
	if m.CheckFunc != nil && m.CheckFunc(tool.Name) {
		return plan.Observation{Presence: plan.PresencePresent}, nil
	}
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}
func (m *MockAdapter) InstallResolved(_ context.Context, _ run.Runner, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if m.InstallResolvedFunc != nil {
		return m.InstallResolvedFunc(tool, method, resolved)
	}
	if m.InstallFunc != nil {
		return m.InstallFunc(tool.Name)
	}
	return nil
}

func (m *MockAdapter) Remove(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) error {
	if m.RemoveFunc != nil {
		return m.RemoveFunc(tool.Name)
	}
	return fmt.Errorf("mock adapter removal unsupported")
}
func (m *MockAdapter) CanRemove() bool { return m.CanRemoveValue }
func (m *MockAdapter) CheckAvailable(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) bool {
	if m.CheckAvailableFunc != nil {
		return m.CheckAvailableFunc(tool.Name)
	}
	return true
}
func (*MockAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*MockAdapter)(nil)

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
		t.PostInstall = []config.Hook{{Run: []string{"sh", "-c", script}}}
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
// invariants of the exec.AdapterV2 interface. Every adapter package should
// call this from its own test:
//
//	func TestConformance(t *testing.T) {
//	    exectest.TestAdapterConformance(t, myadapter.New())
//	}
func TestAdapterConformance(t *testing.T, a exec.AdapterV2) {
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

	t.Run("Observe_valid_presence", func(t *testing.T) {
		observation, err := a.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "conformance"}, &config.MethodCandidate{Kind: a.Kind(), Config: map[string]any{}})
		if err != nil {
			t.Errorf("Observe(): %v", err)
		}
		if observation.Presence != plan.PresencePresent && observation.Presence != plan.PresenceAbsent && observation.Presence != plan.PresenceUnknown && observation.Presence != plan.PresenceBroken {
			t.Errorf("invalid presence %q", observation.Presence)
		}
	})

}

// SetHome points the process home at dir for the duration of the test.
// os.UserHomeDir ignores $HOME on Windows (it reads %USERPROFILE%), so
// tests that isolate the home directory must set both; otherwise the
// product resolves the real profile while the test asserts on the fake.
func SetHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
}
