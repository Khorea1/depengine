package plan

import (
	"fmt"
	"strings"
)

// Scope is depengine's portable installation scope vocabulary. Adapter-native
// spellings such as "machine" or "global" must be normalized before they are
// stored in a resolved plan so desired-state identity is portable.
type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeSystem Scope = "system"
)

// ParseScope validates a portable scope value. Parsing is intentionally strict:
// surrounding whitespace and adapter-specific aliases are rejected so callers
// cannot accidentally persist non-canonical identity.
func ParseScope(raw string) (Scope, error) {
	if strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("scope %q has surrounding whitespace", raw)
	}
	scope := Scope(raw)
	if err := scope.Validate(); err != nil {
		return "", err
	}
	return scope, nil
}

// Validate reports whether scope is one of the portable canonical values.
func (s Scope) Validate() error {
	switch s {
	case ScopeUser, ScopeSystem:
		return nil
	case "":
		return fmt.Errorf("scope is required")
	default:
		return fmt.Errorf("unsupported portable scope %q", s)
	}
}
