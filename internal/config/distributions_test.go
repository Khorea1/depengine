package config

// Tests for Distribution parsing, cloning, and layer merge (proposal E, 8.2).
// Semantic value rules (empty list is rejected by shape here; regex validity,
// per-method allowlist) are validated in strict follow-ups; parse covers
// structure, hoisting, and fail-closed shapes.

import (
	"strings"
	"testing"
)

func parseDistributionsSchema(t *testing.T, content string) *Schema {
	t.Helper()
	s, err := ParseProjectSchema(writeSchema(t, content), map[string]string{})
	if err != nil {
		t.Fatalf("ParseProjectSchema: %v", err)
	}
	return s
}

func TestParseDistributions(t *testing.T) {
	s := parseDistributionsSchema(t, `
[tools.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

[[tools.br.github.distributions]]
when = { os = ["darwin"] }

[[tools.br.github.distributions]]
when = { os = ["windows"], arch = ["x86_64"] }
asset = "br-{version}-{os_any}_{arch_any}.zip"
entrypoints = { br = "br.exe" }
strip_components = 1
checksum_url = "https://example.com/SHA256SUMS"
`)
	mc := s.Tools["br"].Methods[0]
	if mc.Kind != "github" {
		t.Fatalf("kind=%q, want github", mc.Kind)
	}
	if len(mc.Distributions) != 2 {
		t.Fatalf("%d distributions, want 2", len(mc.Distributions))
	}
	if _, ok := mc.Config["distributions"]; ok {
		t.Fatal("distributions key leaked into Config (must be hoisted)")
	}
	mac := mc.Distributions[0]
	if mac.When == nil || len(mac.When.OS) != 1 || mac.When.OS[0] != "darwin" {
		t.Fatalf("mac when=%+v", mac.When)
	}
	if mac.HasAsset {
		t.Fatal("mac must inherit base asset (HasAsset=false)")
	}
	win := mc.Distributions[1]
	if !win.HasAsset || win.Asset.Template != "br-{version}-{os_any}_{arch_any}.zip" {
		t.Fatalf("win asset=%+v", win.Asset)
	}
	if win.Asset.Regex != "" {
		t.Fatalf("win regex must be empty, got %q", win.Asset.Regex)
	}
	if win.Entrypoints["br"] != "br.exe" || len(win.Entrypoints) != 1 {
		t.Fatalf("win entrypoints=%v (must replace whole map)", win.Entrypoints)
	}
	if !win.HasStripComponents || win.StripComponents != 1 {
		t.Fatalf("win strip=%d has=%v", win.StripComponents, win.HasStripComponents)
	}
	if !win.HasChecksumURL {
		t.Fatal("win must carry checksum_url override")
	}
	if win.When.Arch[0] != "x86_64" {
		t.Fatalf("win arch=%v", win.When.Arch)
	}
}

func TestParseDistributionRegexAsset(t *testing.T) {
	s := parseDistributionsSchema(t, `
[tools.atlas.github]
repo = "example/atlas"
asset = "atlas-{os_any}-{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.atlas.github.distributions]]
when = { os = ["linux"] }
asset = { regex = 'atlas-{version}-linux-{arch_any}\.tar\.gz' }
`)
	d := s.Tools["atlas"].Methods[0].Distributions[0]
	if !d.HasAsset || d.Asset.Regex != `atlas-{version}-linux-{arch_any}\.tar\.gz` {
		t.Fatalf("regex asset=%+v", d.Asset)
	}
	if d.Asset.Template != "" {
		t.Fatalf("template must be empty, got %q", d.Asset.Template)
	}
}

func TestParseDistributionsNoList(t *testing.T) {
	s := parseDistributionsSchema(t, `
[tools.yq.github]
repo = "mikefarah/yq"
asset = "yq_{os_any}_{arch_any}"
`)
	if s.Tools["yq"].Methods[0].Distributions != nil {
		t.Fatal("method without distributions must keep nil slice")
	}
}

