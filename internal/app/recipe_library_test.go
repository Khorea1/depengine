package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

var literalSHA256 = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// The Git checkout is the community library. The binary should not silently
// acquire community recipes, while the deliberately bundled recipe remains in
// sync byte for byte.
func TestRepositoryRecipeLibraryImportAndBuiltinPolicy(t *testing.T) {
	library, err := filepath.Abs(filepath.Join("..", "..", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	builtin := config.NewCatalogWithBuiltins(nil, validateCatalogRecipe)
	for _, name := range []string{"depengine", "nvim"} {
		if _, err := builtin.Lookup(name); !errors.Is(err, config.ErrRecipeNotFound) {
			t.Fatalf("%s must be opt-in, got %v", name, err)
		}
	}
	target := t.TempDir()
	user := config.NewCatalog([]string{target}, validateCatalogRecipe)
	sources, err := filepath.Glob(filepath.Join(library, "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("recipe library is empty")
	}
	for _, source := range sources {
		name := strings.TrimSuffix(filepath.Base(source), ".toml")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(source) // #nosec G304 -- test reads a repository fixture path.
			if err != nil {
				t.Fatal(err)
			}
			schema, err := config.ParseRecipeBytes(source, raw, nil)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(schema.Tools) != 1 || schema.Tools[name] == nil {
				t.Fatalf("recipe must declare exactly [packages.%s]: %v", name, schema.Tools)
			}
			if err := validateCatalogRecipe(schema); err != nil {
				t.Fatalf("not a valid catalog recipe: %v", err)
			}
			assertDeclarativeRecipePolicy(t, name, schema.Tools[name])

			repo := config.NewCatalog([]string{library}, validateCatalogRecipe)
			recipe, err := repo.Lookup(name)
			if err != nil {
				t.Fatal(err)
			}
			if recipe.RootIndex != 0 || recipe.Path != source {
				t.Fatalf("unexpected repository origin: %+v", recipe)
			}
			var out, stderr bytes.Buffer
			if err := runImportToFormat(&out, &stderr, source, []string{target}, false, false, "text"); err != nil {
				t.Fatalf("import %s: %v\nstdout=%s\nstderr=%s", name, err, out.String(), stderr.String())
			}
			imported, err := user.Lookup(name)
			if err != nil {
				t.Fatal(err)
			}
			if imported.Digest != recipe.Digest {
				t.Fatalf("imported digest %q != repository %q", imported.Digest, recipe.Digest)
			}
			stored, err := os.ReadFile(filepath.Join(target, name+".toml")) // #nosec G304 -- target is a test-owned catalog directory.
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, raw) {
				t.Fatal("import modified the recipe")
			}
			if name != "golang" {
				return
			}
			embedded, err := builtin.Lookup(name)
			if err != nil {
				t.Fatal(err)
			}
			if recipe.Digest != embedded.Digest {
				t.Fatalf("embedded %s diverges from versioned recipe", name)
			}
			embeddedBytes, err := os.ReadFile(filepath.Join("..", "config", "builtin_recipes", name+".toml")) // #nosec G304 -- repository fixture path derived from the embedded recipe list.
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(embeddedBytes, raw) {
				t.Fatal("embedded recipe is not byte-identical to the public recipe")
			}
		})
	}
}

func assertDeclarativeRecipePolicy(t *testing.T, name string, tool *config.Tool) {
	t.Helper()
	if tool == nil {
		t.Fatal("missing tool")
	}
	kinds := make(map[string]bool, len(tool.Methods))
	for _, method := range tool.Methods {
		kinds[method.Kind] = true
		if checksum, ok := method.Config["checksum"].(string); ok && strings.HasSuffix(checksum, ":auto") {
			t.Fatalf("%s/%s uses a mutable %q checksum; shared recipes require a literal hash", name, method.Kind, checksum)
		}
		if method.Kind == "http" || method.Kind == "github" {
			checksum, _ := method.Config["checksum"].(string)
			if !literalSHA256.MatchString(checksum) {
				t.Fatalf("%s/%s downloads an artifact without a literal sha256 checksum", name, method.Kind)
			}
		}
	}
	for _, only := range tool.MethodOnly {
		if !kinds[only] {
			t.Fatalf("%s method_only names %q but the recipe declares no such method", name, only)
		}
	}
}

// The public library is the reviewable coverage matrix for the method
// families the binary supports without platform-specific installer knowledge.
// A new curated recipe must not silently drop a family, and no recipe may
// declare the same method twice.
func TestRepositoryRecipeLibraryCoversMethodFamilies(t *testing.T) {
	library, err := filepath.Abs(filepath.Join("..", "..", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(library, "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	covered := make(map[string]bool)
	for _, source := range sources {
		raw, err := os.ReadFile(source) // #nosec G304 -- test reads a repository fixture path.
		if err != nil {
			t.Fatal(err)
		}
		schema, err := config.ParseRecipeBytes(source, raw, nil)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		for name, tool := range schema.Tools {
			labels := make(map[string]bool, len(tool.Methods))
			for _, method := range tool.Methods {
				covered[method.Kind] = true
				label := method.Label
				if label == "" {
					label = method.Kind
				}
				if labels[label] {
					t.Errorf("%s/%s declares method %q twice", filepath.Base(source), name, label)
				}
				labels[label] = true
			}
		}
	}
	for _, kind := range []string{
		"native", "aur", "cargo", "go", "npm", "pnpm", "bun", "pipx", "uv",
		"gem", "composer", "flatpak", "snap", "github", "appimage", "http",
	} {
		if !covered[kind] {
			t.Errorf("recipe library has no %q method; add a recipe or extend an existing one", kind)
		}
	}
}

func TestDepengineSelfManagementExampleIsAProjectSchema(t *testing.T) {
	path := filepath.Join("..", "..", "recipes", "depengine.toml.example")
	raw, err := os.ReadFile(path) // #nosec G304 -- test reads a repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	schema, err := config.ParseProjectSchema(path, nil)
	if err != nil {
		t.Fatalf("self-management example is not a valid project schema: %v", err)
	}
	tool := schema.Tools["depengine"]
	if tool == nil || len(tool.Methods) != 1 || tool.Methods[0].Kind != "git" {
		t.Fatalf("self-management example must declare one git method: %+v", tool)
	}
	if _, ok := tool.Methods[0].Config["build"]; !ok {
		t.Fatal("self-management example must build from source")
	}
	if _, err := config.ParseRecipeBytes(path, raw, nil); err == nil {
		t.Fatal("self-management example must not parse as a catalog recipe")
	}
}
