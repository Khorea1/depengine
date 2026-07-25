package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/engine"
)

// writeSchema writes a temp schema.toml next to the test, returning its path.
func writeSchema(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return p
}

// fixedMap is a deterministic substitution table that exercises the new
// placeholders end-to-end without invoking detect_os.sh. Keep the values
// here in sync with the assertions in each test.
func fixedMap() map[string]string {
	return map[string]string{
		"id":            "arch",
		"distro_family": "arch",
		"arch":          "x86_64",
		"os":            "linux",
		"kernel":        "6.7.0-arch",
		"libc":          "glibc",
		"init_system":   "systemd",
	}
}

func TestParseSchemaExpandsPlaceholdersInURL(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
fastfetch = { http = { url = "https://x.com/{os}/{arch}/{libc}/fastfetch-{arch}.deb" } }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["fastfetch"].Methods
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods (native + http), got %d", len(methods))
	}
	mc := methods[1] // native[0] + http[1]
	if mc.Kind != "http" {
		t.Fatalf("methods[1] expected http, got %q", mc.Kind)
	}
	want := "https://x.com/linux/x86_64/glibc/fastfetch-x86_64.deb"
	if got := mc.Config["url"]; got != want {
		t.Fatalf("url not expanded:\n got: %v\nwant: %v", got, want)
	}
}

func TestParseSchemaCollapsesNativeManagerOverrides(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
neovim = { pacman = "neovim-{arch}", apt = "neovim", brew = "neovim" }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	mc := s.Tools["neovim"].Methods[0]
	if mc.Kind != "native" {
		t.Fatalf("expected collapsed native method, got kind=%q", mc.Kind)
	}
	// Default pkg should be the tool name.
	if got := mc.Config["pkg"]; got != "neovim" {
		t.Fatalf("default pkg should be tool name, got %v", got)
	}
	// Overrides should be expanded.
	overrides, ok := mc.Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides in config")
	}
	if overrides["pacman"] != "neovim-x86_64" {
		t.Fatalf("pacman override not expanded: got %v", overrides["pacman"])
	}
	if overrides["apt"] != "neovim" {
		t.Fatalf("apt override mismatch: got %v", overrides["apt"])
	}
	if overrides["brew"] != "neovim" {
		t.Fatalf("brew override mismatch: got %v", overrides["brew"])
	}
}

func TestParseSchemaExpandsPlaceholdersInPostinstallAndBuild(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools.DepartureMono]
post_install = "echo installed on {os}/{arch} via {init_system}"

  [tools.DepartureMono.git]
  url   = "https://github.com/x/{os}/{arch}.git"
  build = "make OS={os} ARCH={arch} LIBC={libc}"
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool := s.Tools["DepartureMono"]
	if tool.PostInstall != "echo installed on linux/x86_64 via systemd" {
		t.Fatalf("postinstall not expanded: %q", tool.PostInstall)
	}
	if len(tool.Methods) != 2 {
		t.Fatalf("expected 2 methods (native + git), got %d", len(tool.Methods))
	}
	git := tool.Methods[1] // native[0] + git[1]
	if git.Kind != "git" {
		t.Fatalf("methods[1] expected git, got %q", git.Kind)
	}
	if git.Config["url"] != "https://github.com/x/linux/x86_64.git" {
		t.Fatalf("git url not expanded: %v", git.Config["url"])
	}
	if git.Config["build"] != "make OS=linux ARCH=x86_64 LIBC=glibc" {
		t.Fatalf("git build not expanded: %v", git.Config["build"])
	}
}

func TestParseSchemaPreservesLatestPlaceholderSlot(t *testing.T) {
	// {latest} is owned by the http/git adapter at install time; it must
	// survive fact-substitution untouched.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ff = { http = { url = "https://x.com/{latest}/ff-{arch}.deb" } }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["ff"].Methods
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods (native + http), got %d", len(methods))
	}
	mc := methods[1] // native[0] + http[1]
	if mc.Kind != "http" {
		t.Fatalf("methods[1] expected http, got %q", mc.Kind)
	}
	want := "https://x.com/{latest}/ff-x86_64.deb"
	if got := mc.Config["url"]; got != want {
		t.Fatalf("want %q, got %v", want, got)
	}
}

func TestParseSchemaUnknownPlaceholderLeftUntouched(t *testing.T) {
	// a typo like {archh} must survive so the validator can flag it; the
	// engine must not silently turn it into an empty string.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ff = { http = { url = "https://x.com/{archh}/x" } }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["ff"].Methods
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods (native + http), got %d", len(methods))
	}
	mc := methods[1] // native[0] + http[1]
	if mc.Kind != "http" {
		t.Fatalf("methods[1] expected http, got %q", mc.Kind)
	}
	if got := mc.Config["url"]; got != "https://x.com/{archh}/x" {
		t.Fatalf("unknown placeholder was altered: %v", got)
	}
}

func TestParseSchemaLanguageKeysRemainSeparate(t *testing.T) {
	// pip/pipx/uv are NOT native manager names → should stay as separate methods.
	// A native method is auto-injected before them.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
organize = { pip = "organize-tool", pipx = "organize-tool", uv = "organize-tool" }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["organize"].Methods
	if len(methods) != 4 {
		t.Fatalf("expected 4 methods (native + pip/pipx/uv), got %d", len(methods))
	}
	if methods[0].Kind != "native" {
		t.Fatalf("methods[0] expected native (injected), got %q", methods[0].Kind)
	}
	if methods[0].Config["pkg"] != "organize" {
		t.Fatalf("injected native pkg should be tool name, got %v", methods[0].Config["pkg"])
	}
	if methods[1].Kind != "pipx" || methods[2].Kind != "uv" || methods[3].Kind != "pip" {
		t.Fatalf("expected kinds native/pipx/uv/pip, got %s/%s/%s/%s",
			methods[0].Kind, methods[1].Kind, methods[2].Kind, methods[3].Kind)
	}
}

