package app

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/spf13/cobra"
)

// addCatalogFlags installs the ADR-009 catalog flags on a command.
func addCatalogFlags(cmd *cobra.Command, roots *[]string, noCatalog *bool) {
	f := cmd.Flags()
	f.StringArrayVar(roots, "catalog", nil, "path to a recipe catalog root (repeatable; default: platform config dir/depengine/catalog)")
	f.BoolVar(noCatalog, "no-catalog", false, "disable the recipe catalog (default: auto-detect)")
}

// catalogFromCommand resolves the catalog flags into a Catalog. Commands that
// call it must have installed the flags with addCatalogFlags.
func catalogFromCommand(cmd *cobra.Command) (*config.Catalog, error) {
	roots, err := cmd.Flags().GetStringArray("catalog")
	if err != nil {
		return nil, fmt.Errorf("read --catalog: %w", err)
	}
	noCatalog, err := cmd.Flags().GetBool("no-catalog")
	if err != nil {
		return nil, fmt.Errorf("read --no-catalog: %w", err)
	}
	resolved, err := config.ResolveCatalogRoots(roots, noCatalog)
	if err != nil {
		return nil, err
	}
	return config.NewCatalog(resolved, validateCatalogRecipe), nil
}
