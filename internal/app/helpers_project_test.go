package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSchemaFilePathCanonicalizesFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "nested", "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(projectDir, "schema.toml")
	if err := os.WriteFile(schemaPath, []byte("schema_version = 1\n[tools]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Clean(want)

	gotDir, err := resolveSchemaFilePath(projectDir)
	if err != nil {
		t.Fatalf("directory input: %v", err)
	}
	if gotDir != want {
		t.Fatalf("directory resolved to %q, want %q", gotDir, want)
	}

	t.Chdir(root)
	relative := filepath.Join("nested", "project", "..", "project", "schema.toml")
	gotFile, err := resolveSchemaFilePath(relative)
	if err != nil {
		t.Fatalf("relative file input: %v", err)
	}
	if gotFile != want {
		t.Fatalf("relative file resolved to %q, want %q", gotFile, want)
	}
	if !filepath.IsAbs(gotFile) {
		t.Fatalf("resolved schema path is not absolute: %q", gotFile)
	}
}

func TestResolveSchemaFilePathRejectsDirectoryWithoutSchema(t *testing.T) {
	_, err := resolveSchemaFilePath(t.TempDir())
	if err == nil {
		t.Fatal("expected missing schema.toml error")
	}
}