func TestParseSchemaMixedNativeAndLanguageKeys(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
mytool = { apt = "foo-apt", cargo = "foo-cargo" }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["mytool"].Methods
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods (native+cargo), got %d", len(methods))
	}
	// First: native method with apt override
	if methods[0].Kind != "native" {
		t.Fatalf("methods[0] expected native, got %q", methods[0].Kind)
	}
	overrides, ok := methods[0].Config["pkg_overrides"].(map[string]any)
	if !ok {
		t.Fatal("expected pkg_overrides in native method")
	}
	if overrides["apt"] != "foo-apt" {
		t.Fatalf("apt override: got %v", overrides["apt"])
	}
	if methods[0].Config["pkg"] != "mytool" {
		t.Fatalf("default pkg should be tool name, got %v", methods[0].Config["pkg"])
	}
	// Second: cargo method
	if methods[1].Kind != "cargo" {
		t.Fatalf("methods[1] expected cargo, got %q", methods[1].Kind)
	}
	if methods[1].Config["pkg"] != "foo-cargo" {
		t.Fatalf("cargo pkg mismatch: got %v", methods[1].Config["pkg"])
	}
}

func TestParseSchemaBlockSyntaxNativeKeysNotCollapsed(t *testing.T) {
	// Block-style CASO 8 with when clause → "apt" not collapsed (explicit method).
	// A native method is auto-injected before it as fallback.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools.foo]
  [tools.foo.apt]
  pkg  = "foo-apt"
  when = { distro_family = ["debian"] }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["foo"].Methods
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods (native + apt), got %d", len(methods))
	}
	if methods[0].Kind != "native" {
		t.Fatalf("methods[0] expected native (injected), got %q", methods[0].Kind)
	}
	if methods[0].Config["pkg"] != "foo" {
		t.Fatalf("injected native pkg should be tool name, got %v", methods[0].Config["pkg"])
	}
	mc := methods[1]
	if mc.Kind != "apt" {
		t.Fatalf("block-style should keep kind=%q, got %q", "apt", mc.Kind)
	}
	if mc.Config["pkg"] != "foo-apt" {
		t.Fatalf("pkg mismatch: got %v", mc.Config["pkg"])
	}
	if mc.When == nil || len(mc.When.DistroFamily) != 1 || mc.When.DistroFamily[0] != "debian" {
		t.Fatalf("when clause not preserved: %+v", mc.When)
	}
}

func TestParseSchemaSimpleListAndWhen(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"
method_order = ["native", "aur"]

[tools]
simple = ["zsh", "bat"]

  [tools.foo.aur]
  pkg  = "foo-{arch}"
  when = { distro_family = ["arch"] }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(s.Tools))
	}
	if !s.Tools["zsh"].IsSimple {
		t.Fatalf("zsh should be IsSimple")
	}
	foo := s.Tools["foo"]
	if len(foo.Methods) != 2 {
		t.Fatalf("expected 2 methods (native + aur), got %d", len(foo.Methods))
	}
	if foo.Methods[0].Kind != "native" {
		t.Fatalf("foo method[0] = %v, want native (injected)", foo.Methods[0].Kind)
	}
	if foo.Methods[0].Config["pkg"] != "foo" {
		t.Fatalf("injected native pkg should be tool name, got %v", foo.Methods[0].Config["pkg"])
	}
	if foo.Methods[1].Kind != "aur" {
		t.Fatalf("foo method[1] = %v, want aur", foo.Methods[1].Kind)
	}
	if foo.Methods[1].When == nil || len(foo.Methods[1].When.DistroFamily) != 1 || foo.Methods[1].When.DistroFamily[0] != "arch" {
		t.Fatalf("foo when not parsed: %+v", foo.Methods[1].When)
	}
	if foo.Methods[1].Config["pkg"] != "foo-x86_64" {
		t.Fatalf("pkg not expanded: %v", foo.Methods[1].Config["pkg"])
	}
}

