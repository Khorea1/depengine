package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/validate"
	"github.com/spf13/cobra"
)

// These are real packaged recipes, not parallel test-only TOML fixtures.
func TestBuiltinCatalogRecipesAreValid(t *testing.T) {
	catalog := config.NewCatalogWithBuiltins(nil, validateCatalogRecipe)
	if got := catalog.Sources(); len(got) != 1 || got[0] != "builtin" {
		t.Fatalf("Sources = %v, want [builtin]", got)
	}
	const name = "golang"
	recipe, err := catalog.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	if recipe.RootIndex != -1 || recipe.Path != "builtin:builtin_recipes/"+name+".toml" {
		t.Fatalf("origin = %+v", recipe)
	}
	if !strings.HasPrefix(recipe.Digest, "sha256:") || len(recipe.Digest) != 71 {
		t.Fatalf("invalid digest %q", recipe.Digest)
	}
	specialized, err := recipe.Specialize(map[string]string{"arch": "x86_64", "os": "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if tool := specialized.Tools[name]; tool == nil || len(tool.Methods) == 0 {
		t.Fatalf("missing methods: %+v", tool)
	}
	project := &config.Schema{Version: 1, Tools: map[string]*config.Tool{name: {Name: name}}}
	result, err := config.ResolveLayers(project, nil, catalog, map[string]string{"arch": "x86_64", "os": "linux"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.CatalogCount != 1 || len(result.Schema.Tools) != 1 {
		t.Fatalf("unexpected catalog merge: count %d, tools %v", result.CatalogCount, result.Schema.Tools)
	}
	if origin := result.Schema.Tools[name].Recipe; origin == nil || origin.Digest != recipe.Digest || origin.Root != "builtin" {
		t.Fatalf("lost recipe origin: %+v", origin)
	}
	vr := validate.ValidateSchema(result.Schema, exec.RegisteredKinds())
	if vr.HasErrors() {
		t.Fatalf("invalid built-in %s: %+v", name, vr.Errors)
	}
	for _, missing := range []string{"depengine", "missing"} {
		if _, err := catalog.Lookup(missing); !errors.Is(err, config.ErrRecipeNotFound) {
			t.Fatalf("%s lookup: %v", missing, err)
		}
	}
}

func TestBuiltinCatalogExplicitOverridesAndFailClosed(t *testing.T) {
	root := t.TempDir()
	cat := config.NewCatalogWithBuiltins([]string{root}, validateCatalogRecipe)
	fallback, err := cat.Lookup("golang")
	if err != nil || fallback.RootIndex != -1 {
		t.Fatalf("builtin fallback = %+v, %v", fallback, err)
	}
	path := filepath.Join(root, "golang.toml")
	writeCatalogRecipe(t, root, "golang", "recipe_version = 1\n[packages.golang]\nnative = \"custom-golang\"\n")
	recipe, err := cat.Lookup("golang")
	if err != nil {
		t.Fatal(err)
	}
	if recipe.RootIndex != 0 || recipe.Path != path || recipe.Schema.Tools["golang"].Methods[0].Config["pkg"] != "custom-golang" {
		t.Fatalf("user root did not win: %+v", recipe)
	}
	if err := os.WriteFile(path, []byte("not valid = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Lookup("golang"); err == nil || errors.Is(err, config.ErrRecipeNotFound) {
		t.Fatalf("broken override incorrectly fell through: %v", err)
	}
}

func TestBuiltinCatalogProjectAndManifestOverride(t *testing.T) {
	root := t.TempDir()
	schemaPath := filepath.Join(root, "schema.toml")
	manifestPath := filepath.Join(root, "manifest.toml")
	if err := os.WriteFile(schemaPath, []byte("schema_version = 1\n[tools.golang]\nmethod_only = [\"native\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("schema_version = 1\n[packages.golang]\nnative = \"go-local\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := config.ParseProjectSchema(schemaPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := config.ParseManifest(manifestPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := config.ResolveLayers(schema, manifest, config.NewCatalogWithBuiltins(nil, validateCatalogRecipe), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.ManifestCount != 1 || res.CatalogCount != 1 {
		t.Fatalf("layer counts = %d/%d", res.ManifestCount, res.CatalogCount)
	}
	tool := res.Schema.Tools["golang"]
	if len(tool.MethodOnly) != 1 || tool.MethodOnly[0] != "native" {
		t.Fatalf("project method_only lost: %v", tool.MethodOnly)
	}
	if tool.Recipe == nil || tool.Recipe.Root != "builtin" {
		t.Fatalf("catalog origin lost: %+v", tool.Recipe)
	}
	found := false
	for _, method := range tool.Methods {
		if method.Kind == "native" {
			if method.Config["pkg"] != "go-local" {
				t.Fatalf("manifest native pkg did not override: %v", method.Config)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no native candidate")
	}
}

func TestCatalogFromCommandBuiltinAndDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		enabled bool
	}{
		{"default", nil, true},
		{"disabled", []string{"--no-catalog"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}
			var roots []string
			var noCatalog bool
			addCatalogFlags(cmd, &roots, &noCatalog)
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			cat, err := catalogFromCommand(cmd)
			if err != nil {
				t.Fatal(err)
			}
			recipe, err := cat.Lookup("golang")
			if tc.enabled {
				if err != nil || recipe.RootIndex != -1 {
					t.Fatalf("built-in missing: %+v, %v", recipe, err)
				}
			} else if !errors.Is(err, config.ErrRecipeNotFound) {
				t.Fatalf("--no-catalog Lookup = %v", err)
			}
		})
	}
}

func TestBuiltinCatalogWhyAudit(t *testing.T) {
	cat := config.NewCatalogWithBuiltins(nil, validateCatalogRecipe)
	schema := &config.Schema{Version: 1, Tools: map[string]*config.Tool{"golang": {Name: "golang"}}}
	res, err := config.ResolveLayers(schema, nil, cat, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	report := buildWhyReport("golang", res.Schema.Tools["golang"], res.Schema.Provenance["golang"], nil)
	if report.Recipe == nil || report.Recipe.Root != "builtin" || report.Recipe.RootIndex != -1 {
		t.Fatalf("wrong provenance: %+v", report.Recipe)
	}
	var buf bytes.Buffer
	renderWhyAudit(&buf, report)
	if !strings.Contains(buf.String(), "layer catalog, builtin") {
		t.Fatalf("wrong audit output: %s", buf.String())
	}
}
