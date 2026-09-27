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
	if !ok || contract.Environment == nil && (method.Kind != "git" || resolved.Identity.Revision == "") {
		return method
	}

	methodCopy := *method
	methodCopy.Config = make(map[string]any, len(method.Config))
	for key, value := range method.Config {
		methodCopy.Config[key] = value
	}

	if contract.Environment != nil {
		for _, field := range contract.Environment.Fields() {
			delete(methodCopy.Config, field)
		}
		if target := resolved.Identity.Environment; target != nil {
			if field, ok := contract.Environment.FieldFor(target.Kind); ok {
				methodCopy.Config[field] = target.Value
			}
		}
	}
	if method.Kind == "git" && resolved.Identity.Revision != "" {
		delete(methodCopy.Config, "branch")
		delete(methodCopy.Config, "tag")
		methodCopy.Config["rev"] = resolved.Identity.Revision
	}

	return &methodCopy
}

func configForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) map[string]any {
	return methodForResolvedTarget(method, resolved).Config
}