func TestValidateRejectsUnreachableTool(t *testing.T) {
	s := &Schema{
		Defaults: Defaults{MethodOrder: []string{"native"}},
		Tools: map[string]*Tool{
			"mytool": {
				Name: "mytool",
				Methods: []*MethodCandidate{
					{Kind: "nonexistent"},
				},
			},
		},
	}
	warnings, err := Validate(s, []string{"native", "cargo"})
	if err == nil {
		t.Fatal("expected error for unreachable tool")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("error should mention unknown kind %q, got: %v", "nonexistent", err)
	}
	if !strings.Contains(err.Error(), "mytool") {
		t.Fatalf("error should mention tool name %q, got: %v", "mytool", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestValidateWarnsOnUnknownKindWithKnownFallback(t *testing.T) {
	s := &Schema{
		Defaults: Defaults{MethodOrder: []string{"cargo", "nonexistent"}},
		Tools: map[string]*Tool{
			"mytool": {
				Name: "mytool",
				Methods: []*MethodCandidate{
					{Kind: "nonexistent"},
					{Kind: "cargo"},
				},
			},
		},
	}
	warnings, err := Validate(s, []string{"native", "cargo"})
	if err != nil {
		t.Fatalf("expected no error (has known fallback), got: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning about unknown kind")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "nonexistent") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warning mentioning %q, got: %v", "nonexistent", warnings)
	}
}

func TestValidateAcceptsAllKnown(t *testing.T) {
	s := &Schema{
		Defaults: Defaults{MethodOrder: []string{"native", "cargo"}},
		Tools: map[string]*Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*MethodCandidate{
					{Kind: "native"},
				},
			},
		},
	}
	warnings, err := Validate(s, []string{"native", "cargo"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestParseSchemaToolWithTagsInBlock(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools.myapp]
tags = ["desktop", "server"]
manager = "native"
pkg = "myapp"`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["myapp"]
	if !ok {
		t.Fatal("expected tool myapp")
	}
	if len(tool.Tags) != 2 || tool.Tags[0] != "desktop" || tool.Tags[1] != "server" {
		t.Fatalf("expected tags [desktop server], got %v", tool.Tags)
	}
}

func TestParseSchemaToolWithTagsInInline(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
mycli = { tags = ["minimal"], manager = "native", pkg = "mycli" }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["mycli"]
	if !ok {
		t.Fatal("expected tool mycli")
	}
	if len(tool.Tags) != 1 || tool.Tags[0] != "minimal" {
		t.Fatalf("expected tags [minimal], got %v", tool.Tags)
	}
}

func TestParseSchemaSimpleToolNoTags(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
simple = ["zsh", "bat"]`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	for _, name := range []string{"zsh", "bat"} {
		tool, ok := s.Tools[name]
		if !ok {
			t.Fatalf("expected tool %s", name)
		}
		if len(tool.Tags) != 0 {
			t.Fatalf("simple tool %s should have no tags, got %v", name, tool.Tags)
		}
	}
}


func TestParseMethodBool_TrueReturnsEmptyPkg(t *testing.T) {
	mc, err := parseMethod("pipx", true)
	if err != nil {
		t.Fatalf("parseMethod(pipx, true): %v", err)
	}
	if mc.Kind != "pipx" {
		t.Fatalf("expected kind pipx, got %s", mc.Kind)
	}
	pkg, ok := mc.Config["pkg"].(string)
	if !ok {
		t.Fatalf("Config[pkg] is not a string: %T", mc.Config["pkg"])
	}
	if pkg != "" {
		t.Fatalf("expected empty pkg (SubstitutePkg fallback), got %q", pkg)
	}
}

func TestParseMethodBool_FalseReturnsError(t *testing.T) {
	_, err := parseMethod("pipx", false)
	if err == nil {
		t.Fatal("expected error for false, got nil")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("error should mention false, got: %v", err)
	}
}

func TestParseMethodBool_StringStillWorks(t *testing.T) {
	mc, err := parseMethod("pipx", "ruff")
	if err != nil {
		t.Fatalf("parseMethod(pipx, ruff): %v", err)
	}
	pkg, ok := mc.Config["pkg"].(string)
	if !ok {
		t.Fatalf("Config[pkg] is not a string: %T", mc.Config["pkg"])
	}
	if pkg != "ruff" {
		t.Fatalf("expected pkg ruff, got %q", pkg)
	}
}

func TestParseSchemaToolWithBoolMethods(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ruff = { pipx = true, uv = true }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["ruff"]
	if !ok {
		t.Fatal("expected tool ruff")
	}
	// Should have native (auto-injected) + pipx + uv = 3 methods
	if len(tool.Methods) != 3 {
		t.Fatalf("expected 3 methods (native, pipx, uv), got %d", len(tool.Methods))
	}
	gotPipx := false
	gotUv := false
	gotNative := false
	for _, m := range tool.Methods {
		switch m.Kind {
		case "pipx":
			gotPipx = true
			if m.Err != nil {
				t.Fatalf("pipx method has error: %v", m.Err)
			}
		case "uv":
			gotUv = true
		case "native":
			gotNative = true
		}
	}
	if !gotPipx {
		t.Fatal("missing pipx method")
	}
	if !gotUv {
		t.Fatal("missing uv method")
	}
	if !gotNative {
		t.Fatal("missing native method")
	}
}

func TestBucketExpansion_Python(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ruff = { python = true }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["ruff"]
	if !ok {
		t.Fatal("expected tool ruff")
	}
	// python → pip + pipx + uv + native (auto-injected) = 4 methods
	if len(tool.Methods) != 4 {
		t.Fatalf("expected 4 methods (native, pip, pipx, uv), got %d: %v", len(tool.Methods), methodKinds(tool.Methods))
	}
	kinds := methodKindSet(tool.Methods)
	for _, want := range []string{"native", "pip", "pipx", "uv"} {
		if !kinds[want] {
			t.Fatalf("missing method %q in %v", want, methodKinds(tool.Methods))
		}
	}
}

func TestBucketExpansion_Node(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
prettier = { node = true }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["prettier"]
	if !ok {
		t.Fatal("expected tool prettier")
	}
	// node → npm + pnpm + bun + native (auto-injected) = 4 methods
	if len(tool.Methods) != 4 {
		t.Fatalf("expected 4 methods (native, npm, pnpm, bun), got %d: %v", len(tool.Methods), methodKinds(tool.Methods))
	}
	kinds := methodKindSet(tool.Methods)
	for _, want := range []string{"native", "npm", "pnpm", "bun"} {
		if !kinds[want] {
			t.Fatalf("missing method %q in %v", want, methodKinds(tool.Methods))
		}
	}
}

func TestBucketExpansion_ExplicitMethodNotOverwritten(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ruff = { pip = "organize-tool", python = true }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["ruff"]
	if !ok {
		t.Fatal("expected tool ruff")
	}
	// python → pipx + uv (pip already exists and is NOT overwritten)
	// Should have: native, pip (with pkg=organize-tool), pipx, uv = 4 methods
	if len(tool.Methods) != 4 {
		t.Fatalf("expected 4 methods (native, pip, pipx, uv), got %d: %v", len(tool.Methods), methodKinds(tool.Methods))
	}
	// Verify pip pkg is "organize-tool", not overridden
	for _, m := range tool.Methods {
		if m.Kind == "pip" {
			if pkg, ok := m.Config["pkg"].(string); !ok || pkg != "organize-tool" {
				t.Fatalf("pip method pkg should be 'organize-tool' (not overwritten), got %q", pkg)
			}
		}
	}
	kinds := methodKindSet(tool.Methods)
	for _, want := range []string{"native", "pip", "pipx", "uv"} {
		if !kinds[want] {
			t.Fatalf("missing method %q in %v", want, methodKinds(tool.Methods))
		}
	}
}


func TestBucketExpansion_BucketWithStringValNotExpanded(t *testing.T) {
	// When bucket value is a string, it expands to each method in the bucket.
	// The native method is auto-injected by buildMethods.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ruff = { python = "some-string" }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["ruff"]
	if !ok {
		t.Fatal("expected tool ruff")
	}
	// Bucket expansion creates methods for each method kind in the python bucket:
	// pip, pipx, uv — each with pkg="some-string"
	expectedKinds := map[string]string{
		"pip":  "some-string",
		"pipx": "some-string",
		"uv":   "some-string",
	}
	found := make(map[string]bool)
	for _, m := range tool.Methods {
		if want, ok := expectedKinds[m.Kind]; ok {
			found[m.Kind] = true
			if pkg, ok := m.Config["pkg"].(string); !ok || pkg != want {
				t.Fatalf("%s: expected pkg %q, got %q", m.Kind, want, pkg)
			}
		}
	}
	for kind := range expectedKinds {
		if !found[kind] {
			t.Fatalf("expected method %q from bucket expansion, not found", kind)
		}
	}
}

func TestBucketExpansion_BucketWithFalseNotExpanded(t *testing.T) {
	// false keeps the key in valMap; buildMethods will try to parse it and get an error.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ruff = { python = false }`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["ruff"]
	if !ok {
		t.Fatal("expected tool ruff")
	}
	var found bool
	for _, m := range tool.Methods {
		if m.Kind == "python" {
			found = true
			if m.Err == nil {
				t.Fatal("expected error on python method with false")
			}
		}
	}
	if !found {
		t.Fatal("expected python method to remain (not expanded) for false value")
	}
}


// --- Per-tool method_prefer, method_only, and deprecated method_order ---

func TestToolMethodPrefer(t *testing.T) {
	// method_prefer prepends to the default order without removing other methods.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_prefer = ["cargo"], cargo = true }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["myapp"]
	if !ok {
		t.Fatal("tool myapp not found")
	}
	// MethodPrefer should be set.
	if len(tool.MethodPrefer) != 1 || tool.MethodPrefer[0] != "cargo" {
		t.Fatalf("expected MethodPrefer [\"cargo\"], got %v", tool.MethodPrefer)
	}
	// Deprecated MethodOrder should also be set for backward compat.
	if len(tool.MethodOrder) != 0 {
		t.Fatalf("expected MethodOrder to be empty (only set by method_order key), got %v", tool.MethodOrder)
	}
}

func TestToolMethodOnly(t *testing.T) {
	// method_only restricts to only the listed methods, in that order.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_only = ["cargo"], cargo = true }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["myapp"]
	if !ok {
		t.Fatal("tool myapp not found")
	}
	if len(tool.MethodOnly) != 1 || tool.MethodOnly[0] != "cargo" {
		t.Fatalf("expected MethodOnly [\"cargo\"], got %v", tool.MethodOnly)
	}
}

func TestToolDeprecatedMethodOrder(t *testing.T) {
	// Deprecated method_order still works (backward compat) with same semantics
	// as method_prefer (sets both MethodPrefer and MethodOrder).
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_order = ["cargo"], cargo = true }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["myapp"]
	if !ok {
		t.Fatal("tool myapp not found")
	}
	if len(tool.MethodOrder) != 1 || tool.MethodOrder[0] != "cargo" {
		t.Fatalf("expected MethodOrder [\"cargo\"], got %v", tool.MethodOrder)
	}
	// Backward compat: MethodPrefer should also be set.
	if len(tool.MethodPrefer) != 1 || tool.MethodPrefer[0] != "cargo" {
		t.Fatalf("expected MethodPrefer [\"cargo\"] from deprecated method_order, got %v", tool.MethodPrefer)
	}
}

func TestToolMethodPreferAndOrderPreferWins(t *testing.T) {
	// When both method_order and method_prefer are specified, method_prefer wins.
	// The tool should only have MethodPrefer set by method_prefer (not by method_order).
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_order = ["pip"], method_prefer = ["cargo"], cargo = true }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool, ok := s.Tools["myapp"]
	if !ok {
		t.Fatal("tool myapp not found")
	}
	// MethodPrefer should be set from method_prefer, not method_order.
	if len(tool.MethodPrefer) != 1 || tool.MethodPrefer[0] != "cargo" {
		t.Fatalf("expected MethodPrefer [\"cargo\"] (method_prefer wins), got %v", tool.MethodPrefer)
	}
}


