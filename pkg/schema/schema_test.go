package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	err, warnings := Validate(s, []string{"native", "cargo"})
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
	err, warnings := Validate(s, []string{"native", "cargo"})
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
	err, warnings := Validate(s, []string{"native", "cargo"})
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
	// When bucket value is not bool(true), the key stays in valMap and
	// buildMethods will handle it (as an unknown method kind → error).
	// This should parse without panic/crash; validation catches unknown kinds.
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
	// "python" key stays in valMap since value is string, not bool true.
	// buildMethods creates a method with Kind="python" (no parse error, but
	// Validate will flag it as unknown kind).
	var found bool
	for _, m := range tool.Methods {
		if m.Kind == "python" {
			found = true
			if m.Err != nil {
				t.Fatalf("string value does not cause parse error, got: %v", m.Err)
			}
			if pkg, ok := m.Config["pkg"].(string); !ok || pkg != "some-string" {
				t.Fatalf("expected pkg 'some-string', got %q", pkg)
			}
		}
	}
	if !found {
		t.Fatal("expected python method to remain (not expanded) for non-bool value")
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