package config

import (
	"os"
	"path/filepath"
	"reflect"
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
// merge field-by-field according to their MergeStrategy, not whole-tool.
func TestMergeLayers_LaterLayerWins(t *testing.T) {
	// Layer 1 (lower priority): manifest with nvim having pacman override.
	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
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

	// Field-level merge: schema's apt + manifest's pacman should both survive.
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
	if !hasPacman {
		t.Fatal("expected pacman override from manifest to be present (field-level merge)")
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

// TestValidateManifestLayer_AcceptsIntentFields verifies that fields like
// pre_install, post_install, requires, method_order are now allowed in the
// manifest layer. The manifest provides defaults; the schema overrides.
func TestValidateManifestLayer_AcceptsIntentFields(t *testing.T) {
	p := writeSchemaInline(t, `
[packages]
	nvim = { pacman = "neovim", pre_install = "some-setup", requires = ["zsh"] }
	`)
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	err = ValidateManifestLayer(s)
	if err != nil {
		t.Fatalf("expected no error (intent fields are now allowed in manifest), got: %v", err)
	}
}

// TestValidateManifestLayer_AcceptsCleanLayer verifies that a clean manifest
// passes validation.
func TestValidateManifestLayer_AcceptsCleanLayer(t *testing.T) {
	p := writeSchemaInline(t, `
[packages]
fd = { cargo = "fd-find" }
	`)
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	err = ValidateManifestLayer(s)
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
[manifest]
allow_new_tools = true

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

// TestMergeFieldStrategies — table-driven unit tests for each merge strategy.
func TestMergeFieldStrategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy MergeStrategy
		lower    any // value from lower-priority layer
		upper    any // value from higher-priority layer
		want     any // expected merged value
	}{
		// MergeOverwrite
		{name: "overwrite/lower-only", strategy: MergeOverwrite,
			lower: "apt", upper: nil, want: "apt"},
		{name: "overwrite/upper-only", strategy: MergeOverwrite,
			lower: nil, upper: "pacman", want: "pacman"},
		{name: "overwrite/both-upper-wins", strategy: MergeOverwrite,
			lower: "apt", upper: "pacman", want: "pacman"},
		{name: "overwrite/both-empty", strategy: MergeOverwrite,
			lower: "", upper: "", want: ""},

		// MergeMapMerge
		{name: "mapmerge/lower-only",
			strategy: MergeMapMerge,
			lower:    map[string]any{"apt": "neovim"},
			upper:    nil,
			want:     map[string]any{"apt": "neovim"}},
		{name: "mapmerge/upper-only",
			strategy: MergeMapMerge,
			lower:    nil,
			upper:    map[string]any{"pacman": "neovim"},
			want:     map[string]any{"pacman": "neovim"}},
		{name: "mapmerge/both-disjoint-keys",
			strategy: MergeMapMerge,
			lower:    map[string]any{"apt": "neovim"},
			upper:    map[string]any{"pacman": "neovim"},
			want:     map[string]any{"apt": "neovim", "pacman": "neovim"}},
		{name: "mapmerge/upper-overwrites-same-key",
			strategy: MergeMapMerge,
			lower:    map[string]any{"apt": "neovim-old"},
			upper:    map[string]any{"apt": "neovim"},
			want:     map[string]any{"apt": "neovim"}},
		{name: "mapmerge/both-empty",
			strategy: MergeMapMerge,
			lower:    map[string]any{},
			upper:    map[string]any{},
			want:     map[string]any{}},

		// MergeUnionSlice
		{name: "unionslice/lower-only",
			strategy: MergeUnionSlice,
			lower:    []string{"a", "b"}, upper: nil,
			want: []string{"a", "b"}},
		{name: "unionslice/upper-only",
			strategy: MergeUnionSlice,
			lower: nil, upper: []string{"c"},
			want: []string{"c"}},
		{name: "unionslice/both-distinct",
			strategy: MergeUnionSlice,
			lower: []string{"a", "b"}, upper: []string{"c"},
			want: []string{"a", "b", "c"}},
		{name: "unionslice/dedup",
			strategy: MergeUnionSlice,
			lower: []string{"a", "b"}, upper: []string{"b", "c"},
			want: []string{"a", "b", "c"}},
		{name: "unionslice/both-empty",
			strategy: MergeUnionSlice,
			lower: []string{}, upper: []string{},
			want: []string{}},
		{name: "unionslice/upper-subset-of-lower",
			strategy: MergeUnionSlice,
			lower: []string{"a", "b", "c"}, upper: []string{"a"},
			want: []string{"a", "b", "c"}},

		// MergeLocalOnly
		{name: "localonly/lower-only",
			strategy: MergeLocalOnly,
			lower: "some-value", upper: nil,
			want: "some-value"},
		{name: "localonly/neither",
			strategy: MergeLocalOnly,
			lower: "", upper: "",
			want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyMergeStrategy(tt.strategy, tt.lower, tt.upper)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyMergeStrategy(%v, %v, %v) = %v, want %v",
					tt.strategy, tt.lower, tt.upper, got, tt.want)
			}
		})
	}
}

