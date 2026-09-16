package config

import "testing"

// aarch64Map is fixedMap() with arch/os swapped for values that DO have a
// builtin alias (see defaultArchMap/defaultOSMap in archdefaults.go),
// so these tests can observe the mapping actually firing.
func aarch64Map() map[string]string {
	m := fixedMap()
	m["arch"] = "aarch64"
	m["os"] = "darwin"
	return m
}

func TestArchMap_BuiltinOnly(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
fastfetch = { http = { url = "https://x.com/{os}/{arch}/fastfetch-{arch}.tar.gz" } }
`)
	s, err := ParseProjectSchema(p, aarch64Map())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	mc := s.Tools["fastfetch"].Methods[1] // native[0] + http[1]
	if mc.Kind != "http" {
		t.Fatalf("expected http, got %q", mc.Kind)
	}
	// No arch_map/os_map anywhere in the schema: falls all the way through
	// to the engine builtin (aarch64->arm64, darwin->macos).
	want := "https://x.com/macos/arm64/fastfetch-arm64.tar.gz"
	if got := mc.Config["url"]; got != want {
		t.Fatalf("url:\n got: %v\nwant: %v", got, want)
	}
}

func TestArchMap_DefaultsOverridesBuiltin(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"
arch_map = { aarch64 = "arm64_v2" }

[tools]
fastfetch = { http = { url = "https://x.com/{arch}/fastfetch.tar.gz" } }
`)
	s, err := ParseProjectSchema(p, aarch64Map())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	mc := s.Tools["fastfetch"].Methods[1]
	want := "https://x.com/arm64_v2/fastfetch.tar.gz"
	if got := mc.Config["url"]; got != want {
		t.Fatalf("url:\n got: %v\nwant: %v", got, want)
	}
	if s.Defaults.ArchMap["aarch64"] != "arm64_v2" {
		t.Fatalf("Defaults.ArchMap not parsed: %#v", s.Defaults.ArchMap)
	}
}

func TestArchMap_MethodOverridesDefaultsAndBuiltin(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"
arch_map = { aarch64 = "arm64_v2" }

[tools]
# Per-tool arch_map wins over both [defaults].arch_map and the builtin.
fastfetch = { http = { url = "https://x.com/{arch}/fastfetch.tar.gz", arch_map = { aarch64 = "aarch64_be" } } }
# A second http tool with no override still gets the [defaults] mapping —
# proves the method-level override is scoped to just the one tool.
other = { http = { url = "https://x.com/{arch}/other.tar.gz" } }
`)
	s, err := ParseProjectSchema(p, aarch64Map())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	got := s.Tools["fastfetch"].Methods[1].Config["url"]
	want := "https://x.com/aarch64_be/fastfetch.tar.gz"
	if got != want {
		t.Fatalf("fastfetch url:\n got: %v\nwant: %v", got, want)
	}
	got2 := s.Tools["other"].Methods[1].Config["url"]
	want2 := "https://x.com/arm64_v2/other.tar.gz"
	if got2 != want2 {
		t.Fatalf("other url:\n got: %v\nwant: %v", got2, want2)
	}
	// arch_map must never leak into the adapter-facing Config.
	if _, ok := s.Tools["fastfetch"].Methods[1].Config["arch_map"]; ok {
		t.Fatalf("arch_map leaked into Config: %#v", s.Tools["fastfetch"].Methods[1].Config)
	}
}

func TestArchMap_KeyNormalizedValuePreserved(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
# Key written uppercase in the schema; value has meaningful mixed case that
# must survive verbatim (upstream URLs are frequently case-sensitive).
fastfetch = { http = { url = "https://x.com/{os}/fastfetch.tar.gz", os_map = { DARWIN = "macOS" } } }
`)
	s, err := ParseProjectSchema(p, aarch64Map())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	mc := s.Tools["fastfetch"].Methods[1]
	want := "https://x.com/macOS/fastfetch.tar.gz"
	if got := mc.Config["url"]; got != want {
		t.Fatalf("value case not preserved:\n got: %v\nwant: %v", got, want)
	}
	if _, ok := mc.OSMap["darwin"]; !ok {
		t.Fatalf("key not normalized to lowercase: %#v", mc.OSMap)
	}
}

