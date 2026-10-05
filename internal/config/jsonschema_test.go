package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/methodkind"
)

func TestGeneratedJSONSchemaIsCurrent(t *testing.T) {
	want, err := GenerateJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../schema/depengine.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("schema/depengine.schema.json is stale; run go generate ./internal/config")
	}
}

func TestMethodJSONSchemaUsesContractSourceAlternatives(t *testing.T) {
	for _, tt := range []struct {
		kind string
		want [][]string
	}{
		{kind: "http", want: [][]string{{"url"}, {"repo", "asset"}}},
		{kind: "github", want: [][]string{{"repo", "asset"}}},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			contract, _ := methodkind.Lookup(tt.kind)
			options := methodObjectJSONSchema(contract, "")["oneOf"].([]any)
			got := make([][]string, len(options))
			for i, option := range options {
				got[i] = option.(map[string]any)["required"].([]string)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("required source alternatives = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExamplesMatchRuntimeGrammar(t *testing.T) {
	tests := []struct {
		path  string
		parse func(string, map[string]string) (*Schema, error)
	}{
		{path: "../../schema.example.toml", parse: ParseProjectSchema},
		{path: "../../manifest.example.toml", parse: ParseManifest},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if _, err := tt.parse(tt.path, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMethodJSONSchemaIncludesContractCapabilities(t *testing.T) {
	data, err := GenerateJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	definitions := root["definitions"].(map[string]any)
	choco := definitions[methodDefinition("choco")].(map[string]any)
	options := choco["oneOf"].([]any)
	var description string
	for _, raw := range options {
		option := raw.(map[string]any)
		if option["type"] == "object" {
			description, _ = option["description"].(string)
			break
		}
	}
	for _, want := range []string{"exact-version", "source-selection", "architecture"} {
		if !strings.Contains(description, want) {
			t.Fatalf("choco schema description %q missing capability %q", description, want)
		}
	}
}

func TestMethodJSONSchemaIncludesFieldDependencies(t *testing.T) {
	contract, ok := methodkind.Lookup("cargo")
	if !ok {
		t.Fatal("missing cargo contract")
	}
	schema := methodObjectJSONSchema(contract, "")
	allOf, ok := schema["allOf"].([]any)
	if !ok {
		t.Fatalf("cargo schema missing allOf dependencies: %#v", schema["allOf"])
	}
	found := false
	for _, raw := range allOf {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ifPart, _ := rule["if"].(map[string]any)
		thenPart, _ := rule["then"].(map[string]any)
		if reflect.DeepEqual(ifPart["required"], []string{"rev"}) && reflect.DeepEqual(thenPart["required"], []string{"git"}) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cargo schema does not encode rev -> git dependency: %#v", allOf)
	}
}
