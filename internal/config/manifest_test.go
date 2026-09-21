package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest writes a temp manifest file and returns its path.
func writeManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.toml")
	if !strings.Contains(content, "schema_version") {
		content = "schema_version = 1\n" + content
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return p
}

func TestParseSchema_ManifestPackages(t *testing.T) {
	p := writeManifest(t, `
[packages]
nvim = { pacman = "neovim", apt = "neovim" }
fd = { cargo = "fd-find" }
`)
	s, err := ParseManifest(p, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(manifest): %v", err)
	}
	if len(s.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(s.Tools))
	}

	nvim, ok := s.Tools["nvim"]
	if !ok {
		t.Fatal("expected nvim in manifest")
	}
	if len(nvim.Methods) != 1 {
		t.Fatalf("expected 1 method for nvim, got %d", len(nvim.Methods))
	}
	if nvim.Methods[0].Kind != "native" {
		t.Fatalf("expected native kind, got %q", nvim.Methods[0].Kind)
	}
	overrides, ok := nvim.Methods[0].Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides")
	}
	if overrides["pacman"] != "neovim" || overrides["apt"] != "neovim" {
		t.Fatalf("unexpected overrides: %v", overrides)
	}
}

func TestParseSchema_ManifestNonExistentFile(t *testing.T) {
	_, err := ParseManifest("/nonexistent/manifest.toml", nil)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestParseSchema_ManifestInvalidTOML(t *testing.T) {
	p := writeManifest(t, `[[[invalid`)
	_, err := ParseManifest(p, nil)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestParseSchema_ManifestRejectsUnknownRoot(t *testing.T) {
	p := writeManifest(t, `[other]
key = "value"
`)
	if _, err := ParseManifest(p, nil); err == nil {
		t.Fatal("expected unknown root field error")
	}
}

func TestParseManifestRejectsProjectSection(t *testing.T) {
	p := writeManifest(t, "schema_version = 1\n[tools]\n")
	if _, err := ParseManifest(p, nil); err == nil || !strings.Contains(err.Error(), "tools: unknown root field") {
		t.Fatalf("wrong section error = %v", err)
	}
}

func TestParseSchema_ManifestPreservesIntentFields(t *testing.T) {
	p := writeManifest(t, `
[packages]
nvim = { pacman = "neovim", requires = ["zsh"], tags = ["desktop"] }
`)
	s, err := ParseManifest(p, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(manifest): %v", err)
	}
	nvim := s.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim in parsed manifest")
	}
	// ParseSchema preserves these fields (ValidateManifestLayer catches them
	// at the merge step; see TestValidateManifestLayer_RejectsIntentFields).
	if nvim.Requires == nil || len(nvim.Requires) != 1 || nvim.Requires[0] != "zsh" {
		t.Fatalf("expected requires to be preserved by parser, got %v", nvim.Requires)
	}
	if len(nvim.Tags) != 1 || nvim.Tags[0] != "desktop" {
		t.Fatalf("expected tags to be preserved by parser, got %v", nvim.Tags)
	}
}

func TestParseSchema_ManifestPackagesSectionEmpty(t *testing.T) {
	p := writeManifest(t, `
[packages]
`)
	s, err := ParseManifest(p, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(manifest): %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Schema for empty [packages]")
	}
	if len(s.Tools) != 0 {
		t.Fatalf("expected 0 tools for empty [packages], got %d", len(s.Tools))
	}
}

func TestMergeLayersWithProvenanceThreeLayers(t *testing.T) {
	// Three layers where the same tool appears in all three with different fields.
	// This tests that provenance accumulates across merges rather than being
	// overwritten by the last merge (the bug at line 153).

	l1p := writeSchemaInline(t, `
[tools]
vim = { pre_install = "echo l1" }
`)
	l1, err := ParseProjectSchema(l1p, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(layer1): %v", err)
	}

	l2p := writeSchemaInline(t, `
[tools]
vim = { post_install = "echo l2" }
`)
	l2, err := ParseProjectSchema(l2p, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(layer2): %v", err)
	}

	l3p := writeSchemaInline(t, `
[tools]
vim = { requires = ["g"] }
`)
	l3, err := ParseProjectSchema(l3p, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema(layer3): %v", err)
	}

	merged := MergeLayersWithProvenance(l1, l2, l3)

	prov, ok := merged.Provenance["vim"]
	if !ok {
		t.Fatal("expected provenance for vim")
	}

	// With three layers, the tool goes through two merges.
	// Each merge records a provenance entry per field.
	// With the bug (assignment instead of append), only the last merge's
	// entries survive, so PreInstall would appear only once.
	preCount := 0
	for _, fs := range prov {
		if fs.Field == "PreInstall" {
			preCount++
		}
	}
	if preCount < 2 {
		t.Errorf("PreInstall provenance from multiple merges: got %d occurrence(s), want >= 2 (provenance was overwritten, not accumulated)",
			preCount)
	}
}
