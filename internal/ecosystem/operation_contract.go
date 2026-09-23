package ecosystem

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/plan"
)

func validateResolvedInstallOperation(adapter string, resolved *plan.ResolvedInstallPlan) error {
	if len(resolved.Operations) != 1 {
		return fmt.Errorf("%s: resolved operations are unsupported", adapter)
	}
	op := resolved.Operations[0]
	if op.Kind != "install" || op.Effect != plan.EffectMutation || op.Description != "" || op.Command != nil || op.ArbitraryCode {
		return fmt.Errorf("%s: resolved operations are unsupported", adapter)
	}
	return nil
}

func validateEnvironmentTarget(adapter string, target *plan.EnvironmentTarget, kinds ...plan.EnvironmentKind) error {
	if target == nil {
		return nil
	}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("%s: %w", adapter, err)
	}
	for _, kind := range kinds {
		if target.Kind == kind {
			return nil
		}
	}
	return fmt.Errorf("%s: unsupported environment target kind %q", adapter, target.Kind)
}
