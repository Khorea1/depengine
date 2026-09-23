package exec

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// methodForResolvedTarget projects the selected plan target into the legacy
// candidate consumed by Observe, Check, InstalledVersion, and Remove. Keep the
// original candidate untouched: InstallResolved already reads the plan itself.
func methodForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) *config.MethodCandidate {
	if method == nil || resolved == nil || (method.Kind != "cargo" && method.Kind != "conda") {
		return method
	}
	copy := *method
	copy.Config = make(map[string]any, len(method.Config))
	for key, value := range method.Config {
		copy.Config[key] = value
	}
	target := resolved.Identity.Environment
	switch method.Kind {
	case "cargo":
		delete(copy.Config, "root")
		if target != nil && target.Kind == plan.EnvironmentPrefix {
			copy.Config["root"] = target.Value
		}
	case "conda":
		delete(copy.Config, "environment")
		delete(copy.Config, "prefix")
		if target != nil {
			if target.Kind == plan.EnvironmentPrefix {
				copy.Config["prefix"] = target.Value
			} else if target.Kind == plan.EnvironmentNamed {
				copy.Config["environment"] = target.Value
			}
		}
	}
	return &copy
}

func configForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) map[string]any {
	return methodForResolvedTarget(method, resolved).Config
}