func TestEffectiveMethodOrderDefaults(t *testing.T) {
	// No per-tool method preference → default order is returned unmodified.
	tool := &Tool{Name: "test"}
	defaultOrder := []string{"native", "cargo", "pip"}
	got := EffectiveMethodOrder(tool, defaultOrder, "")
	if len(got) != len(defaultOrder) {
		t.Fatalf("expected length %d, got %d: %v", len(defaultOrder), len(got), got)
	}
	for i, k := range defaultOrder {
		if got[i] != k {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], k)
		}
	}
}

func TestEffectiveMethodOrderPrefer(t *testing.T) {
	// method_prefer prepends to default order, with defaults appended (no duplicates).
	tool := &Tool{
		Name:        "test",
		MethodPrefer: []string{"cargo"},
	}
	defaultOrder := []string{"native", "cargo", "pip"}
	got := EffectiveMethodOrder(tool, defaultOrder, "")
	expected := []string{"cargo", "native", "pip"}
	if len(got) != len(expected) {
		t.Fatalf("expected length %d, got %d: %v", len(expected), len(got), got)
	}
	for i, k := range expected {
		if got[i] != k {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], k)
		}
	}
}

func TestEffectiveMethodOrderOnly(t *testing.T) {
	// method_only returns only the listed methods.
	tool := &Tool{
		Name:       "test",
		MethodOnly: []string{"go"},
	}
	defaultOrder := []string{"native", "cargo", "go", "pip"}
	got := EffectiveMethodOrder(tool, defaultOrder, "")
	expected := []string{"go"}
	if len(got) != len(expected) {
		t.Fatalf("expected length %d, got %d: %v", len(expected), len(got), got)
	}
	for i, k := range expected {
		if got[i] != k {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], k)
		}
	}
}

