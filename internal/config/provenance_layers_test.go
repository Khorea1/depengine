package config

import (
	"path/filepath"
	"testing"
)

// lastFieldSource returns the final provenance entry for a field, which is the
// entry that carries the effective origin after every layer merged.
func lastFieldSource(t *testing.T, provenance []FieldSource, field string) FieldSource {
	t.Helper()
	var found *FieldSource
	for i := range provenance {
		if provenance[i].Field == field {
			found = &provenance[i]
		}
	}
	if found == nil {
		t.Fatalf("no provenance entry for field %q in %+v", field, provenance)
	}
	return *found
}

// TestResolveLayersProvenanceNamesCatalogAndOverrides proves that provenance
// names the real layer for every field across catalog, manifest and schema, and
// that a field overridden by a more specific layer reports both the effective
// origin and the discarded layers.
func TestResolveLayersProvenanceNamesCatalogAndOverrides(t *testing.T) {
	root := t.TempDir()
	recipePath := writeRecipe(t, root, "nvim", `
[packages.nvim]
method_prefer = ["native"]
tags = ["editor"]

[packages.nvim.native]
pkg = "neovim"
`)
	schema := mustParse(t, writeSchemaInline(t, `
[tools.nvim]
method_prefer = ["native"]
tags = ["editor", "project"]
`), ParseProjectSchema)
	manifest := mustParse(t, writeSchemaInline(t, `
[packages.nvim]
method_prefer = ["cargo"]
`), ParseManifest)

	res, err := ResolveLayers(schema, manifest, NewCatalog([]string{root}), nil, true)
	if err != nil {
		t.Fatalf("ResolveLayers: %v", err)
	}
	tool := res.Schema.Tools["nvim"]
	if tool == nil {
		t.Fatal("nvim missing from merged schema")
	}
	if tool.Recipe == nil {
		t.Fatal("expected catalog recipe identity on the merged tool")
	}
	if tool.Recipe.RootIndex != 0 || tool.Recipe.Root != root || tool.Recipe.Path != recipePath {
		t.Fatalf("recipe origin = %+v, want root %q index 0 path %q", tool.Recipe, root, recipePath)
	}
	if filepath.Base(tool.Recipe.Path) != "nvim.toml" || tool.Recipe.Digest == "" {
		t.Fatalf("recipe identity = %+v, want nvim.toml with a digest", tool.Recipe)
	}

	provenance := res.Schema.Provenance["nvim"]
	methodPrefer := lastFieldSource(t, provenance, "MethodPrefer")
	if methodPrefer.Source != LayerSchema {
		t.Fatalf("MethodPrefer origin = %q, want %q", methodPrefer.Source, LayerSchema)
	}
	for _, layer := range []string{LayerCatalog, LayerManifest, LayerSchema} {
		if _, ok := methodPrefer.Values[layer]; !ok {
			t.Errorf("MethodPrefer declared values missing layer %q: %+v", layer, methodPrefer.Values)
		}
	}
	if got := methodPrefer.Overridden; len(got) != 2 || got[0] != LayerCatalog || got[1] != LayerManifest {
		t.Fatalf("MethodPrefer overridden = %v, want [catalog manifest]", got)
	}

	// Tags are unioned: every declaring layer still contributes, so the field
	// is combined rather than overridden.
	tags := lastFieldSource(t, provenance, "Tags")
	if tags.Source != CombinedSource {
		t.Fatalf("Tags origin = %q, want %q", tags.Source, CombinedSource)
	}
	if len(tags.Overridden) != 0 {
		t.Fatalf("Tags overridden = %v, want none", tags.Overridden)
	}
	if _, ok := tags.Values[LayerCatalog]; !ok {
		t.Fatalf("Tags should keep the catalog value: %+v", tags.Values)
	}

	// Methods are only declared by the recipe, so their origin is the catalog.
	methods := lastFieldSource(t, provenance, "Methods")
	if methods.Source != LayerCatalog {
		t.Fatalf("Methods origin = %q, want %q", methods.Source, LayerCatalog)
	}
	if len(methods.Overridden) != 0 {
		t.Fatalf("Methods overridden = %v, want none", methods.Overridden)
	}
}

// TestMergeLayersProvenanceInfersLayerPositions documents the fallback used by
// callers that merge schemas directly instead of going through ResolveLayers.
func TestMergeLayersProvenanceInfersLayerPositions(t *testing.T) {
	lower := mustParse(t, writeSchemaInline(t, `
[tools.tool]
requires = ["a"]
`), ParseProjectSchema)
	upper := mustParse(t, writeSchemaInline(t, `
[tools.tool]
requires = ["b"]
`), ParseProjectSchema)

	merged := MergeLayersWithProvenance(lower, upper)
	entry := lastFieldSource(t, merged.Provenance["tool"], "Requires")
	if entry.Source != LayerSchema {
		t.Fatalf("Requires origin = %q, want %q", entry.Source, LayerSchema)
	}
	if len(entry.Overridden) != 1 || entry.Overridden[0] != LayerManifest {
		t.Fatalf("Requires overridden = %v, want [manifest]", entry.Overridden)
	}
}
