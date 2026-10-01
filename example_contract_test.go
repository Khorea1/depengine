package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/validate"
)

const (
	docMinimalProject = `schema_version = 1

[tools]
simple = ["zsh", "bat"]
`
	docGitHubRelease = `schema_version = 1

[tools]
yq = { method_only = ["github"], github = { repo = "mikefarah/yq", asset = "yq_{os_any}_{arch_any}" } }
`
	docOrderedHooks = `schema_version = 1

[defaults]
method_order = ["native", "cargo", "github", "http"]

[tools]
myapp = { method_prefer = ["cargo"], pre_install = { run = ["test", "-x", "/usr/bin/cargo"] }, post_install = { run = ["myapp", "--version"] }, cargo = true }
`
	docManifestNewTools = `schema_version = 1

[manifest]
allow_new_tools = true

[packages]
personal-tool = { git = { url = "https://github.com/example/personal-tool", depth = 1 } }
`
)

func TestShippedExamplesMatchRuntimeContract(t *testing.T) {
	tests := []struct {
		path     string
		parse    func(string, map[string]string) (*config.Schema, error)
		manifest bool
	}{
		{path: "schema.example.toml", parse: config.ParseProjectSchema},
		{path: "manifest.example.toml", parse: config.ParseManifest, manifest: true},
	}

	coveredByExample := make(map[string]map[string]bool, len(tests))
	for _, tt := range tests {
		covered := make(map[string]bool, len(methodkind.Contracts))
		coveredByExample[tt.path] = covered
		t.Run(tt.path, func(t *testing.T) {
			schema, err := tt.parse(tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.manifest {
				if err := config.ValidateManifestLayer(schema); err != nil {
					t.Fatalf("manifest layer: %v", err)
				}
			}
			result := validate.ValidateSchema(schema, exec.RegisteredKinds())
			if result.HasErrors() {
				t.Fatalf("semantic validation:\n%s", formatValidationErrors(result.Errors))
			}
			for _, tool := range schema.Tools {
				for _, method := range tool.Methods {
					if contract, ok := methodkind.Lookup(method.Kind); ok {
						covered[contract.Kind] = true
					}
				}
			}
			if !covered["github"] {
				t.Fatalf("%s: missing canonical github method contract", tt.path)
			}
			identities := neovimToolIdentities(schema)
			if len(identities) != 1 || identities[0] != "neovim" {
				t.Fatalf("%s: expected one canonical Neovim tool identity named neovim, got %v", tt.path, identities)
			}
		})
	}

	var missing []string
	for _, contract := range methodkind.Contracts {
		resolved, ok := methodkind.Lookup(contract.Kind)
		if !ok || resolved.Kind != contract.Kind {
			t.Fatalf("methodkind.Contracts entry %q does not round-trip through methodkind.Lookup", contract.Kind)
		}
		covered := false
		for _, exampleCoverage := range coveredByExample {
			covered = covered || exampleCoverage[contract.Kind]
		}
		if !covered {
			missing = append(missing, contract.Kind)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("public method contracts missing from shipped examples: %s", strings.Join(missing, ", "))
	}
}

func TestSchemaReferenceCompleteExamplesMatchRuntimeContract(t *testing.T) {
	tests := []struct {
		name     string
		document string
		parse    func(string, map[string]string) (*config.Schema, error)
		manifest bool
	}{
		{name: "minimal project schema", document: docMinimalProject, parse: config.ParseProjectSchema},
		{name: "GitHub release schema", document: docGitHubRelease, parse: config.ParseProjectSchema},
		{name: "ordered candidates and hooks", document: docOrderedHooks, parse: config.ParseProjectSchema},
		{name: "manifest admitting new tools", document: docManifestNewTools, parse: config.ParseManifest, manifest: true},
	}
	reference, err := os.ReadFile("docs/schema-reference.md")
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(string(reference), "```toml\n"+tt.document+"```") {
				t.Fatalf("docs/schema-reference.md %s: documented fixture differs from tested TOML", tt.name)
			}
			schema, err := tt.parse(writeContractFixture(t, tt.document), nil)
			if err != nil {
				t.Fatalf("docs/schema-reference.md %s: parse: %v", tt.name, err)
			}
			if tt.manifest {
				if err := config.ValidateManifestLayer(schema); err != nil {
					t.Fatalf("docs/schema-reference.md %s: manifest layer: %v", tt.name, err)
				}
			}
			result := validate.ValidateSchema(schema, exec.RegisteredKinds())
			if result.HasErrors() {
				t.Fatalf("docs/schema-reference.md %s: semantic validation:\n%s", tt.name, formatValidationErrors(result.Errors))
			}
		})
	}
}