func TestEffectiveMethodOrderDeprecated(t *testing.T) {
	// Deprecated method_order works like method_prefer.
	tool := &Tool{
		Name:        "test",
		MethodOrder: []string{"git"},
	}
	defaultOrder := []string{"native", "git", "http"}
	got := EffectiveMethodOrder(tool, defaultOrder, "")
	expected := []string{"git", "native", "http"}
	if len(got) != len(expected) {
		t.Fatalf("expected length %d, got %d: %v", len(expected), len(got), got)
	}
	for i, k := range expected {
		if got[i] != k {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], k)
		}
	}
}

// helpers for test assertions
func methodKinds(methods []*MethodCandidate) []string {
	kinds := make([]string, len(methods))
	for i, m := range methods {
		kinds[i] = m.Kind
	}
	return kinds
}

func methodKindSet(methods []*MethodCandidate) map[string]bool {
	set := make(map[string]bool, len(methods))
	for _, m := range methods {
		set[m.Kind] = true
	}
	return set
}


func TestConditionMatches(t *testing.T) {
	// Build a baseline Facts that would match a debian system.
	facts := &engine.Facts{
		DistroID:     "ubuntu",
		DistroIDLike: "debian",
		TargetArch:   "x86_64",
		OS:           "linux",
		Kernel:       "6.7.0-generic",
		Libc:         "glibc 2.35",
		InitSystem:   "systemd",
		IsWSL:        false,
		IsContainer:  false,
	}

	// nil condition always matches
	var nilCond *Condition
	if !nilCond.Match(facts) {
		t.Error("nil condition should always match")
	}

	// empty (zero) condition always matches
	empty := &Condition{}
	if !empty.Match(facts) {
		t.Error("empty condition should always match")
	}

	// DistroFamily match
	c := &Condition{DistroFamily: []string{"debian"}}
	if !c.Match(facts) {
		t.Error("ubuntu is debian family, should match")
	}
	c2 := &Condition{DistroFamily: []string{"arch"}}
	if c2.Match(facts) {
		t.Error("ubuntu is not arch family, should not match")
	}

	// DistroID match (case-insensitive)
	c3 := &Condition{DistroID: []string{"Ubuntu"}}
	if !c3.Match(facts) {
		t.Error("DistroID Ubuntu should match (case-insensitive)")
	}
	c4 := &Condition{DistroID: []string{"debian"}}
	if c4.Match(facts) {
		t.Error("DistroID debian should not match ubuntu")
	}

	// Arch match
	c5 := &Condition{Arch: []string{"x86_64", "aarch64"}}
	if !c5.Match(facts) {
		t.Error("x86_64 should match")
	}
	c6 := &Condition{Arch: []string{"aarch64"}}
	if c6.Match(facts) {
		t.Error("aarch64 should not match x86_64")
	}

	// OS match
	c7 := &Condition{OS: []string{"linux"}}
	if !c7.Match(facts) {
		t.Error("linux OS should match")
	}
	c8 := &Condition{OS: []string{"windows"}}
	if c8.Match(facts) {
		t.Error("windows OS should not match linux")
	}

	// Kernel match
	c9 := &Condition{Kernel: []string{"6.7.0-generic"}}
	if !c9.Match(facts) {
		t.Error("kernel should match exactly")
	}

	// Libc prefix match
	c10 := &Condition{Libc: []string{"glibc"}}
	if !c10.Match(facts) {
		t.Error("libc 'glibc' should prefix-match 'glibc 2.35'")
	}
	c11 := &Condition{Libc: []string{"musl"}}
	if c11.Match(facts) {
		t.Error("libc 'musl' should not match 'glibc 2.35'")
	}

	// InitSystem match
	c12 := &Condition{InitSystem: []string{"systemd"}}
	if !c12.Match(facts) {
		t.Error("init_system systemd should match")
	}

	// Three-state bools: IsWSL = false, facts.IsWSL = false → OK
	c13 := &Condition{IsWSL: new(false)}
	if !c13.Match(facts) {
		t.Error("IsWSL=false should match facts.IsWSL=false")
	}

	// Three-state bools: IsContainer = true, facts.IsContainer = false → fail
	c14 := &Condition{IsContainer: new(true)}
	if c14.Match(facts) {
		t.Error("IsContainer=true should NOT match facts.IsContainer=false")
	}

	// AND semantics: all fields must match
	c15 := &Condition{
		DistroFamily: []string{"debian"},
		Arch:         []string{"x86_64"},
		Libc:         []string{"glibc"},
	}
	if !c15.Match(facts) {
		t.Error("all three conditions should match")
	}

	// AND semantics: one field fails
	c16 := &Condition{
		DistroFamily: []string{"debian"},
		Arch:         []string{"aarch64"},
	}
	if c16.Match(facts) {
		t.Error("arch aarch64 should fail on x86_64 system")
	}
}

func TestConditionMatchesNilFacts(t *testing.T) {
	// nil condition → always true
	var nilCond *Condition
	if !nilCond.Match(nil) {
		t.Error("nil condition with nil facts should be true")
	}

	// zero condition with nil facts → true (conservative)
	empty := &Condition{}
	if !empty.Match(nil) {
		t.Error("empty condition with nil facts should be true")
	}

	// non-zero condition with nil facts → false (conservative: can't verify)
	c := &Condition{DistroFamily: []string{"debian"}}
	if c.Match(nil) {
		t.Error("non-empty condition with nil facts should be false (conservative)")
	}

	c2 := &Condition{Arch: []string{"x86_64"}}
	if c2.Match(nil) {
		t.Error("non-empty condition with nil facts should be false (conservative)")
	}
}