func TestParseDistributionsShapeInvalid(t *testing.T) {
	// Shape é rejeitada na validação raw, antes do parse.
	const base = `[tools.x.github]` + "\n" + `repo = "o/r"` + "\n" + `asset = "base.tar.gz"` + "\n" + `checksum = "sha256:auto"` + "\n" + `scope = "user"` + "\n"
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"element-not-table", "distributions = [\"nope\"]", "expected table"},
		{"not-array", "distributions = \"nope\"", "non-empty array of tables"},
		{"empty-array", "distributions = []", "non-empty array of tables"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProjectSchema(writeSchema(t, base+tc.body), map[string]string{})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseDistributionsValueInvalid(t *testing.T) {
	// Valores inválidos viram mc.Err no parse e emergem via Validate,
	// como todo erro de método individual ("pip = false" etc.).
	const base = `[tools.x.github]` + "\n" + `repo = "o/r"` + "\n" + `asset = "base.tar.gz"` + "\n" + `checksum = "sha256:auto"` + "\n" + `scope = "user"` + "\n"
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing-when", "[[tools.x.github.distributions]]\nasset = \"a.tar.gz\"", "when is required"},
		{"when-not-table", "[[tools.x.github.distributions]]\nwhen = \"linux\"", "when must be a table"},
		{"forbidden-repo", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nrepo = \"o/outro\"", "not specializable"},
		{"forbidden-scope", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nscope = \"system\"", "not specializable"},
		{"asset-int", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = 42", "asset must be"},
		{"asset-no-regex", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { glob = \"*.tar.gz\" }", "exactly { regex"},
		{"asset-extra-keys", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { regex = \"a\", flags = \"i\" }", "exactly { regex"},
		{"entrypoints-not-table", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nentrypoints = \"br\"", "entrypoints must be a table"},
		{"entrypoints-nonstring", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nentrypoints = { br = 42 }", "must be a string"},
		{"strip-not-int", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nstrip_components = \"1\"", "must be an integer"},
		{"checksum-not-string", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum = 42", "must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseProjectSchema(writeSchema(t, base+tc.body), map[string]string{})
			if err != nil {
				t.Fatalf("parse must attach mc.Err, not fail: %v", err)
			}
			_, err = Validate(s, []string{"github"})
			if err == nil {
				t.Fatalf("expected Validate error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestMergeDistributionsAtomic(t *testing.T) {
	base := `
[tools.br.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.br.github.distributions]]
when = { os = ["linux"] }

[[tools.br.github.distributions]]
when = { os = ["darwin"] }
`
	replacement := `
[tools.br.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.br.github.distributions]]
when = { os = ["windows"] }
asset = "only-windows.zip"
`
	lower := parseDistributionsSchema(t, base)
	lowerOnly := MergeLayers(lower, parseDistributionsSchema(t, "schema_version = 1\n[tools]\n"))
	if len(lowerOnly.Tools["br"].Methods[0].Distributions) != 2 {
		t.Fatal("upper layer without distributions must keep lower list")
	}
	upper := parseDistributionsSchema(t, replacement)
	merged := MergeLayers(lower, upper)
	dists := merged.Tools["br"].Methods[0].Distributions
	if len(dists) != 1 || !dists[0].HasAsset || dists[0].Asset.Template != "only-windows.zip" {
		t.Fatalf("upper list must replace wholesale, got %+v", dists)
	}
}

func TestCloneDistributionsIndependent(t *testing.T) {
	s := parseDistributionsSchema(t, `
[tools.br.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

[[tools.br.github.distributions]]
when = { os = ["linux"] }
asset = "linux.tar.gz"
entrypoints = { br = "br-linux" }
`)
	original := s.Tools["br"].Methods[0]
	cloned := cloneMethod(original)
	cloned.Distributions[0].Asset.Template = "mutated.tar.gz"
	cloned.Distributions[0].Entrypoints["br"] = "mutated"
	cloned.Distributions[0].When.OS[0] = "windows"
	got := original.Distributions[0]
	if got.Asset.Template != "linux.tar.gz" || got.Entrypoints["br"] != "br-linux" || got.When.OS[0] != "linux" {
		t.Fatalf("clone shares state with original: %+v", got)
	}
	if cloneMethod(original).Distributions == nil {
		t.Fatal("clone must preserve distributions")
	}
}
