package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const intentGoldenPath = "testdata/catalog-intent-equivalence.golden.json"

func methodsJSON(t *testing.T, methods any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(methods, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// TestIntentSchemaGoldenEquivalence proves that a project schema which only
// declares intent resolves to the same candidate sequence as the equivalent
// enumerated schema. The golden file is the normalized resolved methods.
func TestIntentSchemaGoldenEquivalence(t *testing.T) {
	root := t.TempDir()
	writeRecipe(t, root, "foo", "[packages.foo]\ntags = [\"cli\"]\n[packages.foo.native]\npkg = \"foo-pkg\"\n[packages.foo.github]\nrepo = \"owner/foo\"\nasset = \"foo-{version}.tar.gz\"\nchecksum = \"sha256:"+strings.Repeat("a", 64)+"\"\n")

	intent := mustParse(t, writeSchemaInline(t, "[tools]\nfoo = {}\n"), ParseProjectSchema)
	enumerated := mustParse(t, writeSchemaInline(t, "[tools.foo]\ntags = [\"cli\"]\n[tools.foo.native]\npkg = \"foo-pkg\"\n[tools.foo.github]\nrepo = \"owner/foo\"\nasset = \"foo-{version}.tar.gz\"\nchecksum = \"sha256:"+strings.Repeat("a", 64)+"\"\n"), ParseProjectSchema)

	intentResolution, err := ResolveLayers(intent, nil, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	enumeratedResolution, err := ResolveLayers(enumerated, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	got := methodsJSON(t, intentResolution.Schema.Tools["foo"].Methods)
	want := methodsJSON(t, enumeratedResolution.Schema.Tools["foo"].Methods)
	if !bytes.Equal(got, want) {
		t.Fatalf("intent schema resolved differently from enumerated schema\nintent:\n%s\nenumerated:\n%s", got, want)
	}
	if intentResolution.CatalogCount != 1 {
		t.Fatalf("catalog count = %d, want 1", intentResolution.CatalogCount)
	}

	if os.Getenv("UPDATE_CATALOG_INTENT_GOLDEN") == "1" {
		if err := os.WriteFile(intentGoldenPath, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(intentGoldenPath) // #nosec G304 -- test-owned golden path.
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_CATALOG_INTENT_GOLDEN=1): %v", err)
	}
	if !bytes.Equal(got, golden) {
		t.Fatalf("resolved methods differ from golden %s\nwant:\n%s\ngot:\n%s", filepath.Clean(intentGoldenPath), golden, got)
	}
}

func TestResolveLayersWarnsAboutInertTool(t *testing.T) {
	root := t.TempDir()
	schema := mustParse(t, writeSchemaInline(t, "[tools]\nfoo = {}\nbar = { requires = [] }\nbaz = { method_only = [\"http\"] }\n"), ParseProjectSchema)

	resolution, err := ResolveLayers(schema, nil, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	byTool := make(map[string]string, len(resolution.Warnings))
	for _, warning := range resolution.Warnings {
		byTool[warning.Tool] = warning.Message
	}
	if _, ok := byTool["bar"]; ok {
		t.Fatalf("explicit empty requires should suppress the inert-tool warning: %v", byTool["bar"])
	}
	foo, ok := byTool["foo"]
	if !ok || !strings.Contains(foo, "no catalog recipe supplied it") {
		t.Fatalf("foo warning = %q, want missing-recipe diagnostic", foo)
	}
	baz, ok := byTool["baz"]
	if !ok || !strings.Contains(baz, "restricts methods to http") {
		t.Fatalf("baz warning = %q, want method_only diagnostic", baz)
	}

	writeRecipe(t, root, "foo", "[packages.foo]\nnative = \"foo\"\n")
	resolution, err = ResolveLayers(schema, nil, NewCatalog([]string{root}), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range resolution.Warnings {
		if warning.Tool == "foo" {
			t.Fatalf("resolved recipe should suppress the inert-tool warning: %q", warning.Message)
		}
	}
}
