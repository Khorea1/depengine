package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeSchemaInline writes a schema and returns its path.
func writeSchemaInline(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "depengine.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return p
}

func TestResolveSchema_SimpleToolWithManifestOverrides(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["nvim"]
`)
	manifestPath := writeManifest(t, `
[packages]
nvim = { pacman = "neovim", apt = "neovim" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, count := ResolveSchema(s, mt)
	if count != 1 {
		t.Fatalf("expected 1 tool merged, got %d", count)
	}

	nvim := resolved.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim in resolved schema")
	}
	if !nvim.IsSimple {
		t.Fatal("expected IsSimple preserved from schema")
	}

	mc := nvim.Methods[0]
	if mc.Kind != "native" {
		t.Fatalf("expected native method, got %q", mc.Kind)
	}
	// For IsSimple + auto-injected pkg, manifest's pkg wins.
	if mc.Config["pkg"] != "nvim" {
		t.Fatalf("expected pkg=nvim from manifest, got %v", mc.Config["pkg"])
	}
	overrides, ok := mc.Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides from manifest")
	}
	if overrides["pacman"] != "neovim" || overrides["apt"] != "neovim" {
		t.Fatalf("unexpected overrides: %v", overrides)
	}
}

func TestResolveSchema_SimpleToolManifestPkgWins(t *testing.T) {
	// IsSimple auto-injects pkg=toolName. Manifest provides different pkg.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["fd"]
`)
	manifestPath := writeManifest(t, `
[packages]
fd = { apt = "fd-find" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	fd := resolved.Tools["fd"]
	if fd == nil {
		t.Fatal("expected fd in resolved schema")
	}
	mc := fd.Methods[0]
	// IsSimple pkg="fd" equals tool name → manifest pkg wins (still "fd").
	pkg := mc.Config["pkg"].(string)
	if pkg != "fd" {
		t.Fatalf("expected pkg=fd, got %q", pkg)
	}
	// pkg_overrides from manifest should be merged.
	ov, ok := mc.Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides")
	}
	if ov["apt"] != "fd-find" {
		t.Fatalf("expected apt=fd-find, got %v", ov["apt"])
	}
}

func TestResolveSchema_NonSimpleSchemaPkgWins(t *testing.T) {
	// Schema has fd with apt override. Manifest has different apt.
	// Schema's apt should win.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"
method_order = ["native"]

[tools]
fd = { apt = "fd-find-original" }
`)
	manifestPath := writeManifest(t, `
[packages]
fd = { pacman = "fd-bin", apt = "fd-find-override" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	fd := resolved.Tools["fd"]
	if fd == nil {
		t.Fatal("expected fd in resolved schema")
	}
	if fd.IsSimple {
		t.Fatal("expected IsSimple=false")
	}

	mc := fd.Methods[0]
	// pkg: schema wins (not IsSimple)
	if mc.Config["pkg"] != "fd" {
		t.Fatalf("expected pkg=fd, got %v", mc.Config["pkg"])
	}
	// pkg_overrides: merge, schema wins on apt, manifest adds pacman.
	ov, ok := mc.Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides")
	}
	if ov["apt"] != "fd-find-original" {
		t.Fatalf("expected apt=fd-find-original (schema wins), got %v", ov["apt"])
	}
	if ov["pacman"] != "fd-bin" {
		t.Fatalf("expected pacman=fd-bin from manifest, got %v", ov["pacman"])
	}
}

func TestResolveSchema_ManifestAddsNonNativeMethod(t *testing.T) {
	// Schema: nvim simple. Manifest adds cargo method.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["nvim"]
`)
	manifestPath := writeManifest(t, `
[packages]
nvim = { cargo = "neovim" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	nvim := resolved.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim in resolved schema")
	}
	if len(nvim.Methods) != 2 {
		t.Fatalf("expected 2 methods (native + cargo), got %d", len(nvim.Methods))
	}
	// cargo should be the second method.
	if nvim.Methods[1].Kind != "cargo" {
		t.Fatalf("expected methods[1].Kind=cargo, got %q", nvim.Methods[1].Kind)
	}
	if nvim.Methods[1].Config["pkg"] != "neovim" {
		t.Fatalf("expected cargo pkg=neovim, got %v", nvim.Methods[1].Config["pkg"])
	}
}

