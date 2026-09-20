package planner

import (
	"fmt"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
)

// applyScope projects the configured scope into canonical identity. A value
// outside the portable vocabulary is kept adapter-native only when the method
// owns its own scope semantics (e.g. gem "default"); otherwise the request
// cannot be honored and must not be dropped.
func applyScope(identity *plan.ResolvedIdentity, method *config.MethodCandidate, contract *methodkind.Contract) error {
	raw := stringValue(method.Config, "scope")
	if raw == "" {
		return nil
	}
	var scope plan.Scope
	var err error
	if contract.Scopes != nil {
		scope, err = contract.NormalizeScope(raw)
	} else {
		scope, err = plan.ParseScope(raw)
	}
	switch {
	case err == nil:
		identity.Scope = string(scope)
	case !contract.Supports(methodkind.CapabilityScope):
		return &plan.PlannerError{Class: plan.ErrorUnsupportedCapability, Op: "candidate", Err: fmt.Errorf("method %q cannot honor scope %q: %w", contract.Kind, raw, err)}
	}
	return nil
}

func applyEnvironment(identity *plan.ResolvedIdentity, method *config.MethodCandidate) {
	if value := stringValue(method.Config, "environment"); value != "" {
		identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: value}
		return
	}
	if value := firstValue(method.Config, "prefix", "root"); value != "" {
		identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: value}
	}
}
