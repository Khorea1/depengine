package source

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// PreparationPlan builds the durable transaction model for sources that were
// already observed missing. Every source add is depengine-owned and safely
// reversible by its canonical kind+name identity. The commit operation models
// the candidate installation that consumes these prepared sources.
func PreparationPlan(missing []config.Source) (plan.PreparationPlan, error) {
	preparation := plan.PreparationPlan{
		Prepare: make([]plan.PreparationMutation, 0, len(missing)),
		Commit: []plan.Operation{{
			Kind:   "install",
			Effect: plan.EffectMutation,
		}},
	}
	for i, configured := range missing {
		identity, err := ResourceIdentity(configured)
		if err != nil {
			return plan.PreparationPlan{}, fmt.Errorf("source %d: %w", i, err)
		}
		rollback := plan.Operation{
			Kind:        "remove-source",
			Description: identity.Key,
			Effect:      plan.EffectMutation,
		}
		preparation.Prepare = append(preparation.Prepare, plan.PreparationMutation{
			ID:        fmt.Sprintf("source-%d", i),
			Resource:  identity,
			Ownership: plan.OwnershipDepengine,
			Apply: plan.Operation{
				Kind:        "add-source",
				Description: identity.Key,
				Effect:      plan.EffectMutation,
			},
			Rollback: &rollback,
			Policy:   plan.RollbackSafe,
		})
	}
	if err := preparation.Validate(); err != nil {
		return plan.PreparationPlan{}, err
	}
	return preparation, nil
}
