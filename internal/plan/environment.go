package plan

import (
	"fmt"
	"strings"
)

// EnvironmentKind distinguishes environment/profile targeting semantics that
// are materially different across ecosystems. Scope remains separate: scope is
// about user/system authority and placement, while environment identifies the
// logical target inside that scope.
type EnvironmentKind string

const (
	EnvironmentNamed   EnvironmentKind = "named"
	EnvironmentPrefix  EnvironmentKind = "prefix"
	EnvironmentProfile EnvironmentKind = "profile"
	EnvironmentProject EnvironmentKind = "project"
)

// EnvironmentTarget is the adapter-neutral identity of an explicitly selected
// environment/profile. Value is interpreted according to Kind by the adapter;
// the planner never substitutes the invoking shell's active environment.
type EnvironmentTarget struct {
	Kind  EnvironmentKind `json:"kind"`
	Value string          `json:"value"`
}

// Validate checks only adapter-neutral invariants. Path-like prefix/project
// values are not validated with filepath.IsAbs because plans may target an OS
// different from the planner host; target-specific validation belongs in the
// adapter/resolver.
func (e EnvironmentTarget) Validate() error {
	switch e.Kind {
	case EnvironmentNamed, EnvironmentPrefix, EnvironmentProfile, EnvironmentProject:
	default:
		return fmt.Errorf("unsupported environment target kind %q", e.Kind)
	}
	if e.Value == "" {
		return fmt.Errorf("environment target %q requires a value", e.Kind)
	}
	if strings.TrimSpace(e.Value) != e.Value {
		return fmt.Errorf("environment target %q has surrounding whitespace", e.Kind)
	}
	if strings.ContainsRune(e.Value, '\x00') {
		return fmt.Errorf("environment target %q contains NUL", e.Kind)
	}
	return nil
}

// CanonicalKey returns a stable comparison/debug identity without conflating
// different target kinds that happen to use the same value.
func (e EnvironmentTarget) CanonicalKey() string {
	if e.Kind == "" && e.Value == "" {
		return ""
	}
	return string(e.Kind) + ":" + e.Value
}
