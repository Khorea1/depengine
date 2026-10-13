package config

// Tests for Distribution parsing, cloning, and layer merge (proposal E, 8.2).
// Semantic value rules (empty list is rejected by shape here; regex validity,
// per-method allowlist) are validated in strict follow-ups; parse covers
// structure, hoisting, and fail-closed shapes.

import (
	"fmt"
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

func TestParseDistributionsValuesDirect(t *testing.T) {
	// Backstop do parse (validação raw roda primeiro nos documentos; aqui
	// exercita a função diretamente).
	when := map[string]any{"os": []any{"linux"}}
	valid := map[string]any{"when": when, "asset": "a.tar.gz"}
	if _, err := parseDistributions([]any{valid}); err != nil {
		t.Fatalf("valid: %v", err)
	}
	cases := []struct {
		name    string
		doc     map[string]any
		wantErr string
	}{
		{"missing-when", map[string]any{"asset": "a.tar.gz"}, "when is required"},
		{"when-not-table", map[string]any{"when": "linux"}, "when must be a table"},
		{"forbidden", map[string]any{"when": when, "repo": "o/r"}, "not specializable"},
		{"asset-int", map[string]any{"when": when, "asset": 42}, "asset must be"},
		{"asset-empty-table", map[string]any{"when": when, "asset": map[string]any{}}, "exactly { regex"},
		{"entrypoints-value", map[string]any{"when": when, "entrypoints": map[string]any{"b": 1}}, "must be a string"},
		{"strip-string", map[string]any{"when": when, "strip_components": "1"}, "must be an integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDistributions([]any{tc.doc}); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
	if _, err := parseDistributions([]any{"nope"}); err == nil {
		t.Fatal("expected table error")
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
	// Raw strict roda primeiro e reporta com as mesmas garantias; o backstop
	// de parse (mc.Err) está coberto em TestParseDistributionsValuesDirect.
	const base = `[tools.x.github]` + "\n" + `repo = "o/r"` + "\n" + `asset = "base.tar.gz"` + "\n" + `checksum = "sha256:auto"` + "\n" + `scope = "user"` + "\n"
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing-when", "[[tools.x.github.distributions]]\nasset = \"a.tar.gz\"", "when is required"},
		{"when-not-table", "[[tools.x.github.distributions]]\nwhen = \"linux\"", ".when: must be a table"},
		{"forbidden-repo", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nrepo = \"o/outro\"", "not specializable"},
		{"forbidden-scope", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nscope = \"system\"", "not specializable"},
		{"asset-int", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = 42", "must be a string or"},
		{"asset-no-regex", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { glob = \"*.tar.gz\" }", "exactly { regex"},
		{"asset-extra-keys", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { regex = \"a\", flags = \"i\" }", "exactly { regex"},
		{"entrypoints-not-table", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nentrypoints = \"br\"", ".entrypoints: must be a table"},
		{"entrypoints-nonstring", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nentrypoints = { br = 42 }", "must be a string"},
		{"strip-not-int", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nstrip_components = \"1\"", "must be an integer"},
		{"checksum-not-string", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum = 42", "must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProjectSchema(writeSchema(t, base+tc.body), map[string]string{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestStrictDistributionsSemantic(t *testing.T) {
	const base = `[tools.x.github]` + "\n" + `repo = "o/r"` + "\n" + `asset = "base.tar.gz"` + "\n" + `checksum = "sha256:auto"` + "\n" + `scope = "user"` + "\n"
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"invalid-regex", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { regex = \"([\" }", "invalid regex"},
		{"empty-regex", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = { regex = \"\" }", "must not be empty"},
		{"empty-template", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nasset = \"\"", "must not be empty"},
		{"negative-strip", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nstrip_components = -1", "must be non-negative"},
		{"empty-binary", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nbinary = \"\"", "must be a string"},
		{"url-without-auto", "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum = \"sha256:abc\"\nchecksum_url = \"https://example.com/SHA\"", "requires checksum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProjectSchema(writeSchema(t, base+tc.body), map[string]string{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
	// Método cujo contrato não lista distribuições: rejeitado.
	t.Run("unsupported-method", func(t *testing.T) {
		doc := "schema_version = 1\n[tools.x.cargo]\npkg = \"x\"\n[[tools.x.cargo.distributions]]\nwhen = { os = [\"linux\"] }\n"
		_, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{})
		if err == nil || !strings.Contains(err.Error(), "does not support distributions") {
			t.Fatalf("err=%v, want unsupported-distributions", err)
		}
	})
	// Custom adapters may be registered for Validate without having a
	// methodkind distribution contract; that registration must not grant
	// distribution support.
	t.Run("uncontracted-custom-adapter", func(t *testing.T) {
		valid, err := ParseProjectSchema(writeSchema(t, "schema_version = 1\n[tools.x.custom-label]\nkind = \"custom-adapter\"\n"), map[string]string{})
		if err != nil {
			t.Fatalf("ParseProjectSchema without distributions: %v", err)
		}
		if _, err := Validate(valid, []string{"custom-adapter"}); err != nil {
			t.Fatalf("Validate registered custom adapter: %v", err)
		}

		cases := []struct {
			name  string
			parse func(string) (*Schema, error)
			doc   string
		}{
			{
				name:  "project-schema",
				parse: func(path string) (*Schema, error) { return ParseProjectSchema(path, map[string]string{}) },
				doc:   "schema_version = 1\n[tools.x.custom-label]\nkind = \"custom-adapter\"\n[[tools.x.custom-label.distributions]]\nwhen = { os = [\"linux\"] }\n",
			},
			{
				name:  "manifest",
				parse: func(path string) (*Schema, error) { return ParseManifest(path, nil) },
				doc:   "schema_version = 1\n[packages.x.custom-label]\nkind = \"custom-adapter\"\n[[packages.x.custom-label.distributions]]\nwhen = { os = [\"linux\"] }\n",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				path := writeSchema(t, tc.doc)
				_, err := tc.parse(path)
				if err == nil || !strings.Contains(err.Error(), "no distribution contract") {
					t.Fatalf("err=%v, want missing distribution contract", err)
				}
			})
		}
	})
	// checksum_url com base :auto continua válido (herança do checksum).
	t.Run("url-with-inherited-auto", func(t *testing.T) {
		doc := base + "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum_url = \"https://example.com/SHA\"\n"
		if _, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{}); err != nil {
			t.Fatalf("inherited :auto must accept checksum_url: %v", err)
		}
	})
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

func TestStrictPrincipalAssetSelector(t *testing.T) {
	const header = "schema_version = 1\n"
	// The principal github asset accepts the same selector shapes as
	// distribution overrides: a literal template or { regex = "..." }.
	t.Run("github-principal-regex-valid", func(t *testing.T) {
		doc := header + "[tools.x.github]\nrepo = \"o/r\"\nasset = { regex = 'tool-(win|windows)\\.zip' }\nchecksum = \"sha256:auto\"\nscope = \"user\"\n"
		s, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{})
		if err != nil {
			t.Fatalf("principal regex asset must validate: %v", err)
		}
		selector, ok := s.Tools["x"].Methods[0].Config["asset"].(map[string]any)
		if !ok || selector["regex"] != `tool-(win|windows)\.zip` {
			t.Fatalf("principal regex asset must survive parsing as a map, got %#v", s.Tools["x"].Methods[0].Config["asset"])
		}
	})
	t.Run("github-principal-literal-valid", func(t *testing.T) {
		doc := header + "[tools.x.github]\nrepo = \"o/r\"\nasset = \"tool.tar.gz\"\nchecksum = \"sha256:auto\"\nscope = \"user\"\n"
		if _, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{}); err != nil {
			t.Fatalf("principal literal asset must validate: %v", err)
		}
	})
	for _, tc := range []struct {
		name    string
		asset   string
		wantErr string
	}{
		{"github-principal-regex-invalid", `asset = { regex = "([" }`, "invalid regex"},
		{"github-principal-regex-empty", `asset = { regex = "" }`, "must not be empty"},
		{"github-principal-asset-wrong-shape", `asset = { glob = "tool-*" }`, "exactly { regex = "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := header + "[tools.x.github]\nrepo = \"o/r\"\n" + tc.asset + "\nchecksum = \"sha256:auto\"\nscope = \"user\"\n"
			_, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
	// Other method kinds keep the string-only asset: the map form that the
	// github resolver accepts must not silently widen the http contract.
	t.Run("http-principal-regex-rejected", func(t *testing.T) {
		doc := header + "[tools.x.http]\nrepo = \"o/r\"\nasset = { regex = 'tool\\.zip' }\nchecksum = \"sha256:auto\"\nscope = \"user\"\n"
		_, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{})
		if err == nil || !strings.Contains(err.Error(), "expected string") {
			t.Fatalf("err=%v, want string-only rejection", err)
		}
	})
}

// Distribution binary overrides reuse the base-field basename rule
// (separators, reserved names, drive prefixes): the same executable name
// that is rejected on the base must be rejected per distribution.
func TestStrictDistributionBinaryBasename(t *testing.T) {
	const base = `[tools.x.github]` + "\n" + `repo = "o/r"` + "\n" + `asset = "base.tar.gz"` + "\n" + `checksum = "sha256:auto"` + "\n" + `scope = "user"` + "\n"
	for _, name := range []string{"bin/tool", "../escape", "..", ".", `C:\tool`, `dir\tool`} {
		t.Run(name, func(t *testing.T) {
			doc := base + "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\n" +
				fmt.Sprintf("binary = %q\n", name)
			_, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{})
			if err == nil || !strings.Contains(err.Error(), "binary") {
				t.Fatalf("binary %q: err=%v, want basename rejection", name, err)
			}
		})
	}
	for _, name := range []string{"br", "tool.exe"} {
		t.Run("valid-"+name, func(t *testing.T) {
			doc := base + "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\n" +
				fmt.Sprintf("binary = %q\n", name)
			if _, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{}); err != nil {
				t.Fatalf("binary %q must validate: %v", name, err)
			}
		})
	}
}

// Distribution integrity is validated against EFFECTIVE values (base
// inherited + override), not just declared fields: pinning checksum away
// from :auto while inheriting a sidecar, or adding a sidecar the effective
// checksum cannot resolve, is rejected on every host before execution.
func TestStrictDistributionEffectiveChecksum(t *testing.T) {
	const base = `[tools.x.github]` + "\n" + `repo = "o/r"` + "\n" + `asset = "base.tar.gz"` + "\n" + `scope = "user"` + "\n"
	const fixed = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := []struct {
		name    string
		base    string
		body    string
		wantErr string
	}{
		{
			// Distribution pins a fixed checksum while inheriting the
			// base sidecar: the effective config cannot resolve it.
			name:    "pinned-checksum-inherits-sidecar",
			base:    base + "checksum = \"sha256:auto\"\nchecksum_url = \"https://example.com/SHA256SUMS\"\n",
			body:    "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum = \"" + fixed + "\"\n",
			wantErr: "distributions[0].checksum_url",
		},
		{
			// Distribution adds a signature sidecar the base checksum
			// cannot resolve, with no signing key to pair it.
			name:    "signature-url-without-key",
			base:    base + "checksum = \"sha256:auto\"\n",
			body:    "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nsignature_url = \"https://example.com/tool.sig\"\n",
			wantErr: "requires signing_key",
		},
		{
			// Base carries a full signature pair; the distribution pins
			// a fixed checksum, orphaning both inherited sidecars.
			name:    "pinned-checksum-inherits-signature-pair",
			base:    base + "checksum = \"sha256:auto\"\nsignature_url = \"https://example.com/tool.sig\"\nsigning_key = \"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"\n",
			body:    "[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum = \"" + fixed + "\"\n",
			wantErr: "requires checksum",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProjectSchema(writeSchema(t, tc.base+tc.body), map[string]string{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
	// A fixed checksum with no sidecars anywhere stays valid: the rule
	// only fires when a sidecar is effectively present.
	t.Run("pinned-checksum-without-sidecars-valid", func(t *testing.T) {
		doc := base + "checksum = \"sha256:auto\"\n" +
			"[[tools.x.github.distributions]]\nwhen = { os = [\"linux\"] }\nchecksum = \"" + fixed + "\"\n"
		if _, err := ParseProjectSchema(writeSchema(t, doc), map[string]string{}); err != nil {
			t.Fatalf("sidecar-free pin must validate: %v", err)
		}
	})
}

// Merged schemas (schema+manifest layering) are validated over their
// EFFECTIVE combinations on every distribution, including ones that match
// no current host. Each layer below parses valid on its own; only the merge
// introduces the fault.
func TestValidateEffectiveIntegrityMerge(t *testing.T) {
	const fixed = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	lower, err := ParseProjectSchema(writeSchema(t, `
[tools.x.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "sha256:auto"
checksum_url = "https://example.com/SHA256SUMS"
scope = "user"

[[tools.x.github.distributions]]
when = { os = ["linux"] }

[[tools.x.github.distributions]]
when = { os = ["windows"] }
`), map[string]string{})
	if err != nil {
		t.Fatalf("lower layer must parse valid: %v", err)
	}
	upper, err := ParseProjectSchema(writeSchema(t, `
[tools.x.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "`+fixed+`"
scope = "user"
`), map[string]string{})
	if err != nil {
		t.Fatalf("upper layer must parse valid: %v", err)
	}
	merged := MergeLayers(lower, upper)
	mc := merged.Tools["x"].Methods[0]
	if mc.Config["checksum"] != fixed {
		t.Fatalf("probe setup wrong: merged checksum=%v", mc.Config["checksum"])
	}
	// The validator is host-independent: the windows-only distribution is
	// flagged even though no check below runs on a windows host.
	err = ValidateEffectiveIntegrity(merged)
	if err == nil {
		t.Fatal("merged fixed checksum + inherited sidecar must be rejected")
	}
	for _, want := range []string{"requires checksum", "distributions[0]", "distributions[1]"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err.Error(), want)
		}
	}
	// The matched host is rejected before execution too.
	if _, err := SpecializeDistribution(mc, linuxGlibc()); err == nil || !strings.Contains(err.Error(), "requires checksum") {
		t.Fatalf("specialize must reject the invalid effective config, got %v", err)
	}
	// A merge that only overrides an unrelated field stays valid, and the
	// matched distribution still specializes.
	upperOK, err := ParseProjectSchema(writeSchema(t, `
[tools.x.github]
repo = "o/r"
asset = "patched.tar.gz"
checksum = "sha256:auto"
checksum_url = "https://example.com/SHA256SUMS"
scope = "user"
`), map[string]string{})
	if err != nil {
		t.Fatalf("upper layer must parse valid: %v", err)
	}
	mergedOK := MergeLayers(lower, upperOK)
	if err := ValidateEffectiveIntegrity(mergedOK); err != nil {
		t.Fatalf("valid merge must pass: %v", err)
	}
	got, err := SpecializeDistribution(mergedOK.Tools["x"].Methods[0], linuxGlibc())
	if err != nil || got == nil || got.Config["asset"] != "patched.tar.gz" {
		t.Fatalf("valid merge must specialize, got %v, %v", got, err)
	}
}

// Real layering priority (manifest lower, schema upper): the manifest adds
// sidecar material and distributions that are valid under its own base, but
// the schema pins a fixed checksum, so every effective combination breaks.
func TestValidateEffectiveIntegrityManifestMerge(t *testing.T) {
	const fixed = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	schema, err := ParseProjectSchema(writeSchema(t, `
[tools.x.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "`+fixed+`"
scope = "user"
`), map[string]string{})
	if err != nil {
		t.Fatalf("schema must parse valid: %v", err)
	}
	manifest, err := ParseManifest(writeManifest(t, `
[packages.x.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "sha256:auto"
checksum_url = "https://example.com/SHA256SUMS"
scope = "user"

[[packages.x.github.distributions]]
when = { os = ["linux"] }
`), map[string]string{})
	if err != nil {
		t.Fatalf("manifest must parse valid: %v", err)
	}
	merged := MergeLayers(manifest, schema)
	if err := ValidateEffectiveIntegrity(merged); err == nil || !strings.Contains(err.Error(), "requires checksum") {
		t.Fatalf("merge-introduced sidecar under fixed checksum must be rejected, got %v", err)
	}
}

// Specialization fails closed when the materialized candidate's effective
// integrity is incoherent (e.g. a merged schema that was never re-validated
// reaches execution): the fault is reported instead of installing unverified.
func TestSpecializeRejectsInvalidEffectiveIntegrity(t *testing.T) {
	mc := &MethodCandidate{
		Kind: "github",
		Config: map[string]any{
			"repo":         "o/r",
			"asset":        "a.tar.gz",
			"checksum":     "sha256:fixed",
			"checksum_url": "https://example.com/SHA256SUMS",
		},
		Distributions: []Distribution{{When: &Condition{OS: []string{"linux"}}}},
	}
	if _, err := SpecializeDistribution(mc, linuxGlibc()); err == nil || !strings.Contains(err.Error(), "requires checksum") {
		t.Fatalf("specialize must reject incoherent effective integrity, got %v", err)
	}
}
