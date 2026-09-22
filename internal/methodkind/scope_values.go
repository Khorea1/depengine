package methodkind

import (
	"fmt"
	"sort"

	"github.com/Khorea1/depengine/internal/plan"
)

// PortableScopes returns supported canonical scopes in stable order.
func (s *ScopeContract) PortableScopes() []plan.Scope {
	if s == nil {
		return nil
	}
	out := make([]plan.Scope, 0, len(s.AdapterValues))
	for scope := range s.AdapterValues {
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AdapterValue resolves a portable scope to the adapter-native spelling.
func (s *ScopeContract) AdapterValue(scope plan.Scope) (string, error) {
	if s == nil {
		return "", fmt.Errorf("portable scope is unsupported")
	}
	if err := scope.Validate(); err != nil {
		return "", err
	}
	value, ok := s.AdapterValues[scope]
	if !ok {
		return "", fmt.Errorf("portable scope %q is unsupported", scope)
	}
	return value, nil
}
