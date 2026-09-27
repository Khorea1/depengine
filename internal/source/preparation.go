package source

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

const preparationSourcePrefix = "source/v1?"

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
		description, err := preparationSourceDescription(configured)
		if err != nil {
			return plan.PreparationPlan{}, fmt.Errorf("source %d: %w", i, err)
		}
		rollback := plan.Operation{
			Kind:        "remove-source",
			Description: description,
			Effect:      plan.EffectMutation,
		}
		preparation.Prepare = append(preparation.Prepare, plan.PreparationMutation{
			ID:        fmt.Sprintf("source-%d", i),
			Resource:  identity,
			Ownership: plan.OwnershipDepengine,
			Apply: plan.Operation{
				Kind:        "add-source",
				Description: description,
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

// SourceFromPreparationMutation reconstructs the exact credential-free source
// descriptor persisted with a source preparation. Older journals that stored
// only the ownership resource key remain readable and fall back to kind+name.
func SourceFromPreparationMutation(mutation plan.PreparationMutation) (config.Source, error) {
	description := mutation.Apply.Description
	if !strings.HasPrefix(description, preparationSourcePrefix) {
		return FromResourceIdentity(mutation.Resource)
	}
	values, err := url.ParseQuery(strings.TrimPrefix(description, preparationSourcePrefix))
	if err != nil {
		return config.Source{}, fmt.Errorf("parse source preparation descriptor: %w", err)
	}
	if len(values) < 2 || len(values) > 3 || len(values["kind"]) != 1 || len(values["name"]) != 1 || len(values["url"]) > 1 {
		return config.Source{}, fmt.Errorf("invalid source preparation descriptor %q", description)
	}
	for key := range values {
		if key != "kind" && key != "name" && key != "url" {
			return config.Source{}, fmt.Errorf("invalid source preparation descriptor %q", description)
		}
	}
	source := config.Source{
		Kind: values.Get("kind"),
		Name: values.Get("name"),
		URL:  values.Get("url"),
	}
	identity, err := ResourceIdentity(source)
	if err != nil {
		return config.Source{}, err
	}
	if identity != mutation.Resource {
		return config.Source{}, fmt.Errorf("source preparation descriptor does not match resource identity")
	}
	if err := validateSourceURLSupport(source); err != nil {
		return config.Source{}, err
	}
	canonical, err := preparationSourceDescription(source)
	if err != nil {
		return config.Source{}, err
	}
	if canonical != description {
		return config.Source{}, fmt.Errorf("source preparation descriptor is not canonical")
	}
	return source, nil
}

func preparationSourceDescription(source config.Source) (string, error) {
	if err := validateSourceURLSupport(source); err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("kind", strings.ToLower(strings.TrimSpace(source.Kind)))
	values.Set("name", strings.ToLower(strings.TrimSpace(source.Name)))
	if source.URL != "" {
		values.Set("url", source.URL)
	}
	return preparationSourcePrefix + values.Encode(), nil
}
