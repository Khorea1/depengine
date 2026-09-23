package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := ParseProjectSchema(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !schema.Tools["helper"].DependencyOnly {
		t.Fatal("dependency_only was not parsed")
	}
	method := schema.Tools["nvim"].Methods[0]
	if len(method.Requires) != 1 || method.Requires[0] != "helper" {
		t.Fatalf("requires = %v", method.Requires)
	}
	if len(method.Sources) != 1 || method.Sources[0].Kind != "apt-ppa" {
		t.Fatalf("sources = %+v", method.Sources)
	}
}

func TestSourceURLOnlyAcceptedWhenAddUsesIt(t *testing.T) {
	for _, tt := range []struct {
		kind    string
		allowed bool
	}{
		{"apt-ppa", false},
		{"dnf-copr", false},
		{"scoop-bucket", true},
		{"brew-tap", true},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema.toml")
			data := "schema_version = 1\n[tools.demo.native]\npkg = \"demo\"\nsources = [{ kind = \"" + tt.kind + "\", name = \"vendor/tools\", url = \"https://example.test/tools\" }]\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := ParseProjectSchema(path, nil)
			if tt.allowed && err != nil {
				t.Fatalf("ParseProjectSchema() = %v, want accepted URL", err)
			}
			if !tt.allowed && (err == nil || !strings.Contains(err.Error(), "sources[0].url: unsupported")) {
				t.Fatalf("ParseProjectSchema() = %v, want source URL rejection", err)
			}
		})
	}
}

func TestParseSourceSecretReferenceInNativeAndLabeledMethods(t *testing.T) {
	for _, method := range []string{"native", "custom"} {
		t.Run(method, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema.toml")
			kind := ""
			if method == "custom" {
				kind = "kind = \"native\"\n"
			}
			data := "schema_version = 1\n[tools.demo." + method + "]\n" + kind + "pkg = \"demo\"\nsources = [{ kind = \"apt-ppa\", name = \"ppa:vendor/stable\", secret_ref = { provider = \"env\", name = \"CORP_TOKEN\" } }]\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			schema, err := ParseProjectSchema(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			ref := schema.Tools["demo"].Methods[0].Sources[0].SecretRef
			if ref == nil || ref.Provider != "env" || ref.Name != "CORP_TOKEN" {
				t.Fatalf("secret reference = %+v", ref)
			}
		})
	}
}

func TestSourceSecretReferenceRejectsInvalidFields(t *testing.T) {
	for _, tt := range []struct {
		name string
		ref  string
		want string
	}{
		{"missing provider", `{ name = "TOKEN" }`, "secret_ref.provider"},
		{"empty name", `{ provider = "env", name = "" }`, "secret_ref.name"},
		{"whitespace", `{ provider = " env", name = "TOKEN" }`, "secret_ref.provider"},
		{"unknown field", `{ provider = "env", name = "TOKEN", value = "literal" }`, "secret_ref.value"},
		{"wrong type", `"TOKEN"`, "secret_ref: expected table"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema.toml")
			data := "schema_version = 1\n[tools.demo.native]\npkg = \"demo\"\nsources = [{ kind = \"apt-ppa\", name = \"ppa:vendor/stable\", secret_ref = " + tt.ref + " }]\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ParseProjectSchema(path, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
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
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProjectSchema(path, nil); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactIntegrityOptionsRejectIgnoredFields(t *testing.T) {
	literal := "sha256:" + strings.Repeat("0", 64)
	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{name: "empty checksum", fields: `checksum = ""`, want: "checksum: must not be empty"},
		{name: "checksum URL without auto checksum", fields: `checksum_url = "https://example.test/SHA256SUMS"`, want: `checksum_url: requires checksum = "<algorithm>:auto"`},
		{name: "checksum URL with literal checksum", fields: `checksum = "` + literal + `"
checksum_url = "https://example.test/SHA256SUMS"`, want: `checksum_url: requires checksum = "<algorithm>:auto"`},
		{name: "checksum format with literal checksum", fields: `checksum = "` + literal + `"
checksum_file_format = "raw"`, want: `checksum_file_format: requires checksum = "<algorithm>:auto"`},
		{name: "signature URL with literal checksum", fields: `checksum = "` + literal + `"
signature_url = "https://example.test/SHA256SUMS.sig"`, want: `signature_url: requires checksum = "<algorithm>:auto"`},
		{name: "signing key without signature", fields: `checksum = "sha256:auto"
signing_key = "ABCD1234"`, want: "signing_key: requires signature_url"},
		{name: "empty checksum URL", fields: `checksum = "sha256:auto"
checksum_url = ""`, want: "checksum_url: must not be empty"},
		{name: "empty signature URL", fields: `checksum = "sha256:auto"
signature_url = ""`, want: "signature_url: must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema.toml")
			data := "schema_version = 1\n[tools.demo.http]\nurl = \"https://example.test/demo.tar.gz\"\n" + tt.fields + "\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ParseProjectSchema(path, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseProjectSchema() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestArtifactIntegrityOptionsAcceptAutoChecksumSidecars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	data := `schema_version = 1
[tools.demo.http]
url = "https://example.test/demo.tar.gz"
checksum = "sha256:auto"
checksum_url = "https://example.test/SHA256SUMS"
checksum_file_format = "sha256sum"
signature_url = "https://example.test/SHA256SUMS.sig"
signing_key = "ABCD1234"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProjectSchema(path, nil); err != nil {
		t.Fatalf("ParseProjectSchema() = %v, want accepted auto-checksum sidecars", err)
	}
}