func TestConditionMatchesPartialFacts(t *testing.T) {
	// Facts with only DistroID set — simulate partial detection
	facts := &engine.Facts{
		DistroID:   "arch",
		TargetArch: runtime.GOARCH,
		OS:         runtime.GOOS,
	}

	// Match on distro_id only should work
	c := &Condition{DistroID: []string{"arch"}}
	if !c.Match(facts) {
		t.Error("DistroID arch should match")
	}

	// Match on a field that IS set should work with zero-value others
	c2 := &Condition{Arch: []string{runtime.GOARCH}}
	if !c2.Match(facts) {
		t.Errorf("Arch %s should match", runtime.GOARCH)
	}

	// Match on a missing field (not in facts) should fail if condition requires it
	// facts.Kernel is "" — Kernel condition with "anything" won't match
	c3 := &Condition{Kernel: []string{"some-kernel"}}
	if c3.Match(facts) {
		t.Error("Kernel condition should not match when facts.Kernel is empty")
	}
}

func TestConditionIsZero(t *testing.T) {
	tests := []struct {
		name   string
		cond   *Condition
		isZero bool
	}{
		{"nil cond", nil, true},
		{"empty", &Condition{}, true},
		{"distro_family", &Condition{DistroFamily: []string{"arch"}}, false},
		{"distro_id", &Condition{DistroID: []string{"ubuntu"}}, false},
		{"arch", &Condition{Arch: []string{"x86_64"}}, false},
		{"os", &Condition{OS: []string{"linux"}}, false},
		{"kernel", &Condition{Kernel: []string{"6.7.0"}}, false},
		{"libc", &Condition{Libc: []string{"glibc"}}, false},
		{"init_system", &Condition{InitSystem: []string{"systemd"}}, false},
		{"is_wsl set", &Condition{IsWSL: new(true)}, false},
		{"is_container set", &Condition{IsContainer: new(false)}, false},
		{"is_wsl nil is zero", &Condition{DistroFamily: []string{}}, true},
	}

	// Note: nil receiver doesn't have IsZero, handle separately
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cond == nil {
				return // nil receiver test handled separately
			}
			got := tt.cond.IsZero()
			if got != tt.isZero {
				t.Errorf("IsZero() = %v, want %v for %s", got, tt.isZero, tt.name)
			}
		})
	}
}

func TestExpandBucketsNoOp(t *testing.T) {
	// Order with no bucket names returns unchanged
	input := []string{"native", "cargo", "go"}
	got := ExpandBuckets(input)
	if len(got) != len(input) {
		t.Fatalf("expected length %d, got %d", len(input), len(got))
	}
	for i, v := range input {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}

	// Empty input
	got = ExpandBuckets(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
}

func TestExpandBucketsExpansion(t *testing.T) {
	// "python" → ["pip", "pipx", "uv"]
	got := ExpandBuckets([]string{"python"})
	want := []string{"pip", "pipx", "uv"}
	if len(got) != len(want) {
		t.Fatalf("expected %d elements, got %d: %v", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}

	// Mixed
	got = ExpandBuckets([]string{"native", "python", "cargo"})
	want = []string{"native", "pip", "pipx", "uv", "cargo"}
	if len(got) != len(want) {
		t.Fatalf("expected %d elements, got %d: %v", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestExpandBucketsPreservesLiteralAfterBucket(t *testing.T) {
	// "python" + "pip" → bucket expands to ["pip", "pipx", "uv"], then "pip" literal stays
	// Dedup only applies WITHIN bucket expansion, not for literal entries
	got := ExpandBuckets([]string{"python", "pip"})
	want := []string{"pip", "pipx", "uv", "pip"}
	if len(got) != len(want) {
		t.Fatalf("expected %d elements, got %d: %v", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestEffectiveMethodOrderWithBuckets(t *testing.T) {
	// No native expansion (empty nativeManagerName), but bucket expansion should work
	defaultOrder := []string{"native", "cargo"}

	tool := &Tool{
		Name:        "test",
		MethodPrefer: []string{"python"},
	}
	got := EffectiveMethodOrder(tool, defaultOrder, "")
	// python → pip,pipx,uv prepended, then native,cargo remain (no duplicates)
	expected := []string{"pip", "pipx", "uv", "native", "cargo"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d elements, got %d: %v", len(expected), len(got), got)
	}
	for i, v := range expected {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}

	// No per-tool overrides — bucket in defaults
	tool2 := &Tool{Name: "test2"}
	defaultOrder2 := []string{"python", "native"}
	got2 := EffectiveMethodOrder(tool2, defaultOrder2, "")
	expected2 := []string{"pip", "pipx", "uv", "native"}
	if len(got2) != len(expected2) {
		t.Fatalf("expected %d elements, got %d: %v", len(expected2), len(got2), got2)
	}
	for i, v := range expected2 {
		if got2[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got2[i], v)
		}
	}
}

func TestValidateAcceptsBucketNames(t *testing.T) {
	// Bucket names in defaults.method_order should produce a warning, not an error
	s := &Schema{
		Defaults: Defaults{
			MethodOrder: []string{"native", "python", "node"},
		},
		Tools: map[string]*Tool{
			"mytool": {
				Name:    "mytool",
				Methods: []*MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "mytool"}}},
			},
		},
	}

	// knownKinds includes the expanded bucket members
	warnings, err := Validate(s, []string{"native", "pip", "pipx", "uv", "npm", "pnpm", "bun"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect warnings about bucket expansion
	foundPython := false
	foundNode := false
	for _, w := range warnings {
		if strings.Contains(w, "python") && strings.Contains(w, "bucket") {
			foundPython = true
		}
		if strings.Contains(w, "node") && strings.Contains(w, "bucket") {
			foundNode = true
		}
	}
	if !foundPython {
		t.Error("expected warning about 'python' bucket name")
	}
	if !foundNode {
		t.Error("expected warning about 'node' bucket name")
	}

	// Bucket names in per-tool method_prefer should be accepted (no error)
	s2 := &Schema{
		Defaults: Defaults{
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*Tool{
			"mytool": {
				Name:        "mytool",
				MethodPrefer: []string{"python"},
				Methods:     []*MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "mytool"}}},
			},
		},
	}
	warnings2, err2 := Validate(s2, []string{"native", "pip", "pipx", "uv"})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	_ = warnings2 // bucket names in per-tool lists don't generate warnings (they pass silently)
}

func TestValidateRejectsUnknownKindInMethodPrefer(t *testing.T) {
	// method_prefer with nonexistent kind should be a hard error
	s := &Schema{
		Defaults: Defaults{
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*Tool{
			"mytool": {
				Name:        "mytool",
				MethodPrefer: []string{"nonexistent"},
				Methods:     []*MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "mytool"}}},
			},
		},
	}

	_, err := Validate(s, []string{"native"})
	if err == nil {
		t.Fatal("expected error for unknown kind in method_prefer")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention 'nonexistent', got: %v", err)
	}

	// Also test method_only with unknown kind
	s2 := &Schema{
		Defaults: Defaults{
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*Tool{
			"mytool": {
				Name:      "mytool",
				MethodOnly: []string{"fakekind"},
				Methods:   []*MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "mytool"}}},
			},
		},
	}
	_, err2 := Validate(s2, []string{"native"})
	if err2 == nil {
		t.Fatal("expected error for unknown kind in method_only")
	}
}

