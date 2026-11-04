package config

import (
	"testing"
)

// TestCatalogManifestProjectPrecedence pins the layer contract: catalog recipes
// are the lowest layer, the personal manifest overrides them, and the project
// schema wins conflicting fields. The project schema also owns project metadata,
// so a catalog layer can never enable allow_new_tools.
func TestCatalogManifestProjectPrecedence(t *testing.T) {
	catalogPath := writeSchemaInline(t, `
[manifest]
allow_new_tools = true

[packages]
foo = { pip = "catalog-pkg", tags = ["catalog"] }
`)
	catalog, err := ParseManifest(catalogPath, nil)
	if err != nil {
		t.Fatalf("ParseManifest(catalog): %v", err)
	}

	manifestPath := writeSchemaInline(t, `
[packages]
foo = { pip = "manifest-pkg" }
`)
	manifest, err := ParseManifest(manifestPath, nil)
	if err != nil {
		t.Fatalf("ParseManifest(manifest): %v", err)
	}

	projectPath := writeSchemaInline(t, `
[tools]
foo = { pip = "project-pkg" }
`)
	project, err := ParseProjectSchema(projectPath, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(project): %v", err)
	}

	merged := MergeLayers(catalog, manifest, project)
	if merged.AllowNewTools {
		t.Fatal("catalog layer must not enable allow_new_tools for the project")
	}
	tool := merged.Tools["foo"]
	if tool == nil {
		t.Fatal("expected foo after merge")
	}
	var pkg string
	for _, method := range tool.Methods {
		if method.Kind == "pip" {
			pkg, _ = method.Config["pkg"].(string)
			break
		}
	}
	if pkg != "project-pkg" {
		t.Fatalf("pip pkg = %q; want project-pkg (schema wins over manifest and catalog)", pkg)
	}
	if !containsString(tool.Tags, "catalog") {
		t.Fatalf("tags = %v; want inherited catalog tag", tool.Tags)
	}
}

// TestCandidateSequenceIndependentOfHostFacts is the config-level parity check
// between validate (no host facts) and execution (host facts): the same schema
// must yield the same candidate sequence in both paths.
func TestCandidateSequenceIndependentOfHostFacts(t *testing.T) {
	path := writeSchema(t, `
[defaults]
method_prefer = ["github", "native"]

[tools]
foo = { github = { repo = "o/foo", asset = "foo.tar.gz", checksum = "sha256:auto", scope = "user", entrypoints = { foo = "foo" } }, cargo = true }
`)

	validateSchema, err := ParseProjectSchema(path, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(validate): %v", err)
	}
	execSchema, err := ParseProjectSchema(path, fixedMap())
	if err != nil {
		t.Fatalf("ParseProjectSchema(exec): %v", err)
	}

	validateMethods := validateSchema.Tools["foo"].Methods
	execMethods := execSchema.Tools["foo"].Methods
	if len(validateMethods) != len(execMethods) {
		t.Fatalf("candidate count differs: validate=%d exec=%d", len(validateMethods), len(execMethods))
	}
	for i := range validateMethods {
		if validateMethods[i].Kind != execMethods[i].Kind || validateMethods[i].Label != execMethods[i].Label {
			t.Fatalf("candidate %d differs: validate=%s/%s exec=%s/%s", i,
				validateMethods[i].Kind, validateMethods[i].Label,
				execMethods[i].Kind, execMethods[i].Label)
		}
	}
}
