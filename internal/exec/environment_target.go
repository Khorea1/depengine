package exec

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// methodForResolvedTarget projects the selected plan target into the legacy
// candidate consumed by Observe, Check, InstalledVersion, and Remove. Keep the
// original candidate untouched: InstallResolved already reads the plan itself.
func methodForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) *config.MethodCandidate {
	if method == nil || resolved == nil {
		return method
	}

	contract, ok := methodkind.Lookup(method.Kind)
	if !ok || contract.Environment == nil {
		return method
	}

	copy := *method
	copy.Config = make(map[string]any, len(method.Config))
	for key, value := range method.Config {
		copy.Config[key] = value
	}

	for _, field := range contract.Environment.Fields() {
		delete(copy.Config, field)
	}

	target := resolved.Identity.Environment
	if target != nil {
		if field, ok := contract.Environment.FieldFor(target.Kind); ok {
			copy.Config[field] = target.Value
		}
	}

	return &copy
}

func configForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) map[string]any {
	return methodForResolvedTarget(method, resolved).Config
}