func TestParseConditionNewFields(t *testing.T) {
	// Each new field parses correctly in isolation
	tests := []struct {
		name  string
		input map[string]any
		check func(*Condition) bool
	}{
		{"distro_id", map[string]any{"distro_id": []any{"ubuntu"}}, func(c *Condition) bool {
			return len(c.DistroID) == 1 && c.DistroID[0] == "ubuntu" && c.DistroFamily == nil
		}},
		{"arch", map[string]any{"arch": []any{"x86_64", "aarch64"}}, func(c *Condition) bool {
			return len(c.Arch) == 2 && c.Arch[0] == "x86_64" && c.Arch[1] == "aarch64"
		}},
		{"os", map[string]any{"os": []any{"linux"}}, func(c *Condition) bool {
			return len(c.OS) == 1 && c.OS[0] == "linux"
		}},
		{"kernel", map[string]any{"kernel": []any{"6.7.0"}}, func(c *Condition) bool {
			return len(c.Kernel) == 1 && c.Kernel[0] == "6.7.0"
		}},
		{"libc", map[string]any{"libc": []any{"musl"}}, func(c *Condition) bool {
			return len(c.Libc) == 1 && c.Libc[0] == "musl"
		}},
		{"init_system", map[string]any{"init_system": []any{"systemd"}}, func(c *Condition) bool {
			return len(c.InitSystem) == 1 && c.InitSystem[0] == "systemd"
		}},
		{"is_wsl true", map[string]any{"is_wsl": true}, func(c *Condition) bool {
			return c.IsWSL != nil && *c.IsWSL == true
		}},
		{"is_wsl false", map[string]any{"is_wsl": false}, func(c *Condition) bool {
			return c.IsWSL != nil && *c.IsWSL == false
		}},
		{"is_container true", map[string]any{"is_container": true}, func(c *Condition) bool {
			return c.IsContainer != nil && *c.IsContainer == true
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := parseCondition(tt.input)
			if cond == nil {
				t.Fatal("parseCondition returned nil")
			}
			if !tt.check(cond) {
				t.Errorf("condition check failed for %s: %+v", tt.name, cond)
			}
		})
	}

	// Single-value string sugar for distro_id
	cond := parseCondition(map[string]any{"distro_id": "void"})
	if cond == nil {
		t.Fatal("parseCondition returned nil for string value")
	}
	if len(cond.DistroID) != 1 || cond.DistroID[0] != "void" {
		t.Errorf("expected [void], got %v", cond.DistroID)
	}
}

func TestParseConditionAllFields(t *testing.T) {
	// Multiple fields together — all should parse
	raw := map[string]any{
		"distro_family": []any{"debian"},
		"distro_id":     []any{"ubuntu"},
		"arch":          []any{"x86_64"},
		"os":            []any{"linux"},
		"kernel":        []any{"6.7.0"},
		"libc":          []any{"glibc"},
		"init_system":   []any{"systemd"},
		"is_wsl":        false,
		"is_container":  false,
	}
	cond := parseCondition(raw)
	if cond == nil {
		t.Fatal("parseCondition returned nil")
	}

	if len(cond.DistroFamily) != 1 || cond.DistroFamily[0] != "debian" {
		t.Errorf("DistroFamily: expected [debian], got %v", cond.DistroFamily)
	}
	if len(cond.DistroID) != 1 || cond.DistroID[0] != "ubuntu" {
		t.Errorf("DistroID: expected [ubuntu], got %v", cond.DistroID)
	}
	if len(cond.Arch) != 1 || cond.Arch[0] != "x86_64" {
		t.Errorf("Arch: expected [x86_64], got %v", cond.Arch)
	}
	if len(cond.OS) != 1 || cond.OS[0] != "linux" {
		t.Errorf("OS: expected [linux], got %v", cond.OS)
	}
	if len(cond.Kernel) != 1 || cond.Kernel[0] != "6.7.0" {
		t.Errorf("Kernel: expected [6.7.0], got %v", cond.Kernel)
	}
	if len(cond.Libc) != 1 || cond.Libc[0] != "glibc" {
		t.Errorf("Libc: expected [glibc], got %v", cond.Libc)
	}
	if len(cond.InitSystem) != 1 || cond.InitSystem[0] != "systemd" {
		t.Errorf("InitSystem: expected [systemd], got %v", cond.InitSystem)
	}
	if cond.IsWSL == nil || *cond.IsWSL != false {
		t.Errorf("IsWSL: expected false, got %v", cond.IsWSL)
	}
	if cond.IsContainer == nil || *cond.IsContainer != false {
		t.Errorf("IsContainer: expected false, got %v", cond.IsContainer)
	}
}

