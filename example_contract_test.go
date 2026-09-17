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

	covered := make(map[string]bool, len(methodkind.Contracts))
	for _, tt := range tests {
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
		})
	}

	var missing []string
	for _, contract := range methodkind.Contracts {
		if !covered[contract.Kind] {
			missing = append(missing, contract.Kind)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("public method contracts missing from shipped examples: %s", strings.Join(missing, ", "))
	}
}

func formatValidationErrors(errors []validate.ValidationError) string {
	lines := make([]string, len(errors))
	for i, err := range errors {
		lines[i] = fmt.Sprintf("%s: %s: %s", err.Code, err.Field, err.Message)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
