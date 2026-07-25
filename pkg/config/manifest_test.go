package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeManifest writes a temp manifest file and returns its path.
func writeManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.toml")
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
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
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
	_, err := ParseSchema("/nonexistent/manifest.toml", nil, "packages")
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestParseSchema_ManifestInvalidTOML(t *testing.T) {
	p := writeManifest(t, `[[[invalid`)
	_, err := ParseSchema(p, nil, "packages")
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestParseSchema_ManifestNoPackages(t *testing.T) {
	p := writeManifest(t, `[other]
key = "value"
`)
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("expected nil error for missing [packages], got %v", err)
	}
	if len(s.Tools) != 0 {
		t.Fatalf("expected 0 tools for missing [packages], got %d", len(s.Tools))
	}
}

func TestParseSchema_ManifestPreservesIntentFields(t *testing.T) {
	p := writeManifest(t, `
[packages]
nvim = { pacman = "neovim", requires = ["zsh"], tags = ["desktop"] }
`)
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}
	nvim := s.Tools["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim in parsed manifest")
	}
	// ParseSchema preserves these fields (ValidateGlobalLayer catches them
	// at the merge step; see TestValidateGlobalLayer_RejectsIntentFields).
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
	s, err := ParseSchema(p, nil, "packages")
	if err != nil {
		t.Fatalf("ParseSchema(manifest): %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Schema for empty [packages]")
	}
	if len(s.Tools) != 0 {
		t.Fatalf("expected 0 tools for empty [packages], got %d", len(s.Tools))
	}
}
