package exec

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/planner"
)

// candidatePlanIntent is the shared static planning boundary for execution and
// explain. It deliberately performs no host probes or mutations.
func candidatePlanIntent(tool *config.Tool, method *config.MethodCandidate) (*plan.ResolvedInstallPlan, string) {
	if method == nil {
		return nil, ""
	}
	contract, ok := methodkind.Lookup(method.Kind)
	if !ok {
		return nil, ""
	}
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		return nil, fmt.Sprintf("method %q has invalid plan intent: %v", method.Kind, err)
	}
	missing, err := contract.MissingPlanCapabilities(intent)
	if err != nil {
		return &intent, fmt.Sprintf("method %q has invalid plan intent: %v", method.Kind, err)
	}
	if missing == 0 {
		return &intent, ""
	}
	return &intent, fmt.Sprintf("method %q cannot honor requested capabilities: %s", method.Kind, strings.Join(methodkind.CapabilityNames(missing), ", "))
}