func TestResolveSchema_SchemaExistingKindNotOverridden(t *testing.T) {
	// Schema has cargo method. Manifest also has cargo for same tool → schema's cargo wins.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
neovim = { cargo = "neovim-schema" }
`)
	manifestPath := writeManifest(t, `
[packages]
neovim = { cargo = "neovim-manifest" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	nvim := resolved.Tools["neovim"]
	if nvim == nil {
		t.Fatal("expected neovim")
	}
	// Should have native + cargo (schema's cargo, not manifest's).
	foundCargo := false
	for _, m := range nvim.Methods {
		if m.Kind == "cargo" {
			foundCargo = true
			if m.Config["pkg"] != "neovim-schema" {
				t.Fatalf("expected schema's cargo pkg=neovim-schema, got %v", m.Config["pkg"])
			}
		}
	}
	if !foundCargo {
		t.Fatal("expected cargo method from schema")
	}
}

func TestResolveSchema_ManifestAddsNewKind(t *testing.T) {
	// Schema has native only. Manifest adds http method.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["fastfetch"]
`)
	manifestPath := writeManifest(t, `
[packages]
fastfetch = { http = { url = "https://example.com/fastfetch.zip" } }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	ff := resolved.Tools["fastfetch"]
	if ff == nil {
		t.Fatal("expected fastfetch")
	}
	// Native + http (from manifest) = 2
	if len(ff.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(ff.Methods))
	}
	httpFound := false
	for _, m := range ff.Methods {
		if m.Kind == "http" {
			httpFound = true
			if m.Config["url"] != "https://example.com/fastfetch.zip" {
				t.Fatalf("unexpected http url: %v", m.Config["url"])
			}
		}
	}
	if !httpFound {
		t.Fatal("expected http method from manifest")
	}
}

func TestResolveSchema_WhenFromManifest(t *testing.T) {
	// Schema has simple nvim. Manifest provides cargo method with When.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["nvim"]
`)
	manifestPath := writeManifest(t, `
[packages]
[packages.nvim]
  [packages.nvim.cargo]
  pkg  = "neovim"
  when = { distro_family = ["arch"] }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	nvim := resolved.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim")
	}
	if len(nvim.Methods) != 2 {
		t.Fatalf("expected 2 methods (native + cargo), got %d", len(nvim.Methods))
	}
	// cargo (index 1) should have When from manifest.
	cargo := nvim.Methods[1]
	if cargo.Kind != "cargo" {
		t.Fatalf("expected methods[1]=cargo, got %q", cargo.Kind)
	}
	if cargo.When == nil {
		t.Fatal("expected When on cargo method from manifest")
	}
	if len(cargo.When.DistroFamily) != 1 || cargo.When.DistroFamily[0] != "arch" {
		t.Fatalf("expected when distro_family=[arch], got %v", cargo.When.DistroFamily)
	}
}

func TestResolveSchema_WhenSchemaWins(t *testing.T) {
	// Schema cargo has When. Manifest cargo also has When. Schema wins.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
nvim = { cargo = { pkg = "neovim", when = { distro_family = ["debian"] } } }
`)
	manifestPath := writeManifest(t, `
[packages]
[packages.nvim]
  [packages.nvim.cargo]
  pkg  = "neovim"
  when = { distro_family = ["arch"] }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	nvim := resolved.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim")
	}
	// Schema should have native + cargo (schema's cargo with debian When).
	cargoFound := false
	for _, m := range nvim.Methods {
		if m.Kind == "cargo" {
			cargoFound = true
			if m.When == nil || len(m.When.DistroFamily) != 1 || m.When.DistroFamily[0] != "debian" {
				t.Fatalf("expected schema's When (debian), got %v", m.When)
			}
		}
	}
	if !cargoFound {
		t.Fatal("expected cargo method")
	}
}