func TestParseConditionUnknownKeysStillIgnored(t *testing.T) {
	// Unknown keys should not cause an error (backward compat with future schema versions)
	raw := map[string]any{
		"distro_family":   []any{"arch"},
		"unknown_field":   "some_value",
		"another_unknown": []any{1, 2, 3},
	}
	cond := parseCondition(raw)
	if cond == nil {
		t.Fatal("parseCondition returned nil")
	}
	if len(cond.DistroFamily) != 1 || cond.DistroFamily[0] != "arch" {
		t.Errorf("DistroFamily should be parsed despite unknown keys: %v", cond.DistroFamily)
	}

	// When ONLY unknown keys are present, should return nil (IsZero)
	raw2 := map[string]any{
		"future_field": "future_value",
	}
	cond2 := parseCondition(raw2)
	if cond2 != nil {
		t.Error("parseCondition should return nil when only unknown keys are present")
	}
}

func TestParseMethodKindField(t *testing.T) {
	// kind = "http" on a block → mc.Kind == "http", mc.Label == "http-musl",
	// mc.Config does NOT contain "kind"
	mc, err := parseMethod("http-musl", map[string]any{
		"kind": "http",
		"url":  "https://example.com/musl",
	})
	if err != nil {
		t.Fatalf("parseMethod: %v", err)
	}
	if mc.Kind != "http" {
		t.Errorf("expected Kind %q, got %q", "http", mc.Kind)
	}
	if mc.Label != "http-musl" {
		t.Errorf("expected Label %q, got %q", "http-musl", mc.Label)
	}
	if _, ok := mc.Config["kind"]; ok {
		t.Errorf("Config should not contain 'kind' key, got %v", mc.Config)
	}
	if mc.Config["url"] != "https://example.com/musl" {
		t.Errorf("expected url in Config, got %v", mc.Config)
	}

	// kind = "" → falls back to section name as kind
	mc, err = parseMethod("http-musl", map[string]any{
		"kind": "",
		"url":  "https://example.com/musl",
	})
	if err != nil {
		t.Fatalf("parseMethod: %v", err)
	}
	if mc.Kind != "http-musl" {
		t.Errorf("expected Kind %q (section name fallback), got %q", "http-musl", mc.Kind)
	}
	if mc.Label != "" {
		t.Errorf("expected empty Label for empty kind, got %q", mc.Label)
	}

	// No kind field → existing behavior unchanged
	mc, err = parseMethod("http", map[string]any{
		"url": "https://example.com",
	})
	if err != nil {
		t.Fatalf("parseMethod: %v", err)
	}
	if mc.Kind != "http" {
		t.Errorf("expected Kind %q, got %q", "http", mc.Kind)
	}
	if mc.Label != "" {
		t.Errorf("expected empty Label, got %q", mc.Label)
	}

	// Non-string kind → ignored, kind stays in Config
	mc, err = parseMethod("http-musl", map[string]any{
		"kind": []string{"http"},
		"url":  "https://example.com",
	})
	if err != nil {
		t.Fatalf("parseMethod: %v", err)
	}
	if mc.Kind != "http-musl" {
		t.Errorf("expected Kind %q (unchanged), got %q", "http-musl", mc.Kind)
	}
	if _, ok := mc.Config["kind"]; !ok {
		t.Errorf("Config should contain 'kind' key for non-string kind values")
	}
}

func TestSchemaKindFieldResolvesAdapter(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools.restic]
  [tools.restic.http-musl]
  kind = "http"
  url = "https://example.com/musl"
  when = { libc = ["musl"] }
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool := s.Tools["restic"]
	if tool == nil {
		t.Fatal("expected tool 'restic' to be parsed")
	}

	// Find the http-musl method
	var found bool
	for _, m := range tool.Methods {
		if m.Label == "http-musl" {
			found = true
			if m.Kind != "http" {
				t.Errorf("expected Kind %q, got %q", "http", m.Kind)
			}
			if m.Label != "http-musl" {
				t.Errorf("expected Label %q, got %q", "http-musl", m.Label)
			}
		}
	}
	if !found {
		t.Fatal("expected method with Label 'http-musl'")
	}

	// Validate with knownKinds containing "http" should pass
	warnings, err := Validate(s, []string{"native", "http"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "http") {
			t.Fatalf("unexpected warning about 'http': %s", w)
		}
	}
}

func TestSchemaKindFieldBackwardCompat(t *testing.T) {
	// Existing schema without 'kind' field — Kind == section name, Label == ""
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools.restic]
  [tools.restic.http]
  url = "https://example.com"
`)
	s, err := ParseSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool := s.Tools["restic"]
	if tool == nil {
		t.Fatal("expected tool 'restic'")
	}

	var found bool
	for _, m := range tool.Methods {
		if m.Kind == "http" {
			found = true
			if m.Label != "" {
				t.Errorf("expected empty Label for backward compat, got %q", m.Label)
			}
		}
	}
	if !found {
		t.Fatal("expected method with Kind 'http'")
	}

	// Validate passes as before
	warnings, err := Validate(s, []string{"native", "http"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "http") && !strings.Contains(w, "method_order") {
			t.Fatalf("unexpected warning: %s", w)
		}
	}
}