// TestMergeLayers_PkgOverridesPreserved — regression test ensuring manifest
// package overrides are merged with (not replaced by) schema overrides.
func TestMergeLayers_PkgOverridesPreserved(t *testing.T) {
	// Schema defines nvim with apt override
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim", requires = ["zsh"] }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	// Manifest defines nvim with pacman override (should complement, not replace)
	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	// Merge: manifest first (lower priority), schema last (higher priority)
	merged := MergeLayers(manifest, schema)
	nvim := merged.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim in merged schema")
	}

	// Find the native method
	var nativeMethod *MethodCandidate
	for _, m := range nvim.Methods {
		if m.Kind == "native" {
			nativeMethod = m
			break
		}
	}
	if nativeMethod == nil {
		t.Fatal("expected native method for nvim")
	}

	overrides, ok := nativeMethod.Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides in native method config")
	}

	// Both apt (from schema) AND pacman (from manifest) should survive
	if overrides["apt"] != "neovim" {
		t.Errorf("apt override lost: got %v, want 'neovim'", overrides["apt"])
	}
	if overrides["pacman"] != "neovim" {
		t.Errorf("pacman override missing: got %v, want 'neovim'", overrides["pacman"])
	}

	// Requires should come from schema (LocalOnly, schema sets it)
	if len(nvim.Requires) != 1 || nvim.Requires[0] != "zsh" {
		t.Errorf("requires should be [zsh] from schema, got %v", nvim.Requires)
	}
}

// TestMergeLayers_MethodKindUnion — different method kinds from both layers merge.
func TestMergeLayers_MethodKindUnion(t *testing.T) {
	// Schema: nvim with cargo method
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { cargo = "neovim" }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	// Manifest: nvim with pacman override
	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	merged := MergeLayers(manifest, schema)
	nvim := merged.Tools["nvim"]

	// Should have both native (from manifest merge) and cargo (from schema) methods
	kinds := make(map[string]bool)
	for _, m := range nvim.Methods {
		kinds[m.Kind] = true
	}
	if !kinds["native"] {
		t.Error("expected native method from merged layers")
	}
	if !kinds["cargo"] {
		t.Error("expected cargo method from schema")
	}
}

// TestAllToolFieldsHaveMergeStrategy — enforces completeness of ToolFieldStrategy.
func TestAllToolFieldsHaveMergeStrategy(t *testing.T) {
	toolType := reflect.TypeOf(Tool{})
	for i := 0; i < toolType.NumField(); i++ {
		field := toolType.Field(i)
		if !field.IsExported() {
			continue
		}
		if _, ok := ToolFieldStrategy[field.Name]; !ok {
			t.Errorf("Tool field %q has no merge strategy in ToolFieldStrategy", field.Name)
		}
	}
}

