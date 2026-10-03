package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withSchemaWarningDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"schema.toml", "depengine.toml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("schema_version = 1\n[tools]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestMultipleSchemaWarningNotEmittedForVersion(t *testing.T) {
	withSchemaWarningDir(t)
	var stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "multiple schema files found") {
		t.Fatalf("version emitted schema ambiguity warning: %s", stderr.String())
	}
}

func TestMultipleSchemaWarningEmittedOnceForSchemaCommand(t *testing.T) {
	withSchemaWarningDir(t)
	var stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"validate"})
	_ = cmd.Execute()
	if got := strings.Count(stderr.String(), "multiple schema files found"); got != 1 {
		t.Fatalf("warning count = %d, want 1; stderr=%q", got, stderr.String())
	}
}

func TestExplicitSchemaSuppressesAmbiguityWarning(t *testing.T) {
	withSchemaWarningDir(t)
	var stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"validate", "--schema", "schema.toml"})
	_ = cmd.Execute()
	if strings.Contains(stderr.String(), "multiple schema files found") {
		t.Fatalf("explicit --schema emitted ambiguity warning: %s", stderr.String())
	}
}
