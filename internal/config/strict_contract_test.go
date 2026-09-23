package config

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/methodkind"
)

func TestParseMethodContractsEnforceRequiredFields(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		var required []string
		for name, field := range contract.Fields {
			if field.Required {
				required = append(required, name)
			}
		}
		if len(required) == 0 {
			continue
		}
		sort.Strings(required)

		t.Run(contract.Kind, func(t *testing.T) {
			path := writeTempSchema(t, fmt.Sprintf("[tools.demo.%s]\n", contract.Kind))
			_, err := ParseProjectSchema(path, nil)
			if err == nil {
				t.Fatalf("ParseProjectSchema() accepted %s without required fields %v", contract.Kind, required)
			}
			for _, field := range required {
				want := fmt.Sprintf("tools.demo.%s.%s: required field", contract.Kind, field)
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ParseProjectSchema() error = %v, want %q", err, want)
				}
			}
		})
	}
}

func TestParseMethodContractsEnforceNonEmptyFields(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		for name, field := range contract.Fields {
			if !field.NonEmpty {
				continue
			}
			if field.Type != methodkind.String {
				t.Fatalf("%s.%s: NonEmpty requires a string field, got %s", contract.Kind, name, field.Type)
			}

			t.Run(contract.Kind+"/"+name, func(t *testing.T) {
				path := writeTempSchema(t, fmt.Sprintf("[tools.demo.%s]\n%s = \"\"\n", contract.Kind, name))
				_, err := ParseProjectSchema(path, nil)
				if err == nil {
					t.Fatalf("ParseProjectSchema() accepted empty %s.%s", contract.Kind, name)
				}
				want := fmt.Sprintf("tools.demo.%s.%s: must not be empty", contract.Kind, name)
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("ParseProjectSchema() error = %v, want %q", err, want)
				}
			})
		}
	}
}

// Artifact source-choice validation must follow the contracts, not a
// method-name list: every artifact kind rejects an empty source choice and no
// other kind does.
func TestValidateArtifactChoiceFollowsContracts(t *testing.T) {
	for i := range methodkind.Contracts {
		contract := &methodkind.Contracts[i]
		var errs []string
		validateArtifactChoice(map[string]any{}, "tools.x.methods[0]", contract, &errs)
		if got, want := len(errs) > 0, contract.Artifact != nil; got != want {
			t.Errorf("%s: choice errors = %v (%v), want errors=%v", contract.Kind, got, errs, want)
		}
	}
}
