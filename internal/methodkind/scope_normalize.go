package methodkind

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
)

// Normalize maps an adapter-native scope spelling back to portable identity.
// Canonical portable spellings are also accepted when supported.
func (s *ScopeContract) Normalize(raw string) (plan.Scope, error) {
	if s == nil {
		return "", fmt.Errorf("portable scope is unsupported")
	}
	if strings.TrimSpace(raw) != raw || raw == "" {
		return "", fmt.Errorf("invalid scope %q", raw)
	}
	// Canonical spelling wins over adapter-native spelling so the result never
	// depends on map iteration order when the two vocabularies overlap.
	if portable := plan.Scope(raw); portable.Validate() == nil {
		if _, ok := s.AdapterValues[portable]; ok {
			return portable, nil
		}
	}
	matches := make([]plan.Scope, 0, 1)
	for portable, native := range s.AdapterValues {
		if raw == native {
			matches = append(matches, portable)
		}
	}
	switch len(matches) {
	case 0:
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("scope %q is ambiguous", raw)
	}
	return "", fmt.Errorf("scope %q has no portable mapping", raw)
}
