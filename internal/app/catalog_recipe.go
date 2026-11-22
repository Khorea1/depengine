package app

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
)

// validateCatalogRecipe enforces both the declarative-only and artifact-integrity
// entry rules on every catalog lookup, including recipes added without import.
// Import and lookup share the same executable and checksum checks.
func validateCatalogRecipe(recipe *config.Schema) error {
	if err := planner.ValidateDeclarativeRecipe(recipe); err != nil {
		return err
	}
	// A recipe can be put into a catalog root without going through import.
	// Reapply the same integrity floor on lookup, before the recipe is merged.
	for name, tool := range recipe.Tools {
		report := &importReport{}
		for _, method := range tool.Methods {
			contract, ok := methodkind.Lookup(method.Kind)
			if !ok {
				return fmt.Errorf("recipe %q: unknown method kind %q", name, method.Kind)
			}
			inspectImportIntegrity(report, contract, method)
		}
		if len(report.Errors) > 0 {
			return fmt.Errorf("recipe %q: %s", name, strings.Join(report.Errors, "; "))
		}
	}
	return nil
}
