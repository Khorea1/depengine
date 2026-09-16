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
	if _, err := ParseProjectSchema("../../schema.example.toml", nil); err != nil {
		t.Fatalf("schema.example.toml: %v", err)
	}
	if _, err := ParseManifest("../../manifest.example.toml", nil); err != nil {
		t.Fatalf("manifest.example.toml: %v", err)
	}
}
