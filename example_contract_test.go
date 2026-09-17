package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/validate"
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