func TestArchMap_UnmappedValueFallsBackToRaw(t *testing.T) {
	// fixedMap's arch ("x86_64") has no builtin/defaults/method entry
	// anywhere: must resolve to itself, same as before this feature existed.
	p := writeSchema(t, `
[defaults]
manager = "native"
arch_map = { aarch64 = "arm64" }

[tools]
fastfetch = { http = { url = "https://x.com/{arch}/fastfetch.tar.gz" } }
`)
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	want := "https://x.com/x86_64/fastfetch.tar.gz"
	if got := s.Tools["fastfetch"].Methods[1].Config["url"]; got != want {
		t.Fatalf("url:\n got: %v\nwant: %v", got, want)
	}
}

func TestArchMap_GithubMethodUnaffected(t *testing.T) {
	// github's own {arch_any}/{os_any} regex matching (ghrelease package)
	// must keep receiving the RAW fact value via _current_arch/_current_os,
	// never the arch_map-resolved one — it tries every known synonym
	// itself and arch_map only ever picks a single spelling.
	p := writeSchema(t, `
[defaults]
manager = "native"
arch_map = { aarch64 = "arm64" }

[tools]
fastfetch = { github = { repo = "fastfetch-cli/fastfetch", asset = "fastfetch-{os_any}-{arch_any}" } }
`)
	s, err := ParseProjectSchema(p, aarch64Map())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	mc := s.Tools["fastfetch"].Methods[1]
	if mc.Kind != "github" {
		t.Fatalf("expected github, got %q", mc.Kind)
	}
	if mc.Config["_current_arch"] != "aarch64" {
		t.Fatalf("_current_arch should stay raw, got %v", mc.Config["_current_arch"])
	}
	if mc.Config["_current_os"] != "darwin" {
		t.Fatalf("_current_os should stay raw, got %v", mc.Config["_current_os"])
	}
}

func TestArchMap_PreAndPostInstallUseRawValueNotAlias(t *testing.T) {
	// arch_map/os_map are scoped to [defaults] and method Config blocks —
	// PreInstall/PostInstall are tool-level command strings and always get
	// the raw fact value, same as every other placeholder, regardless of
	// any arch_map/os_map declared elsewhere in the schema.
	p := writeSchema(t, `
[defaults]
manager = "native"
arch_map = { aarch64 = "arm64" }

[tools.fastfetch]
pre_install  = "echo building for {arch}"
post_install = "echo installed on {os}"
  [tools.fastfetch.http]
  url = "https://x.com/{arch}/fastfetch.tar.gz"
`)
	s, err := ParseProjectSchema(p, aarch64Map())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool := s.Tools["fastfetch"]
	if want := "echo building for aarch64"; tool.PreInstall != want {
		t.Fatalf("PreInstall:\n got: %v\nwant: %v", tool.PreInstall, want)
	}
	if want := "echo installed on darwin"; tool.PostInstall != want {
		t.Fatalf("PostInstall:\n got: %v\nwant: %v", tool.PostInstall, want)
	}
	// Meanwhile the sibling http method's url DID get the alias applied.
	mc := tool.Methods[1]
	if want := "https://x.com/arm64/fastfetch.tar.gz"; mc.Config["url"] != want {
		t.Fatalf("url:\n got: %v\nwant: %v", mc.Config["url"], want)
	}
}

func TestArchMap_ValidateModeLeavesPlaceholdersLiteral(t *testing.T) {
	// ParseSchema is also called with m == nil/empty by the validate/check
	// path, to inspect the schema without resolving any machine facts.
	// {arch}/{os} must still come out untouched in that mode, exactly like
	// every other unresolved placeholder — arch_map/os_map must not force
	// a substitution when there are no facts to resolve against.
	p := writeSchema(t, `
[defaults]
manager = "native"
arch_map = { aarch64 = "arm64" }

[tools]
fastfetch = { http = { url = "https://x.com/{arch}/{os}/fastfetch.tar.gz" } }
`)
	s, err := ParseProjectSchema(p, nil)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	want := "https://x.com/{arch}/{os}/fastfetch.tar.gz"
	if got := s.Tools["fastfetch"].Methods[1].Config["url"]; got != want {
		t.Fatalf("validate-mode url:\n got: %v\nwant: %v", got, want)
	}
}
