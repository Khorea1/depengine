package app

import (
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/planner"
)

// validateCatalogRecipe enforces ADR-008 decision 4 at catalog lookup: a recipe
// may not carry lifecycle hooks, command-typed fields or a plan that requires
// arbitrary code. Import uses the same planner inspector to build its preview,
// so the entry gate and the execution gate cannot drift.
func validateCatalogRecipe(recipe *config.Schema) error {
	return planner.ValidateDeclarativeRecipe(recipe)
}
