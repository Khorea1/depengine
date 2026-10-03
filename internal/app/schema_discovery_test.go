package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSchemaCandidates(t *testing.T, dir string) {
	t.Helper()
	body := []byte("schema_version = 1\n\n[defaults]\nmanager = \"native\"\nmethod_order = [\"native\"]\n\n[tools]\n")
	for _, name := range []string{"schema.toml", "depengine.toml"} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestDefaultSchemaPathDiscoveryIsPure(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCandidates(t, dir)
	t.Chdir(dir)

	var got string
	stderr := captureStderr(t, func() { got = defaultSchemaPath() })
	if got != "schema.toml" {
		t.Fatalf("defaultSchemaPath = %q, want schema.toml", got)
	}
	if stderr != "" {
		t.Fatalf("command construction emitted stderr: %q", stderr)
	}
}

func TestAutoSchemaWarningOncePerInvocation(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCandidates(t, dir)
	t.Chdir(dir)

	stderr := captureStderr(t, func() {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"validate", "--no-manifest"})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	if got := strings.Count(stderr, "warning: multiple schema files found"); got != 1 {
		t.Fatalf("ambiguity warning count = %d, stderr = %q", got, stderr)
	}
}

func TestExplicitSchemaAndVersionDoNotWarnAboutAmbiguity(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCandidates(t, dir)
	t.Chdir(dir)

	stderr := captureStderr(t, func() {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"validate", "--no-manifest", "--schema", "schema.toml"})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("explicit validate: %v", err)
		}
		version := newRootCmd()
		version.SetArgs([]string{"version"})
		if err := version.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("version: %v", err)
		}
	})
	if strings.Contains(stderr, "multiple schema files found") {
		t.Fatalf("unexpected ambiguity warning: %q", stderr)
	}
}
