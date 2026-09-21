package source

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// CleanupReleasedSources removes last-reference depengine-owned source
// resources from the host and finalizes their ownership records. Other
// resource kinds are deliberately retained as explicit zero-ref state until a
// type-specific cleanup implementation exists.
//
// On host cleanup failure, the returned ownership snapshot is release.Updated:
// the dependent has been released, but the zero-ref owned source remains
// durable so cleanup can be retried idempotently instead of being forgotten.
func (m *Manager) CleanupReleasedSources(ctx context.Context, release plan.ResourceReleaseDecision) ([]plan.OwnedResourceState, error) {
	if err := plan.ValidateOwnedResourceSnapshot(release.Updated); err != nil {
		return nil, fmt.Errorf("released ownership state: %w", err)
	}

	var sourceResources []plan.ResourceIdentity
	var sources []config.Source
	for _, resource := range release.Removable {
		if resource.Kind != plan.ResourceSource {
			continue
		}
		source, err := FromResourceIdentity(resource)
		if err != nil {
			return cloneOwnedResourceStates(release.Updated), fmt.Errorf("decode released source %q: %w", resource.Key, err)
		}
		sourceResources = append(sourceResources, resource)
		sources = append(sources, source)
	}
	if len(sourceResources) == 0 {
		return cloneOwnedResourceStates(release.Updated), nil
	}

	preparation := sourceCleanupPlan(sourceResources)
	sourceRelease := release
	sourceRelease.Removable = append([]plan.ResourceIdentity(nil), sourceResources...)
	cleanup, err := preparation.CleanupReleasedResources(sourceRelease)
	if err != nil {
		return cloneOwnedResourceStates(release.Updated), fmt.Errorf("plan source cleanup: %w", err)
	}
	if err := m.Remove(ctx, sources); err != nil {
		return cloneOwnedResourceStates(release.Updated), fmt.Errorf("remove released sources: %w", err)
	}
	owned, err := preparation.FinalizeReleasedResourceCleanup(sourceRelease, cleanup)
	if err != nil {
		return cloneOwnedResourceStates(release.Updated), fmt.Errorf("finalize source cleanup: %w", err)
	}
	return owned, nil
}

func sourceCleanupPlan(resources []plan.ResourceIdentity) plan.PreparationPlan {
	preparation := plan.PreparationPlan{Prepare: make([]plan.PreparationMutation, 0, len(resources))}
	for i, resource := range resources {
		rollback := plan.Operation{
			Kind:        "remove-source",
			Description: resource.Key,
			Effect:      plan.EffectMutation,
		}
		preparation.Prepare = append(preparation.Prepare, plan.PreparationMutation{
			ID:        fmt.Sprintf("source-%d", i),
			Resource:  resource,
			Ownership: plan.OwnershipDepengine,
			Apply: plan.Operation{
				Kind:        "add-source",
				Description: resource.Key,
				Effect:      plan.EffectMutation,
			},
			Rollback: &rollback,
			Policy:   plan.RollbackSafe,
		})
	}
	return preparation
}

func cloneOwnedResourceStates(states []plan.OwnedResourceState) []plan.OwnedResourceState {
	if states == nil {
		return nil
	}
	out := make([]plan.OwnedResourceState, len(states))
	for i, state := range states {
		out[i] = state
		out[i].Dependents = append([]string(nil), state.Dependents...)
	}
	return out
}