func TestREADMEQuickStartMatchesRuntimeContract(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("README Quick Start: read: %v", err)
	}
	document, err := firstTOMLBlockInREADMEQuickStart(string(readme))
	if err != nil {
		t.Fatalf("README Quick Start: %v", err)
	}

	schema, err := config.ParseProjectSchema(writeContractFixture(t, document), nil)
	if err != nil {
		t.Fatalf("README Quick Start: parse: %v", err)
	}
	result := validate.ValidateSchema(schema, exec.RegisteredKinds())
	if result.HasErrors() {
		t.Fatalf("README Quick Start: semantic validation:\n%s", formatValidationErrors(result.Errors))
	}
}

// firstTOMLBlockInREADMEQuickStart extracts only the first TOML fence in the
// README Quick start section; it is deliberately not a general Markdown parser.
func firstTOMLBlockInREADMEQuickStart(markdown string) (string, error) {
	const heading = "## Quick start"
	const headingLine = "\n" + heading + "\n"
	headingStart := strings.Index(markdown, headingLine)
	if headingStart == -1 {
		return "", fmt.Errorf("section %q not found", heading)
	}
	section := markdown[headingStart+len(headingLine):]
	if nextHeading := strings.Index(section, "\n## "); nextHeading >= 0 {
		section = section[:nextHeading]
	}
	const openingFence = "```toml\n"
	fenceStart := strings.Index(section, openingFence)
	if fenceStart == -1 {
		return "", fmt.Errorf("first TOML code block not found in %q", heading)
	}
	contentStart := fenceStart + len(openingFence)
	closingFence := strings.Index(section[contentStart:], "\n```")
	if closingFence == -1 {
		return "", fmt.Errorf("first TOML code block in %q is unterminated", heading)
	}
	return section[contentStart : contentStart+closingFence+1], nil
}

func TestDocumentedPlaceholderOwnership(t *testing.T) {
	tests := []struct {
		name        string
		document    string
		wantWarning bool
	}{
		{
			name:        "literal URL rejects asset matching placeholders",
			document:    "schema_version = 1\n[tools]\napp = { http = { url = \"https://example.com/app-{version}-{os_any}-{arch_any}.tar.gz\" } }\n",
			wantWarning: true,
		},
		{name: "GitHub asset accepts matching placeholders", document: docGitHubRelease},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := config.ParseProjectSchema(writeContractFixture(t, tt.document), nil)
			if err != nil {
				t.Fatal(err)
			}
			result := validate.ValidateSchema(schema, exec.RegisteredKinds())
			if result.HasErrors() {
				t.Fatalf("semantic validation:\n%s", formatValidationErrors(result.Errors))
			}
			gotWarning := false
			for _, warning := range result.Warnings {
				gotWarning = gotWarning || warning.Code == validate.WarnUnknownPlaceholder
			}
			if gotWarning != tt.wantWarning {
				t.Fatalf("placeholder warning = %v, want %v; warnings: %v", gotWarning, tt.wantWarning, result.Warnings)
			}
		})
	}
}

func TestDocumentedManifestNewToolPolicy(t *testing.T) {
	project, err := config.ParseProjectSchema(writeContractFixture(t, docMinimalProject), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name     string
		document string
		wantTool bool
	}{
		{name: "excluded by default", document: "schema_version = 1\n[packages]\npersonal-tool = { cargo = true }\n"},
		{name: "admitted explicitly", document: docManifestNewTools, wantTool: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := config.ParseManifest(writeContractFixture(t, tt.document), nil)
			if err != nil {
				t.Fatal(err)
			}
			config.FilterManifestTools(project, manifest)
			_, gotTool := manifest.Tools["personal-tool"]
			if gotTool != tt.wantTool {
				t.Fatalf("manifest-only tool retained = %v, want %v", gotTool, tt.wantTool)
			}
		})
	}
}

func writeContractFixture(t *testing.T, document string) string {
	t.Helper()
	fixture, err := os.CreateTemp(t.TempDir(), "contract-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.WriteString(document); err != nil {
		_ = fixture.Close()
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	return fixture.Name()
}

func neovimToolIdentities(schema *config.Schema) []string {
	var identities []string
	for name, tool := range schema.Tools {
		if name == "nvim" || strings.Contains(strings.ToLower(name), "neovim") {
			identities = append(identities, name)
			continue
		}
		for _, method := range tool.Methods {
			if methodConfigRepresentsNeovim(method.Config) {
				identities = append(identities, name)
				break
			}
		}
	}
	sort.Strings(identities)
	return identities
}

func methodConfigRepresentsNeovim(config map[string]any) bool {
	for _, key := range []string{"pkg", "repo", "url"} {
		value, _ := config[key].(string)
		value = strings.ToLower(value)
		if value == "neovim" || value == "neovim.neovim" || strings.Contains(value, "github.com/neovim/neovim") || value == "neovim/neovim" {
			return true
		}
	}
	return false
}

func formatValidationErrors(errors []validate.ValidationError) string {
	lines := make([]string, len(errors))
	for i, err := range errors {
		lines[i] = fmt.Sprintf("%s: %s: %s", err.Code, err.Field, err.Message)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
