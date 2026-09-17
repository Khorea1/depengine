package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCandidateScopedCapabilities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	data := `schema_version = 1
[tools]
helper = { dependency_only = true, apt = "software-properties-common" }
[tools.nvim.ppa]
kind = "native"
pkg = "neovim"
requires = ["helper"]
sources = [{ kind = "apt-ppa", name = "ppa:neovim-ppa/stable" }]
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := ParseProjectSchema(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !schema.Tools["helper"].DependencyOnly {
		t.Fatal("dependency_only was not parsed")
	}
	method := schema.Tools["nvim"].Methods[1]
	if len(method.Requires) != 1 || method.Requires[0] != "helper" {
		t.Fatalf("requires = %v", method.Requires)
	}
	if len(method.Sources) != 1 || method.Sources[0].Kind != "apt-ppa" {
		t.Fatalf("sources = %+v", method.Sources)
	}
}

func TestParseArtifactAndTypedOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	data := `schema_version = 1
[tools]
nvim = { method_only = ["github"], github = { repo = "neovim/neovim", asset = "nvim-{os_any}-{arch_any}.tar.gz", strip_components = 1, extract_to = "~/.local/opt/nvim", entrypoints = { nvim = "bin/nvim" }, link_dir = "~/.local/bin" } }
snapvim = { snap = { pkg = "nvim", confinement = "classic", channel = "beta" } }
chocovim = { choco = { pkg = "neovim", prerelease = true } }
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProjectSchema(path, nil); err != nil {
		t.Fatal(err)
	}
}
