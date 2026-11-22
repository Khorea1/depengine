package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBuiltinAndRepositoryCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	body := "schema_version = 1\n[tools]\ndepengine = {}\nnvim = {}\ngolang = {}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	output, code := runDepengine("validate", "--schema", path, "--no-manifest")
	if code != 0 || !strings.Contains(output, "builtin (1 recipe)") || strings.Count(output, "W_INERT_TOOL") != 2 {
		t.Fatalf("expected one builtin and two inert tools, exit=%d:\n%s", code, output)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	output, code = runDepengine("validate", "--schema", path, "--no-manifest", "--catalog", root)
	if code != 0 || strings.Count(output, "W_INERT_TOOL") != 1 || !strings.Contains(output, "(2 recipes)") {
		t.Fatalf("expected two repository recipes and one inert tool, exit=%d:\n%s", code, output)
	}
	output, code = runDepengine("validate", "--schema", path, "--no-manifest", "--no-catalog")
	if code != 0 || strings.Count(output, "W_INERT_TOOL") != 3 || strings.Contains(output, "builtin") {
		t.Fatalf("expected three inert tools with --no-catalog, exit=%d:\n%s", code, output)
	}
	output, code = runDepengine("validate", "--schema", path, "--no-manifest", "--format=json")
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("expected clean JSON envelope with default catalog, exit=%d:\n%s", code, output)
	}
}
