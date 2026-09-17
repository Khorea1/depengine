package config

import (
	"bytes"
	"os"
	"testing"
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
