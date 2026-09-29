package exec

import (
	"context"
	"fmt"
	"maps"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// RemoveResolvedCandidate removes the target described by a verified plan.
// It passes adapters a cloned method whose config projects the plan identity.
func (ex *Executor) RemoveResolvedCandidate(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil || tool == nil || method == nil || resolved == nil {
		return fmt.Errorf("runner, tool, method, and resolved plan are required")
	}
	if err := resolved.Validate(); err != nil {
		return fmt.Errorf("invalid resolved plan: %w", err)
	}
	if resolved.Tool.Name != tool.Name {
		return fmt.Errorf("resolved tool %q does not match %q", resolved.Tool.Name, tool.Name)
	}
	if resolved.Candidate.Method != method.Kind {
		return fmt.Errorf("resolved method %q does not match %q", resolved.Candidate.Method, method.Kind)
	}
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return fmt.Errorf("no adapter registered for %q", method.Kind)
	}
	if !adapter.CanRemove() {
		return fmt.Errorf("adapter %q does not support removal", method.Kind)
	}

	projected, err := removalMethodForResolvedTarget(method, resolved)
	if err != nil {
		return err
	}
	if requirer, ok := adapter.(RemovalElevationRequirer); ok && requirer.RequiresRemovalElevation(tool, projected) {
		if session, ok := rn.(run.ElevationSession); ok {
			stop, err := session.StartElevationSession(ctx)
			if err != nil {
				return fmt.Errorf("elevation: %w", err)
			}
			defer stop()
		}
	}
	ctx = run.WithOmittedEnv(ctx, methodSecretEnvNames(method)...)
	return adapter.Remove(ctx, rn, tool, projected)
}

func removalMethodForResolvedTarget(method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (*config.MethodCandidate, error) {
	projected := methodForResolvedTarget(method, resolved)
	methodCopy := *projected
	methodCopy.Config = maps.Clone(projected.Config)
	if methodCopy.Config == nil {
		methodCopy.Config = make(map[string]any)
	}

	contract, ok := methodkind.Lookup(method.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown method kind %q", method.Kind)
	}
	if resolved.Identity.Package != "" {
		methodCopy.Config["pkg"] = resolved.Identity.Package
	}
	if methodkind.IsNativeKind(method.Kind) {
		// pkg_overrides predates resolved plans and can select a different package.
		delete(methodCopy.Config, "pkg_overrides")
	}
	if resolved.Identity.Version != "" {
		methodCopy.Config["version"] = resolved.Identity.Version
	}
	if resolved.Identity.Source != "" {
		if _, exists := contract.Fields["source"]; exists {
			methodCopy.Config["source"] = resolved.Identity.Source
		}
	}
	if resolved.Identity.Scope != "" && contract.Scopes != nil {
		scope, err := contract.AdapterScope(plan.Scope(resolved.Identity.Scope))
		if err != nil {
			return nil, err
		}
		methodCopy.Config["scope"] = scope
	}
	if resolved.Identity.Architecture != "" {
		if _, exists := contract.Fields["architecture"]; exists {
			methodCopy.Config["architecture"] = resolved.Identity.Architecture
		}
	}
	if method.Kind == "scoop" {
		delete(methodCopy.Config, "bucket")
		if bucket := resolvedSelectionSource(resolved); bucket != "" {
			methodCopy.Config["bucket"] = bucket
		}
	}
	return &methodCopy, nil
}
