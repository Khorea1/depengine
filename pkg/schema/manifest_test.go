package schema

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

func TestParseManifest_Packages(t *testing.T) {
	p := writeManifest(t, `
[packages]
nvim = { pacman = "neovim", apt = "neovim" }
fd   = { apt = "fd-find" }
`)
	m, err := ParseManifest(p)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(m))
	}

	nvim, ok := m["nvim"]
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

func TestParseManifest_NonExistentFile(t *testing.T) {
	m, err := ParseManifest("/nonexistent/manifest.toml")
	if err != nil {
		t.Fatalf("expected nil error for non-existent file, got %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil map for non-existent file, got %v", m)
	}
}

func TestParseManifest_InvalidTOML(t *testing.T) {
	p := writeManifest(t, `[[[invalid`)
	_, err := ParseManifest(p)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestParseManifest_NoPackages(t *testing.T) {
	p := writeManifest(t, `[other]
key = "value"
`)
	m, err := ParseManifest(p)
	if err != nil {
		t.Fatalf("expected nil error for missing [packages], got %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil map for missing [packages], got %v", m)
	}
}

func TestParseManifest_StripsIntentFields(t *testing.T) {
	p := writeManifest(t, `
[packages]
nvim = { pacman = "neovim", requires = ["zsh"], tags = ["desktop"] }
`)
	m, err := ParseManifest(p)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	nvim := m["nvim"]
	if nvim == nil {
		t.Fatal("expected nvim")
	}
	if nvim.Requires != nil {
		t.Fatal("expected Requires to be stripped")
	}
	if len(nvim.Tags) > 0 {
		t.Fatal("expected Tags to be stripped")
	}
	// Methods should still be present.
	if len(nvim.Methods) == 0 {
		t.Fatal("expected methods to be preserved")
	}
}

func TestParseManifest_PackagesSectionEmpty(t *testing.T) {
	p := writeManifest(t, `
[packages]
`)
	m, err := ParseManifest(p)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m == nil {
		t.Fatal("expected empty map, not nil, for empty [packages]")
	}
	if len(m) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(m))
	}
}
