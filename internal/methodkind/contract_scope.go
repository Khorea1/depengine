package methodkind

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/plan"
)

// SupportsScope reports whether the method can represent a portable scope.
func (c Contract) SupportsScope(scope plan.Scope) bool {
	if !c.Supports(CapabilityScope) || c.Scopes == nil {
		return false
	}
	_, ok := c.Scopes.AdapterValues[scope]
	return ok
}

// AdapterScope resolves canonical planner scope into adapter-native spelling.
func (c Contract) AdapterScope(scope plan.Scope) (string, error) {
	if !c.Supports(CapabilityScope) {
		return "", fmt.Errorf("method %q does not support installation scope", c.Kind)
	}
	value, err := c.Scopes.AdapterValue(scope)
	if err != nil {
		return "", fmt.Errorf("method %q: %w", c.Kind, err)
	}
	return value, nil
}

// NormalizeScope maps adapter-native spelling to canonical planner identity.
func (c Contract) NormalizeScope(raw string) (plan.Scope, error) {
	if !c.Supports(CapabilityScope) {
		return "", fmt.Errorf("method %q does not support installation scope", c.Kind)
	}
	scope, err := c.Scopes.Normalize(raw)
	if err != nil {
		return "", fmt.Errorf("method %q: %w", c.Kind, err)
	}
	return scope, nil
}