func TestResolveSchema_ToolOnlyInManifestIgnored(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["zsh"]
`)
	manifestPath := writeManifest(t, `
[packages]
nvim = { pacman = "neovim" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	// nvim is not in schema, should not appear in resolved.
	if _, ok := resolved.Tools["nvim"]; ok {
		t.Fatal("expected nvim to NOT be in resolved schema (tool only in manifest)")
	}
	// zsh should still be there.
	if _, ok := resolved.Tools["zsh"]; !ok {
		t.Fatal("expected zsh in resolved schema")
	}
}

func TestResolveSchema_RequiresFromSchema(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
nvim = { requires = ["zsh"], pacman = "neovim" }
`)
	manifestPath := writeManifest(t, `
[packages]
nvim = { pacman = "neovim" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	nvim := resolved.Tools["nvim"]
	if len(nvim.Requires) != 1 || nvim.Requires[0] != "zsh" {
		t.Fatalf("expected requires=[zsh] from schema, got %v", nvim.Requires)
	}
}
func TestResolveSchema_DoesNotMutateOriginal(t *testing.T) {
	// Test 1: Single native method (existing test).
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["nvim"]
`)
	manifestPath := writeManifest(t, `
[packages]
nvim = { pacman = "neovim" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	// Capture original methods reference before resolve.
	origMethods := s.Tools["nvim"].Methods
	resolved, _ := ResolveSchema(s, mt)

	// Original should be unchanged.
	if len(s.Tools["nvim"].Methods) != 1 {
		t.Fatal("original was mutated: methods count changed")
	}
	// Methods should not share the same pointer.
	if s.Tools["nvim"].Methods[0] == resolved.Tools["nvim"].Methods[0] {
		t.Fatal("original and resolved share method pointer")
	}
	if &s.Tools["nvim"].Methods[0].Config == &resolved.Tools["nvim"].Methods[0].Config {
		t.Fatal("original and resolved share config map")
	}
	_ = origMethods

	// Test 2: Schema has native + non-native methods, manifest only modifies native.
	// This exercises the `default` branch of mergeMethods where sm was appended
	// without deep copy.
	schemaPath2 := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
nvim = { pacman = "neovim", cargo = "neovim-cargo" }
`)
	manifestPath2 := writeManifest(t, `
[packages]
nvim = { pacman = "neovim" }
`)

	s2, err := ParseSchemaNoFacts(schemaPath2)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt2, err := ParseManifest(manifestPath2)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved2, _ := ResolveSchema(s2, mt2)

	// Native method (index 0) should not share pointer.
	if s2.Tools["nvim"].Methods[0] == resolved2.Tools["nvim"].Methods[0] {
		t.Fatal("original and resolved share native method pointer")
	}
	// Cargo method (index 1) should not share pointer.
	if s2.Tools["nvim"].Methods[1] == resolved2.Tools["nvim"].Methods[1] {
		t.Fatal("original and resolved share non-native method pointer")
	}
	// Config maps should not be the same map.
	if &s2.Tools["nvim"].Methods[1].Config == &resolved2.Tools["nvim"].Methods[1].Config {
		t.Fatal("original and resolved share non-native config map")
	}
}

func TestResolveSchema_OrderByMethodOrder(t *testing.T) {
	// Schema has method_order with native before cargo.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"
method_order = ["native", "cargo"]

[tools]
simple = ["nvim"]
`)
	manifestPath := writeManifest(t, `
[packages]
nvim = { cargo = "neovim" }
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}
	mt, err := ParseManifest(manifestPath)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	resolved, _ := ResolveSchema(s, mt)
	nvim := resolved.Tools["nvim"]
	if len(nvim.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(nvim.Methods))
	}
	if nvim.Methods[0].Kind != "native" {
		t.Fatalf("expected methods[0]=native, got %q", nvim.Methods[0].Kind)
	}
	if nvim.Methods[1].Kind != "cargo" {
		t.Fatalf("expected methods[1]=cargo, got %q", nvim.Methods[1].Kind)
	}
}

func TestResolveSchema_ManifestErrMethodSkipped(t *testing.T) {
	// Schema has nvim. Manifest has a method with invalid value → skipped.
	// We can't easily create an Err method through toml, so we test the
	// edge case by directly constructing a manifest with an errored method.
	schemaPath := writeSchemaInline(t, `
[defaults]
manager = "native"

[tools]
simple = ["nvim"]
`)

	s, err := ParseSchemaNoFacts(schemaPath)
	if err != nil {
		t.Fatalf("ParseSchemaNoFacts: %v", err)
	}

	// Manually construct manifest with an errored method.
	manifest := map[string]*Tool{
		"nvim": {
			Name: "nvim",
			Methods: []*MethodCandidate{
				{Kind: "native", Config: map[string]any{"pkg": "neovim"}},
				{Kind: "broken", Config: map[string]any{}, Err: fmt.Errorf("invalid method")},
			},
		},
	}

	resolved, count := ResolveSchema(s, manifest)
	if count != 1 {
		t.Fatalf("expected 1 tool merged, got %d", count)
	}
	nvim := resolved.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim")
	}
	// Only native from schema should remain + the valid manifest native.
	// But schema already has a native method which wins. The manifest's
	// native adds pkg_overrides (none here). The "broken" method is skipped.
	for _, m := range nvim.Methods {
		if m.Kind == "broken" {
			t.Fatal("expected broken method to be skipped")
		}
	}
}
