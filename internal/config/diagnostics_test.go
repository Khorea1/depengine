package config

import (
	"strings"
	"testing"
)

func TestSchemaDiagnosticsIncludePositionSuggestionExampleAndDocs(t *testing.T) {
	path := writeSchema(t, "[tools.zsh]\ntagz = [\"shell\"]\n")
	_, err := ParseProjectSchema(path, nil)
	if err == nil {
		t.Fatal("expected invalid schema error")
	}
	message := err.Error()
	for _, want := range []string{
		"3:1: tools.zsh.tagz:",
		`did you mean "tags"?`,
		`example: tags = ["cli"]`,
		"docs: docs/schema-reference.md#per-tool-method-control",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("error missing %q:\n%s", want, message)
		}
	}
}

func TestSchemaDiagnosticsPositionWrongShape(t *testing.T) {
	path := writeSchema(t, "[tools]\nzsh = 1\n")
	_, err := ParseProjectSchema(path, nil)
	if err == nil {
		t.Fatal("expected invalid schema error")
	}
	message := err.Error()
	if !strings.Contains(message, "3:1: tools.zsh: expected table, got int64") {
		t.Errorf("error missing position/type:\n%s", message)
	}
	if !strings.Contains(message, "example:\n    [tools.zsh]") {
		t.Errorf("error missing table example:\n%s", message)
	}
}

func TestSchemaDiagnosticsWorkWithoutFileSource(t *testing.T) {
	raw := []byte("recipe_version = 1\n[packages.zsh]\ntagz = [\"shell\"]\n")
	_, err := ParseRecipeBytes("/virtual/recipe.toml", raw, nil)
	if err == nil {
		t.Fatal("expected invalid recipe error")
	}
	if !strings.Contains(err.Error(), `did you mean "tags"?`) {
		t.Errorf("recipe diagnostics missing suggestion:\n%s", err)
	}
}

func TestSchemaDiagnosticsSuggestOnlyForUnknownFields(t *testing.T) {
	path := writeSchema(t, "[tools.zsh]\ntags = \"cli\"\n")
	_, err := ParseProjectSchema(path, nil)
	if err == nil {
		t.Fatal("expected invalid schema error")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("valid field with wrong type got a suggestion:\n%s", err)
	}
	if !strings.Contains(err.Error(), `example: tags = ["value"]`) {
		t.Errorf("wrong-type error missing example:\n%s", err)
	}
}
