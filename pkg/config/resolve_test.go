package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSchemaInline writes a schema and returns its path.
func writeSchemaInline(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return p
}

// TestMergeLayers_LaterLayerWins verifies that tools in later layers
// completely overwrite earlier layers (whole-tool, not field-by-field).
func TestMergeLayers_LaterLayerWins(t *testing.T) {
	// Layer 1 (lower priority): manifest with nvim having native + manual method_order.
	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim", method_order = ["native"] }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	// Layer 2 (higher priority): schema with nvim having apt override + requires.
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim-full", requires = ["zsh"] }
	`)
	localSchema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	merged := MergeLayers(manifest, localSchema)
	nvim := merged.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim in merged schema")
	}

	// Schema layer wins entirely: should have apt from schema, not pacman from manifest.
	hasPacman := false
	hasApt := false
	for _, m := range nvim.Methods {
		if m.Kind == "native" {
			if overrides, ok := m.Config["pkg_overrides"].(map[string]any); ok {
				if _, ok := overrides["pacman"]; ok {
					hasPacman = true
				}
				if _, ok := overrides["apt"]; ok {
					hasApt = true
				}
			}
		}
	}
	if hasPacman {
		t.Fatal("expected schema's version to win (no pacman override from manifest)")
	}
	if !hasApt {
		t.Fatal("expected apt override from schema to be present")
	}
	if len(nvim.Requires) != 1 || nvim.Requires[0] != "zsh" {
		t.Fatalf("expected requires=[zsh] from schema, got %v", nvim.Requires)
	}
}

// TestMergeLayers_ToolOnlyInOneLayer verifies tools present in only one layer.
func TestMergeLayers_ToolOnlyInOneLayer(t *testing.T) {
	manifestPath := writeSchemaInline(t, `
[packages]
fd = { cargo = "fd-find" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { pacman = "neovim" }
	`)
	localSchema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	// Layer order: manifest first, schema second (most specific).
	merged := MergeLayers(manifest, localSchema)

	if _, ok := merged.Tools["fd"]; !ok {
		t.Fatal("expected fd from manifest layer to be present")
	}
	if _, ok := merged.Tools["nvim"]; !ok {
		t.Fatal("expected nvim from schema layer to be present")
	}
	if len(merged.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(merged.Tools))
	}
}

// TestMergeLayers_DefaultsFromMostSpecific verifies defaults come from
// the most specific layer.
func TestMergeLayers_DefaultsFromMostSpecific(t *testing.T) {
	manifestPath := writeSchemaInline(t, `
[packages]
fd = { cargo = "fd-find" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "apt"
method_order = ["native", "cargo"]
[tools]
nvim = { pacman = "neovim" }
	`)
	localSchema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	merged := MergeLayers(manifest, localSchema)
	if merged.Defaults.Manager != "apt" {
		t.Fatalf("expected manager=apt from schema, got %q", merged.Defaults.Manager)
	}
	if len(merged.Defaults.MethodOrder) == 0 {
		t.Fatal("expected method_order from schema")
	}
}

// TestMergeLayers_EmptyLayers verifies that MergeLayers handles empty input.
func TestMergeLayers_EmptyLayers(t *testing.T) {
	merged := MergeLayers()
	if merged == nil {
		t.Fatal("expected non-nil Schema from MergeLayers()")
	}
	if merged.Defaults.Manager != "native" {
		t.Fatalf("expected default manager 'native', got %q", merged.Defaults.Manager)
	}
	if len(merged.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(merged.Tools))
	}
}

// TestMergeLayers_ToolOnlyInManifestNowIncluded verifies that tools only
// in the manifest layer ARE included (unlike old ResolveSchema which ignored them).
func TestMergeLayers_ToolOnlyInManifestNowIncluded(t *testing.T) {
	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	schemaPath := writeSchemaInline(t, `
[tools]
fd = { cargo = "fd-find" }
	`)
	localSchema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	merged := MergeLayers(manifest, localSchema)
	if _, ok := merged.Tools["nvim"]; !ok {
		t.Fatal("expected nvim (manifest-only tool) to be included in merged result")
	}
	if _, ok := merged.Tools["fd"]; !ok {
		t.Fatal("expected fd (schema-only tool) to be included")
	}
}

// TestMergeLayers_ThreeLayers verifies merging with three layers.
func TestMergeLayers_ThreeLayers(t *testing.T) {
	p1 := writeSchemaInline(t, `
[packages]
a = { pip = "a1" }
b = { pip = "b1" }
	`)
	l1, err := ParseSchema(p1, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(l1): %v", err)
	}

	p2 := writeSchemaInline(t, `
[packages]
b = { pip = "b2" }
c = { pip = "c2" }
	`)
	l2, err := ParseSchema(p2, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(l2): %v", err)
	}

	p3 := writeSchemaInline(t, `
[tools]
c = { pip = "c3" }
d = { pip = "d3" }
	`)
	l3, err := ParseSchema(p3, nil)
	if err != nil {
		t.Fatalf("ParseSchema(l3): %v", err)
	}

	merged := MergeLayers(l1, l2, l3)
	if _, ok := merged.Tools["a"]; !ok {
		t.Fatal("expected a from l1")
	}
	// b was in l1 and l2: l2 wins.
	// Native method is auto-injected first; non-native methods follow.
	var bPkg string
	for _, m := range merged.Tools["b"].Methods {
		if m.Kind == "pip" {
			if pkg, ok := m.Config["pkg"].(string); ok {
				bPkg = pkg
			}
			break
		}
	}
	if bPkg != "b2" {
		t.Fatalf("expected b pip method pkg=b2 from l2, got pkg=%q", bPkg)
	}

	// c was in l2 and l3: l3 wins.
	var cPkg string
	for _, m := range merged.Tools["c"].Methods {
		if m.Kind == "pip" {
			if pkg, ok := m.Config["pkg"].(string); ok {
				cPkg = pkg
			}
			break
		}
	}
	if cPkg != "c3" {
		t.Fatalf("expected c pip method pkg=c3 from l3, got pkg=%q", cPkg)
	}

	if _, ok := merged.Tools["d"]; !ok {
		t.Fatal("expected d from l3")
	}
}

// TestValidateGlobalLayer_RejectsIntentFields verifies that ValidateGlobalLayer
// rejects fields that should not be in a manifest.
func TestValidateGlobalLayer_RejectsIntentFields(t *testing.T) {
	p := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim", pre_install = "dangerous", requires = ["zsh"], tags = ["desktop"] }
	`)
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	err = ValidateGlobalLayer(s)
	if err == nil {
		t.Fatal("expected error from ValidateGlobalLayer, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pre_install") {
		t.Fatalf("expected error about pre_install, got: %s", msg)
	}
	if !strings.Contains(msg, "requires") {
		t.Fatalf("expected error about requires, got: %s", msg)
	}
	if !strings.Contains(msg, "tags") {
		t.Fatalf("expected error about tags, got: %s", msg)
	}
}

// TestValidateGlobalLayer_AcceptsCleanLayer verifies that a clean manifest
// passes validation.
func TestValidateGlobalLayer_AcceptsCleanLayer(t *testing.T) {
	p := writeSchemaInline(t, `
[packages]
fd = { cargo = "fd-find" }
	`)
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	err = ValidateGlobalLayer(s)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestResolveSchemaFromFiles verifies the convenience function works.
func TestResolveSchemaFromFiles(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim" }
	`)
	manifestPath := writeSchemaInline(t, `
[packages]
fd = { cargo = "fd-find" }
	`)

	s, err := ResolveSchemaFromFiles(schemaPath, manifestPath)
	if err != nil {
		t.Fatalf("ResolveSchemaFromFiles: %v", err)
	}
	if _, ok := s.Tools["nvim"]; !ok {
		t.Fatal("expected nvim in resolved schema")
	}
	if _, ok := s.Tools["fd"]; !ok {
		t.Fatal("expected fd (from manifest) in resolved schema")
	}
}

// TestResolveSchemaFromFiles_EmptyManifestPath skips empty paths.
func TestResolveSchemaFromFiles_EmptyManifestPath(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim" }
	`)

	s, err := ResolveSchemaFromFiles(schemaPath, "")
	if err != nil {
		t.Fatalf("ResolveSchemaFromFiles: %v", err)
	}
	if _, ok := s.Tools["nvim"]; !ok {
		t.Fatal("expected nvim in resolved schema")
	}
}
