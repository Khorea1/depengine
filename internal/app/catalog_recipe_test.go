package app

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestResolveLayersRejectsNonDeclarativeCatalogRecipe(t *testing.T) {
	root := t.TempDir()
	writeCatalogRecipe(t, root, "tool", `recipe_version = 1

[packages.tool]
native = "tool"
pre_install = { run = ["echo", "hi"] }
`)
	schema := &config.Schema{Version: 1, Tools: map[string]*config.Tool{"tool": {Name: "tool"}}}

	_, err := config.ResolveLayers(schema, nil, config.NewCatalog([]string{root}, validateCatalogRecipe), nil, false)
	if err == nil || !strings.Contains(err.Error(), "not declarative") {
		t.Fatalf("ResolveLayers() error = %v, want declarative rejection", err)
	}
}

func TestResolveLayersAcceptsDeclarativeCatalogRecipe(t *testing.T) {
	root := t.TempDir()
	writeCatalogRecipe(t, root, "tool", "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n")
	schema := &config.Schema{Version: 1, Tools: map[string]*config.Tool{"tool": {Name: "tool"}}}

	res, err := config.ResolveLayers(schema, nil, config.NewCatalog([]string{root}, validateCatalogRecipe), nil, false)
	if err != nil {
		t.Fatalf("ResolveLayers() error = %v", err)
	}
	if res.CatalogCount != 1 {
		t.Fatalf("CatalogCount = %d, want 1", res.CatalogCount)
	}
	if tool := res.Schema.Tools["tool"]; tool == nil || len(tool.Methods) != 1 {
		t.Fatalf("merged tool = %+v, want one catalog method", tool)
	}
}
