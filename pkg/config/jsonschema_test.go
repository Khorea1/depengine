package config

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/pkg/methodkind"
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
		t.Fatal("schema/depengine.schema.json is stale; run go generate ./pkg/config")
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