// TestMergeLayers_ManifestToolSchemaFieldOverwrite — schema field wins over
// manifest for same tool, but manifest additions survive.
func TestMergeLayers_ManifestToolSchemaFieldOverwrite(t *testing.T) {
	// Schema defines fd with apt override
	schemaPath := writeSchemaInline(t, `
[tools]
fd = { apt = "fd-find" }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	// Manifest defines same tool fd but with pacman override
	manifestPath := writeSchemaInline(t, `
[packages]
fd = { pacman = "fd-rust" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	merged := MergeLayers(manifest, schema)
	fd := merged.Tools["fd"]

	var nativeMethod *MethodCandidate
	for _, m := range fd.Methods {
		if m.Kind == "native" {
			nativeMethod = m
			break
		}
	}
	if nativeMethod == nil {
		t.Fatal("expected native method")
	}

	overrides, ok := nativeMethod.Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides")
	}

	// Schema's apt should be preserved, manifest's pacman should be added
	if overrides["apt"] != "fd-find" {
		t.Errorf("schema apt override lost: got %v, want 'fd-find'", overrides["apt"])
	}
	if overrides["pacman"] != "fd-rust" {
		t.Errorf("manifest pacman override missing: got %v, want 'fd-rust'", overrides["pacman"])
	}
}

// TestFilterManifestTools_RejectsWithoutFlag — FilterManifestTools removes tools
// from the manifest that are not in the schema when AllowNewTools is not set.
func TestFilterManifestTools_RejectsWithoutFlag(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim" }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
newtool = { pip = "new-pkg" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	// Without allow_new_tools, FilterManifestTools should remove "newtool"
	FilterManifestTools(schema, manifest)
	if _, exists := manifest.Tools["newtool"]; exists {
		t.Fatal("expected 'newtool' to be removed by FilterManifestTools, but it still exists")
	}
	// "nvim" should remain since it's in the schema
	if _, exists := manifest.Tools["nvim"]; !exists {
		t.Fatal("expected 'nvim' to remain after filtering, but it was removed")
	}
}

// TestValidateManifestNewTools_AllowsWithFlag — manifest introducing new tools
// succeeds when allow_new_tools is set.
func TestValidateManifestNewTools_AllowsWithFlag(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim" }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	manifestPath := writeSchemaInline(t, `
[manifest]
allow_new_tools = true

[packages]
nvim = { pacman = "neovim" }
newtool = { pip = "new-pkg" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	// With allow_new_tools, "newtool" should be allowed
	err = ValidateManifestNewTools(schema, manifest)
	if err != nil {
		t.Errorf("expected no error with allow_new_tools, got: %v", err)
	}
}

// TestValidateManifestNewTools_NoNewToolsIsFine — existing tools only in
// manifest is always fine.
func TestValidateManifestNewTools_NoNewToolsIsFine(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim" }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	err = ValidateManifestNewTools(schema, manifest)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

// TestMergeLayers_Provenance — provenance tracking records which layer each
// tool's fields came from.
func TestMergeLayers_Provenance(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
nvim = { apt = "neovim", requires = ["zsh"] }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}

	manifestPath := writeSchemaInline(t, `
[packages]
nvim = { pacman = "neovim" }
	`)
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	merged := MergeLayersWithProvenance(manifest, schema)

	if merged.Provenance == nil {
		t.Fatal("expected Provenance to be populated")
	}

	nvimProv, ok := merged.Provenance["nvim"]
	if !ok {
		t.Fatal("expected provenance for nvim")
	}

	// Should have at least one field source recorded
	if len(nvimProv) == 0 {
		t.Fatal("expected at least one provenance entry for nvim")
	}
}

// TestMergeLayers_TagsUnion verifies that Tags use MergeUnionSlice:
// tags from both layers are unioned without duplicates.
func TestMergeLayers_TagsUnion(t *testing.T) {
	schemaPath := writeSchemaInline(t, `
[tools]
rustup = { cargo = "rustup", tags = ["dev", "lang"] }
	`)
	manifestPath := writeSchemaInline(t, `
[packages]
rustup = { tags = ["personal", "lang"] }
	`)
	schema, err := ParseSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("ParseSchema(schema): %v", err)
	}
	manifest, err := ParseSchema(manifestPath, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}

	merged := MergeLayers(manifest, schema)

	tool, ok := merged.Tools["rustup"]
	if !ok {
		t.Fatal("expected rustup in merged result")
	}

	want := []string{"personal", "lang", "dev"}
	got := tool.Tags
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged Tags = %v, want %v", got, want)
	}
}

// applyMergeStrategy is a test helper that applies a MergeStrategy to generic
// lower/upper values for use in table-driven tests.
func applyMergeStrategy(s MergeStrategy, lower, upper any) any {
	switch s {
	case MergeOverwrite:
		if upper != nil && upper != "" {
			return upper
		}
		return lower
	case MergeLocalOnly:
		if upper != nil && upper != "" {
			return upper
		}
		return lower
	case MergeMapMerge:
		if upper == nil {
			return lower
		}
		if lower == nil {
			return upper
		}
		lowerMap, lowerOk := lower.(map[string]any)
		upperMap, upperOk := upper.(map[string]any)
		if lowerOk && upperOk {
			result := make(map[string]any, len(lowerMap)+len(upperMap))
			for k, v := range lowerMap {
				result[k] = v
			}
			for k, v := range upperMap {
				result[k] = v
			}
			return result
		}
		return upper
	case MergeUnionSlice:
		if upper == nil {
			return lower
		}
		if lower == nil {
			return upper
		}
		lowerSlice, lowerOk := lower.([]string)
		upperSlice, upperOk := upper.([]string)
		if lowerOk && upperOk {
			seen := make(map[string]bool, len(lowerSlice))
			result := make([]string, 0, len(lowerSlice)+len(upperSlice))
			for _, v := range lowerSlice {
				result = append(result, v)
				seen[v] = true
			}
			for _, v := range upperSlice {
				if !seen[v] {
					result = append(result, v)
					seen[v] = true
				}
			}
			return result
		}
		return upper
	default:
		if upper != nil {
			return upper
		}
		return lower
	}
}
