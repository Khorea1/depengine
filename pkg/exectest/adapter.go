// Package exectest provides test helpers for pkg/exec: a mock adapter and
// a helper to build minimal schemas inline.
package exectest

import (
	"context"
	"fmt"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
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
func MockSchema(tools ...string) *schema.Schema {
	s := &schema.Schema{
		Defaults: schema.Defaults{
			Manager:     "native",
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*schema.Tool{},
	}
	for _, name := range tools {
		s.Tools[name] = &schema.Tool{
			Name:     name,
			IsSimple: true,
			Methods: []*schema.MethodCandidate{
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
func MockSchemaWithMethod(kind string, tools ...string) *schema.Schema {
	s := &schema.Schema{
		Defaults: schema.Defaults{
			Manager:     kind,
			MethodOrder: []string{kind},
		},
		Tools: map[string]*schema.Tool{},
	}
	for _, name := range tools {
		s.Tools[name] = &schema.Tool{
			Name: name,
			Methods: []*schema.MethodCandidate{
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
func MockTool(name string, methods ...*schema.MethodCandidate) *schema.Tool {
	return &schema.Tool{
		Name:    name,
		Methods: methods,
	}
}

// MockMethod returns a method candidate for testing.
func MockMethod(kind string, when *schema.Condition) *schema.MethodCandidate {
	return &schema.MethodCandidate{
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

func (m *MockAdapter) Check(_ context.Context, _ run.Runner, tool *schema.Tool, _ *schema.MethodCandidate) bool {
	m.Calls = append(m.Calls, MockCall{Method: "Check", Tool: tool.Name})
	if m.CheckFunc != nil {
		return m.CheckFunc(tool.Name)
	}
	return false
}

func (m *MockAdapter) Install(_ context.Context, _ run.Runner, tool *schema.Tool, _ *schema.MethodCandidate) error {
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
func WithRequires(s *schema.Schema, toolName string, requires ...string) *schema.Schema {
	if t, ok := s.Tools[toolName]; ok {
		t.Requires = append(t.Requires, requires...)
	}
	return s
}

// WithPostInstall adds a postinstall script to a tool.
func WithPostInstall(s *schema.Schema, toolName, script string) *schema.Schema {
	if t, ok := s.Tools[toolName]; ok {
		t.PostInstall = script
	}
	return s
}

// MustSort is a test helper that calls graph.Sort and panics on error.
func MustSort(tools map[string]*schema.Tool) [][]string {
	levels, err := graphSort(tools)
	if err != nil {
		panic(fmt.Sprintf("graph.Sort: %v", err))
	}
	return levels
}

// graphSort is a shim imported here so exectest doesn't force its callers
// to depend on pkg/graph. Tests that need cycle-specific behavior (e.g.,
// CycleError assertions) should import pkg/graph directly.
func graphSort(tools map[string]*schema.Tool) ([][]string, error) {
	return sortFunc(tools)
}

// sortFunc exists as a package-level var so tests can stub dependency
// sorting (e.g., return a fixed order). Reassign in test setup.
var sortFunc = func(tools map[string]*schema.Tool) ([][]string, error) {
	inDegree := map[string]int{}
	children := map[string][]string{}
	for name, t := range tools {
		inDegree[name] = len(t.Requires)
		for _, dep := range t.Requires {
			children[dep] = append(children[dep], name)
		}
	}
	var levels [][]string
	remaining := map[string]bool{}
	for name := range tools {
		remaining[name] = true
	}
	for len(remaining) > 0 {
		var level []string
		for name := range remaining {
			if inDegree[name] == 0 {
				level = append(level, name)
			}
		}
		if len(level) == 0 {
			return nil, fmt.Errorf("graph cycle detected")
		}
		for _, name := range level {
			delete(remaining, name)
			for _, child := range children[name] {
				inDegree[child]--
			}
		}
		levels = append(levels, level)
	}
	return levels, nil
}
