package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestLoadProjectRejectsMergeIntroducedEffectiveIntegrityError(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.toml")
	manifestPath := filepath.Join(dir, "manifest.toml")
	schema := "schema_version = 1\n\n[tools.x.github]\nrepo = \"o/r\"\nasset = \"base.tar.gz\"\nchecksum = \"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"\nscope = \"user\"\n"
	manifest := "schema_version = 1\n\n[packages.x.github]\nrepo = \"o/r\"\nasset = \"base.tar.gz\"\nchecksum = \"sha256:auto\"\nchecksum_url = \"https://example.com/SHA256SUMS\"\nscope = \"user\"\n\n[[packages.x.github.distributions]]\nwhen = { os = [\"windows\"] }\n"
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := config.ParseProjectSchema(schemaPath, nil)
	if err != nil {
		t.Fatalf("parse project layer: %v", err)
	}
	manifestLayer, err := config.ParseManifest(manifestPath, nil)
	if err != nil {
		t.Fatalf("parse manifest layer: %v", err)
	}
	if err := config.ValidateManifestLayer(manifestLayer); err != nil {
		t.Fatalf("manifest layer should be valid before merge: %v", err)
	}
	if err := config.ValidateManifestNewTools(base, manifestLayer); err != nil {
		t.Fatalf("manifest tool should be valid before merge: %v", err)
	}

	_, err = loadProject(schemaPath, projectLoadOptions{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "requires checksum") {
		t.Fatalf("loadProject merge error = %v, want effective checksum/sidecar diagnostic", err)
	}
}
