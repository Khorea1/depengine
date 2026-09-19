package exec

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
)

// methodCapabilityMismatch is a defensive planner boundary. Parsed schemas
// normally reject fields a method does not understand, but programmatic callers
// can construct MethodCandidate values directly. Execution must never silently
// weaken semantic intent in that case.
func methodCapabilityMismatch(method *config.MethodCandidate) string {
	if method == nil {
		return ""
	}
	contract, ok := methodkind.Lookup(method.Kind)
	if !ok {
		return ""
	}
	missing := contract.MissingCapabilities(method.Config)
	if missing == 0 {
		return ""
	}
	return fmt.Sprintf("method %q cannot honor requested capabilities: %s", method.Kind, strings.Join(methodkind.CapabilityNames(missing), ", "))
}
