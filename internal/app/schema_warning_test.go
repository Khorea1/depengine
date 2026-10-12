package app

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	depLog "github.com/Khorea1/depengine/internal/log"
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

func TestWhyShowsOnlySelectedToolValidationWarnings(t *testing.T) {
	dir := t.TempDir()
	schema := "schema_version = 1\n\n[tools.relevant.http]\nurl = \"https://example.test/relevant.tar.gz\"\nchecksum = \"sha256:auto\"\n\n[tools.other.http]\nurl = \"https://example.test/other.tar.gz\"\nchecksum = \"sha256:6ca13d52ca70c883e0f0bb101e425a89e8624de51db2d2392593af6a84118090\"\n"
	path := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(path, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tool        string
		wantWarning bool
	}{{"relevant", true}, {"other", false}} {
		t.Run(tc.tool, func(t *testing.T) {
			var stderr bytes.Buffer
			previousLogger := depLog.Default
			depLog.Default = depLog.New(&stderr, slog.LevelInfo)
			t.Cleanup(func() { depLog.Default = previousLogger })
			cmd := newRootCmd()
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"why", tc.tool, "--schema", path, "--no-manifest"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			got := strings.Contains(stderr.String(), "W_AUTO_CHECKSUM")
			if got != tc.wantWarning {
				t.Fatalf("selected warning present = %t, want %t; stderr=%q", got, tc.wantWarning, stderr.String())
			}
		})
	}
}
