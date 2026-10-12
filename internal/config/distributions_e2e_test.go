package config

// End-to-end offline da proposta E (8.6): fixtures reais br/omp/wt em E,
// parse → merge → especialização → matching contra catálogos simulados, mais
// ferramenta sintética de 12 métodos com distribuições cruzadas, conflitos e
// assets malformados. Sem rede: MatchAssetNames sobre listas em memória.

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/platform"
)

type e2eHost struct {
	name  string
	facts *platform.Facts
	subst map[string]string
	tag   string
}

func e2eHosts() []e2eHost {
	base := map[string]string{"id": "arch", "distro_family": "arch", "kernel": "6.7", "libc": "glibc", "init_system": "systemd"}
	with := func(m map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return []e2eHost{
		{"linux-glibc", &platform.Facts{OS: "linux", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix"}, with(map[string]string{"arch": "x86_64", "os": "linux"}), "v1.2.3"},
		{"linux-musl", &platform.Facts{OS: "linux", TargetArch: "x86_64", Libc: "musl 1.2.4", TargetFamily: "unix"}, with(map[string]string{"arch": "x86_64", "os": "linux"}), "v1.2.3"},
		{"mac-arm64", &platform.Facts{OS: "darwin", TargetArch: "arm64", TargetFamily: "unix"}, with(map[string]string{"arch": "arm64", "os": "darwin"}), "v1.2.3"},
		{"windows", &platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"}, with(map[string]string{"arch": "x86_64", "os": "windows"}), "v1.2.3"},
		{"android", &platform.Facts{OS: "linux", TargetArch: "aarch64", Libc: "bionic", IsAndroid: true, TargetFamily: "unix"}, with(map[string]string{"arch": "aarch64", "os": "linux"}), "v1.2.3"},
	}
}

func matchEffective(t *testing.T, mc *MethodCandidate, facts *platform.Facts, tag string, catalog []string) string {
	t.Helper()
	effective, err := SpecializeDistribution(mc, facts)
	if err != nil {
		t.Fatalf("specialize: %v", err)
	}
	if effective == nil {
		return ""
	}
	raw, _ := effective.Config["asset"].(string)
	if raw == "" {
		if form, ok := effective.Config["asset"].(map[string]any); ok {
			regex, _ := form["regex"].(string)
			re, err := ghrelease.CompileExplicitRegex(regex, tag, facts.TargetArch, facts.OS)
			if err != nil {
				t.Fatalf("compile regex: %v", err)
			}
			matched := ghrelease.MatchAssetNames(re, catalog)
			if len(matched) != 1 {
				t.Fatalf("regex matched %v, want exactly one", matched)
			}
			return matched[0]
		}
		t.Fatal("effective candidate has no asset")
	}
	re, err := ghrelease.CompileLiteralPattern(raw, tag, facts.TargetArch, facts.OS)
	if err != nil {
		t.Fatalf("compile pattern: %v", err)
	}
	matched := ghrelease.MatchAssetNames(re, catalog)
	if len(matched) != 1 {
		t.Fatalf("pattern %q matched %v, want exactly one", raw, matched)
	}
	return matched[0]
}

func TestE2EDistributionsRealTools(t *testing.T) {
	brCatalog := []string{"br-1.2.3-darwin_arm64.tar.gz", "br-1.2.3-linux_x86_64.tar.gz", "br-1.2.3-linux_musl_x86_64.tar.gz", "br-1.2.3-windows_x86_64.zip", "SHA256SUMS.txt"}
	ompCatalog := []string{"omp-linux-x86_64", "omp-linux-musl-x86_64", "omp-darwin-arm64", "SHA256SUMS.txt"}
	wtCatalog := []string{"worktrunk-x86_64-unknown-linux-musl.tar.xz", "worktrunk-arm64-apple-darwin.tar.xz", "SHA256SUMS.txt"}
	want := map[string]map[string]string{
		"br":  {"linux-glibc": "br-1.2.3-linux_x86_64.tar.gz", "linux-musl": "br-1.2.3-linux_musl_x86_64.tar.gz", "mac-arm64": "br-1.2.3-darwin_arm64.tar.gz", "windows": "br-1.2.3-windows_x86_64.zip", "android": ""},
		"omp": {"linux-glibc": "omp-linux-x86_64", "linux-musl": "omp-linux-musl-x86_64", "mac-arm64": "omp-darwin-arm64", "windows": "", "android": ""},
		"wt":  {"linux-glibc": "worktrunk-x86_64-unknown-linux-musl.tar.xz", "linux-musl": "worktrunk-x86_64-unknown-linux-musl.tar.xz", "mac-arm64": "worktrunk-arm64-apple-darwin.tar.xz", "windows": "", "android": ""},
	}
	catalogs := map[string][]string{"br": brCatalog, "omp": ompCatalog, "wt": wtCatalog}
	files := map[string]string{"br": "e_br.toml", "omp": "e_omp.toml", "wt": "e_wt.toml"}
	for tool, file := range files {
		for _, host := range e2eHosts() {
			s, err := ParseProjectSchema(testdataFile(t, file), host.subst)
			if err != nil {
				t.Fatalf("%s/%s: parse: %v", tool, host.name, err)
			}
			if _, err := Validate(s, methodkind.KnownKinds()); err != nil {
				t.Fatalf("%s/%s: validate: %v", tool, host.name, err)
			}
			var github *MethodCandidate
			for _, mc := range s.Tools[tool].Methods {
				if mc.Kind == "github" {
					github = mc
				}
			}
			if github == nil {
				t.Fatalf("%s/%s: no github candidate", tool, host.name)
			}
			for i := 0; i < 3; i++ {
				if got := matchEffective(t, github, host.facts, host.tag, catalogs[tool]); got != want[tool][host.name] {
					t.Fatalf("%s/%s: got %q, want %q", tool, host.name, got, want[tool][host.name])
				}
			}
		}
	}
}

func TestE2ESyntheticTwelveMethods(t *testing.T) {
	s, err := ParseProjectSchema(writeSchema(t, `
[tools.atlas]
method_only = ["native", "aur", "cargo", "go", "npm", "pipx", "uv", "conda", "nix", "http", "github"]

[tools.atlas.native]
pkg = "atlas"

[tools.atlas.aur]
pkg = "atlas-bin"
when = { distro_family = ["arch"] }

[tools.atlas.cargo]
pkg = "atlas"

[tools.atlas.go]
pkg = "example.org/atlas"

[tools.atlas.npm]
pkg = "@example/atlas"

[tools.atlas.pipx]
pkg = "atlas-cli"

[tools.atlas.uv]
pkg = "atlas-cli"

[tools.atlas.conda]
pkg = "atlas"
channels = ["conda-forge"]

[tools.atlas.nix]
pkg = "atlas"

[tools.atlas.http]
url = "https://example.com/atlas-{os}-{arch}.tar.gz"
checksum = "sha256:auto"
when = { os = ["freebsd"] }

[tools.atlas.github]
repo = "example/atlas"
asset = "atlas-linux-amd64.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.atlas.github.distributions]]
when = { os = ["linux"], arch = ["x86_64"] }

[[tools.atlas.github.distributions]]
when = { os = ["darwin"] }
asset = "atlas-darwin-arm64.tar.gz"

[tools.atlas.gh_alt]
kind = "github"
repo = "example/atlas-alt"
asset = "alt-windows-amd64.zip"
checksum = "sha256:auto"
scope = "user"

[[tools.atlas.gh_alt.distributions]]
when = { os = ["windows"] }
`), map[string]string{})
	if err != nil {
		t.Fatalf("synthetic tool must parse: %v", err)
	}
	methods := s.Tools["atlas"].Methods
	if len(methods) != 12 {
		t.Fatalf("%d methods, want 12", len(methods))
	}
	if _, err := Validate(s, methodkind.KnownKinds()); err != nil {
		t.Fatalf("synthetic tool must validate: %v", err)
	}
	linux := &platform.Facts{OS: "linux", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix", DistroID: "debian"}
	var github *MethodCandidate
	for _, mc := range methods {
		if mc.Kind == "github" {
			github = mc
		}
	}
	effective, err := SpecializeDistribution(github, linux)
	if err != nil || effective == nil || effective.Config["asset"] != "atlas-linux-amd64.tar.gz" {
		t.Fatalf("linux selects base asset, got %v, %v", effective, err)
	}
	mac, err := SpecializeDistribution(github, &platform.Facts{OS: "darwin", TargetArch: "arm64", TargetFamily: "unix"})
	if err != nil || mac == nil || mac.Config["asset"] != "atlas-darwin-arm64.tar.gz" {
		t.Fatalf("darwin selects override, got %v, %v", mac, err)
	}
	var alt *MethodCandidate
	for _, mc := range methods {
		if mc.Label == "gh_alt" {
			alt = mc
		}
	}
	if alt == nil || alt.Kind != "github" {
		t.Fatal("second github method must keep kind with custom label")
	}
	winAlt, err := SpecializeDistribution(alt, &platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"})
	if err != nil || winAlt == nil || winAlt.Config["asset"] != "alt-windows-amd64.zip" {
		t.Fatalf("windows selects alt distribution, got %v, %v", winAlt, err)
	}
	// Condições conflitantes: erro, não primeira-da-lista.
	conflict, err := ParseProjectSchema(writeSchema(t, `
[tools.clash.github]
repo = "example/clash"
asset = "clash.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.clash.github.distributions]]
when = { os = ["linux"] }

[[tools.clash.github.distributions]]
when = { target_family = ["unix"] }
`), map[string]string{})
	if err != nil {
		t.Fatalf("conflict fixture must parse: %v", err)
	}
	if _, err := SpecializeDistribution(conflict.Tools["clash"].Methods[0], linux); err == nil {
		t.Fatal("overlapping distributions must abort, not pick first")
	}
	// Assets malformados: regex inválida e lista vazia.
	for name, body := range map[string]string{
		"bad-regex":  "[[tools.bad.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { regex = \"([\" }",
		"empty-list": "distributions = []",
	} {
		doc := "[tools.bad.github]\nrepo = \"o/r\"\nasset = \"b.tar.gz\"\nchecksum = \"sha256:auto\"\nscope = \"user\"\n" + body + "\n"
		if _, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{}); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func testdataFile(t *testing.T, name string) string {
	t.Helper()
	return "testdata/" + name
}

func TestE2EExecutablesInArchives(t *testing.T) {
	// .exe portátil dentro de ZIP e binário bruto resolvem pelo mesmo caminho
	// (independente de dep-laz, que trata do .exe como artefato direto).
	catalog := []string{"cli-windows-amd64.zip", "cli-linux-amd64", "setup.exe"}
	zipRe, err := ghrelease.CompileLiteralPattern("cli-{os_any}-{arch_any}.zip", "v2.0.0", "x86_64", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if got := ghrelease.MatchAssetNames(zipRe, catalog); len(got) != 1 || got[0] != "cli-windows-amd64.zip" {
		t.Fatalf("zip matched=%v", got)
	}
	rawRe, err := ghrelease.CompileLiteralPattern("cli-{os_any}-{arch_any}", "v2.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got := ghrelease.MatchAssetNames(rawRe, catalog); len(got) != 1 || got[0] != "cli-linux-amd64" {
		t.Fatalf("raw matched=%v", got)
	}
	// Formato proibido como artefato direto continua rejeitado pelo contrato.
	contract, ok := methodkind.Lookup("github")
	if !ok || contract.Artifact == nil {
		t.Fatal("github contract must exist")
	}
	if err := contract.Artifact.ValidateArtifact("cli-windows-amd64.exe"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("direct .exe must stay forbidden, got %v", err)
	}
}
