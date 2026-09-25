package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/engine"
)

// boolPtr returns a pointer to v, for populating *bool condition fields
// (e.g. Condition.IsWSL, Condition.IsContainer) inline in test tables.
func boolPtr(v bool) *bool { return &v }

// writeSchema writes a temp schema.toml next to the test, returning its path.
func writeSchema(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.toml")
	if !strings.Contains(content, "schema_version") {
		content = "schema_version = 1\n" + content
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return p
}

func TestParseProjectSchemaRequiresSupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(path, []byte("[tools]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProjectSchema(path, nil); err == nil || !strings.Contains(err.Error(), "schema_version: required") {
		t.Fatalf("missing version error = %v", err)
	}
	path = writeSchema(t, "schema_version = 2\n[tools]\n")
	if _, err := ParseProjectSchema(path, nil); err == nil || !strings.Contains(err.Error(), "unsupported version 2") {
		t.Fatalf("unknown version error = %v", err)
	}
}

func TestParseProjectSchemaRejectsManifestSection(t *testing.T) {
	path := writeSchema(t, "schema_version = 1\n[packages]\n")
	if _, err := ParseProjectSchema(path, nil); err == nil || !strings.Contains(err.Error(), "packages: unknown root field") {
		t.Fatalf("wrong section error = %v", err)
	}
}

// fixedMap is a deterministic substitution table that exercises the new
// placeholders end-to-end without invoking host detection. Keep the values
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
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["fastfetch"].Methods
	if len(methods) != 1 {
		t.Fatalf("expected explicit http method only, got %d", len(methods))
	}
	mc := methods[0]
	if mc.Kind != "http" {
		t.Fatalf("methods[0] expected http, got %q", mc.Kind)
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tool := s.Tools["DepartureMono"]
	if got := tool.PostInstall[0].Run[2]; got != "echo installed on linux/x86_64 via systemd" {
		t.Fatalf("postinstall not expanded: %q", got)
	}
	if len(tool.Methods) != 1 {
		t.Fatalf("expected explicit git method only, got %d", len(tool.Methods))
	}
	git := tool.Methods[0]
	if git.Kind != "git" {
		t.Fatalf("methods[0] expected git, got %q", git.Kind)
	}
	if git.Config["url"] != "https://github.com/x/linux/x86_64.git" {
		t.Fatalf("git url not expanded: %v", git.Config["url"])
	}
	if git.Config["build"] != "make OS=linux ARCH=x86_64 LIBC=glibc" {
		t.Fatalf("git build not expanded: %v", git.Config["build"])
	}
}

func TestParseSchemaExpandsPortableBuildArguments(t *testing.T) {
	p := writeSchema(t, `
[tools]
tool = { git = { url = "https://example.com/tool.git", build = [{ run = ["go", "build", "-o", "tool-{arch}"] }, { run = ["strip", "tool-{arch}"] }] } }
`)
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	build := s.Tools["tool"].Methods[0].Config["build"].([]any)
	first := build[0].(map[string]any)["run"].([]any)
	second := build[1].(map[string]any)["run"].([]any)
	if first[3] != "tool-x86_64" || second[1] != "tool-x86_64" {
		t.Fatalf("portable build placeholders not expanded: %v", build)
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
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["ff"].Methods
	if len(methods) != 1 {
		t.Fatalf("expected explicit http method only, got %d", len(methods))
	}
	mc := methods[0]
	if mc.Kind != "http" {
		t.Fatalf("methods[0] expected http, got %q", mc.Kind)
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
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["ff"].Methods
	if len(methods) != 1 {
		t.Fatalf("expected explicit http method only, got %d", len(methods))
	}
	mc := methods[0]
	if mc.Kind != "http" {
		t.Fatalf("methods[0] expected http, got %q", mc.Kind)
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	// Block-style declaration is explicit: "apt" stays a distinct method and
	// does not receive an implicit native fallback.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools.foo]
  [tools.foo.apt]
  pkg  = "foo-apt"
  when = { distro_family = ["debian"] }
`)
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	methods := s.Tools["foo"].Methods
	if len(methods) != 1 {
		t.Fatalf("expected explicit apt method only, got %d", len(methods))
	}
	mc := methods[0]
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	if len(foo.Methods) != 1 {
		t.Fatalf("expected explicit aur method only, got %d", len(foo.Methods))
	}
	if foo.Methods[0].Kind != "aur" {
		t.Fatalf("foo method[0] = %v, want aur", foo.Methods[0].Kind)
	}
	if foo.Methods[0].When == nil || len(foo.Methods[0].When.DistroFamily) != 1 || foo.Methods[0].When.DistroFamily[0] != "arch" {
		t.Fatalf("foo when not parsed: %+v", foo.Methods[0].When)
	}
	if foo.Methods[0].Config["pkg"] != "foo-x86_64" {
		t.Fatalf("pkg not expanded: %v", foo.Methods[0].Config["pkg"])
	}
}

func TestParseSchemaMultipleGithubCandidatesDifferentWhen(t *testing.T) {
	// Two candidates of the same `kind = "github"` with different `when`
	// conditions parse as distinct MethodCandidate entries — this falls
	// out of the generic label+kind-override machinery in parseMethod.
	p := writeSchema(t, `
[defaults]
manager = "native"
method_order = ["native", "github"]

[tools.obsidian]
method_only = ["github"]

  [tools.obsidian.gh_apk]
  kind    = "github"
  repo    = "obsidianmd/obsidian-releases"
  asset   = "obsidian-{version}-android.apk"
  when    = { is_android = true }

  [tools.obsidian.gh_linux]
  kind  = "github"
  repo  = "obsidianmd/obsidian-releases"
  asset = "Obsidian-{version}.AppImage"
  when  = { os = ["linux"] }
`)
	s, err := ParseProjectSchema(p, fixedMap())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	obsidian := s.Tools["obsidian"]
	if obsidian == nil {
		t.Fatal("expected tool obsidian")
	}
	methods := SelectMethods(obsidian, s.Defaults.MethodOrder, "")
	if len(methods) != 2 {
		t.Fatalf("expected method_only to select 2 github candidates, got %d: %+v", len(methods), methods)
	}

	byLabel := map[string]*MethodCandidate{}
	for _, m := range methods {
		if m.Kind != "github" {
			t.Errorf("expected Kind=github for every candidate, got %q", m.Kind)
		}
		byLabel[m.Label] = m
	}

	apk := byLabel["gh_apk"]
	if apk == nil {
		t.Fatal("expected candidate labeled gh_apk")
	}
	if apk.When == nil || apk.When.IsAndroid == nil || *apk.When.IsAndroid != true {
		t.Errorf("gh_apk: expected when.is_android=true, got %+v", apk.When)
	}
	if apk.Config["asset"] != "obsidian-{version}-android.apk" {
		t.Errorf("gh_apk: unexpected asset config %v", apk.Config["asset"])
	}

	linux := byLabel["gh_linux"]
	if linux == nil {
		t.Fatal("expected candidate labeled gh_linux")
	}
	if linux.When == nil || len(linux.When.OS) != 1 || linux.When.OS[0] != "linux" {
		t.Errorf("gh_linux: expected when.os=[linux], got %+v", linux.When)
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

func TestValidateRejectsUnknownKindWithKnownFallback(t *testing.T) {
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
	_, err := Validate(s, []string{"native", "cargo"})
	if err == nil {
		t.Fatal("expected unknown declared method kind to be a hard error")
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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

func TestBucketExpansion_BucketWithFalseRejected(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
ruff = { python = false }`)
	if _, err := ParseProjectSchema(p, fixedMap()); err == nil {
		t.Fatal("expected python=false to fail strict parsing")
	}
}

// --- Per-tool method_prefer and method_only ---

func TestToolMethodPrefer(t *testing.T) {
	// method_prefer prepends to the default order without removing other methods.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_prefer = ["cargo"], cargo = true }
`)
	s, err := ParseProjectSchema(p, fixedMap())
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
}

func TestToolMethodOnly(t *testing.T) {
	// method_only restricts to only the listed methods, in that order.
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_only = ["cargo"], cargo = true }
`)
	s, err := ParseProjectSchema(p, fixedMap())
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

func TestToolPerToolMethodOrderRejected(t *testing.T) {
	p := writeSchema(t, `
[defaults]
manager = "native"

[tools]
myapp = { method_order = ["cargo"], cargo = true }
`)
	if _, err := ParseProjectSchema(p, fixedMap()); err == nil {
		t.Fatal("expected per-tool method_order to fail strict parsing")
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
		Name:         "test",
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

func TestSelectMethodsByKindAndLabel(t *testing.T) {
	methods := []*MethodCandidate{
		{Kind: "native"},
		{Kind: "github", Label: "gh_linux"},
		{Kind: "github", Label: "gh_apk"},
		{Kind: "http", Label: "http_musl"},
	}
	tests := []struct {
		name string
		tool *Tool
		want []string
	}{
		{
			name: "only kind",
			tool: &Tool{Methods: methods, MethodOnly: []string{"github"}},
			want: []string{"gh_apk", "gh_linux"},
		},
		{
			name: "only label",
			tool: &Tool{Methods: methods, MethodOnly: []string{"gh_apk"}},
			want: []string{"gh_apk"},
		},
		{
			name: "prefer label keeps fallbacks",
			tool: &Tool{Methods: methods, MethodPrefer: []string{"http_musl"}},
			want: []string{"http_musl", "native", "gh_apk", "gh_linux"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMethods := SelectMethods(tt.tool, []string{"native", "github", "http"}, "")
			got := make([]string, len(gotMethods))
			for i, method := range gotMethods {
				got[i] = methodName(method)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("SelectMethods() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSchemaStrictDiagnostics(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{"unknown condition", `[tools]\napp = { native = { when = { is_andriod = true } } }`, "tools.app.native.when.is_andriod"},
		{"tags type", `[tools]\napp = { native = true, tags = "desktop" }`, "tools.app.tags"},
		{"method only type", `[tools]\napp = { native = true, method_only = "native" }`, "tools.app.method_only"},
		{"tool when", `[tools]\napp = { native = true, when = { os = ["linux"] } }`, "tools.app.when"},
		{"orphan requires when", `[tools]\napp = { native = true, requires_when = { dep = { os = ["linux"] } } }`, "tools.app.requires_when.dep"},
		{"empty condition", `[tools]\napp = { native = { when = {} } }`, "condition must not be empty"},
		{"legacy preinstall", `[tools]\napp = { native = true, preinstall = "echo no" }`, "use pre_install"},
		{"legacy postinstall", `[tools]\napp = { native = true, postinstall = "echo no" }`, "use post_install"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseProjectSchema(writeSchema(t, strings.ReplaceAll(tt.schema, `\n`, "\n")), nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
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

func TestConditionDistroVersionsAcrossPlatforms(t *testing.T) {
	tests := []struct {
		name  string
		facts *engine.Facts
		cond  *Condition
		want  bool
	}{
		{"ubuntu exact", &engine.Facts{OS: "linux", DistroID: "ubuntu", DistroVersion: "24.04"}, &Condition{DistroID: []string{"ubuntu"}, DistroVersion: []string{"24.4"}}, true},
		{"ubuntu older rejected", &engine.Facts{OS: "linux", DistroID: "ubuntu", DistroVersion: "22.04"}, &Condition{DistroVersionMin: "24.04"}, false},
		{"fedora range", &engine.Facts{OS: "linux", DistroID: "fedora", DistroVersion: "42"}, &Condition{DistroID: []string{"fedora"}, DistroVersionMin: "41", DistroVersionMax: "43"}, true},
		{"macos major", &engine.Facts{OS: "darwin", DistroID: "macos", DistroVersion: "15.6.1"}, &Condition{OS: []string{"darwin"}, DistroVersionMin: "15", DistroVersionMax: "15.99"}, true},
		{"windows build range", &engine.Facts{OS: "windows", DistroID: "windows", DistroVersion: "10.0.26100.4652"}, &Condition{OS: []string{"windows"}, DistroVersionMin: "10.0.26100", DistroVersionMax: "10.0.26100.9999"}, true},
		{"windows future build rejected", &engine.Facts{OS: "windows", DistroID: "windows", DistroVersion: "10.0.26200"}, &Condition{DistroVersionMax: "10.0.26199"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cond.Match(tt.facts); got != tt.want {
				t.Fatalf("Match() = %v, want %v for facts=%+v condition=%+v", got, tt.want, tt.facts, tt.cond)
			}
		})
	}
}

func TestConditionMatches(t *testing.T) {
	// Build a baseline Facts that would match a debian system.
	facts := &engine.Facts{
		DistroID:      "ubuntu",
		DistroVersion: "24.04",
		DistroIDLike:  "debian",
		TargetFamily:  "unix",
		TargetArch:    "x86_64",
		OS:            "linux",
		Kernel:        "6.7.0-generic",
		Libc:          "glibc 2.35",
		InitSystem:    "systemd",
		IsWSL:         false,
		IsContainer:   false,
		IsAndroid:     false,
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
	// TargetFamily match
	cTF := &Condition{TargetFamily: []string{"unix"}}
	if !cTF.Match(facts) {
		t.Error("TargetFamily unix should match linux facts")
	}
	cTF2 := &Condition{TargetFamily: []string{"windows"}}
	if cTF2.Match(facts) {
		t.Error("TargetFamily windows should not match unix facts")
	}
	cTF3 := &Condition{TargetFamily: []string{"UNIX"}}
	if !cTF3.Match(facts) {
		t.Error("TargetFamily match should be case-insensitive")
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

	// Distro version exact/range matching. Comparisons are numeric/textual, not SemVer.
	if !(&Condition{DistroVersion: []string{"24.4"}}).Match(facts) {
		t.Error("distro_version 24.4 should match 24.04")
	}
	if (&Condition{DistroVersion: []string{"22.04"}}).Match(facts) {
		t.Error("distro_version 22.04 should not match 24.04")
	}
	if !(&Condition{DistroVersionMin: "22.04", DistroVersionMax: "24.04"}).Match(facts) {
		t.Error("24.04 should match inclusive distro version range 22.04..24.04")
	}
	if (&Condition{DistroVersionMin: "24.10"}).Match(facts) {
		t.Error("24.04 should not match distro_version_min 24.10")
	}
	if (&Condition{DistroVersionMax: "22.04"}).Match(facts) {
		t.Error("24.04 should not match distro_version_max 22.04")
	}
	if (&Condition{DistroVersionMin: "1"}).Match(&engine.Facts{DistroID: "ubuntu"}) {
		t.Error("missing distro version must not satisfy a version bound")
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
	c13 := &Condition{IsWSL: boolPtr(false)}
	if !c13.Match(facts) {
		t.Error("IsWSL=false should match facts.IsWSL=false")
	}

	// Three-state bools: IsContainer = true, facts.IsContainer = false → fail
	c14 := &Condition{IsContainer: boolPtr(true)}
	if c14.Match(facts) {
		t.Error("IsContainer=true should NOT match facts.IsContainer=false")
	}

	// Three-state bools: IsAndroid = false, facts.IsAndroid = false → OK
	c13b := &Condition{IsAndroid: boolPtr(false)}
	if !c13b.Match(facts) {
		t.Error("IsAndroid=false should match facts.IsAndroid=false")
	}

	// Three-state bools: IsAndroid = true, facts.IsAndroid = false → fail
	c14b := &Condition{IsAndroid: boolPtr(true)}
	if c14b.Match(facts) {
		t.Error("IsAndroid=true should NOT match facts.IsAndroid=false")
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
		{"target_family", &Condition{TargetFamily: []string{"unix"}}, false},
		{"distro_id", &Condition{DistroID: []string{"ubuntu"}}, false},
		{"arch", &Condition{Arch: []string{"x86_64"}}, false},
		{"os", &Condition{OS: []string{"linux"}}, false},
		{"kernel", &Condition{Kernel: []string{"6.7.0"}}, false},
		{"libc", &Condition{Libc: []string{"glibc"}}, false},
		{"init_system", &Condition{InitSystem: []string{"systemd"}}, false},
		{"is_wsl set", &Condition{IsWSL: boolPtr(true)}, false},
		{"is_container set", &Condition{IsContainer: boolPtr(false)}, false},
		{"is_android set", &Condition{IsAndroid: boolPtr(true)}, false},
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
		Name:         "test",
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
				Name:         "mytool",
				MethodPrefer: []string{"python"},
				Methods: []*MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "mytool"}},
					{Kind: "pip", Config: map[string]any{"pkg": "mytool"}},
					{Kind: "pipx", Config: map[string]any{"pkg": "mytool"}},
					{Kind: "uv", Config: map[string]any{"pkg": "mytool"}},
				},
			},
		},
	}
	warnings2, err2 := Validate(s2, []string{"native", "pip", "pipx", "uv"})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if len(warnings2) != 0 {
		t.Errorf("expected no warnings for bucket names in per-tool lists, got %v", warnings2)
	}
}

func TestValidateRejectsUnknownKindInMethodPrefer(t *testing.T) {
	// method_prefer with nonexistent kind should be a hard error
	s := &Schema{
		Defaults: Defaults{
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*Tool{
			"mytool": {
				Name:         "mytool",
				MethodPrefer: []string{"nonexistent"},
				Methods:      []*MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "mytool"}}},
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
				Name:       "mytool",
				MethodOnly: []string{"fakekind"},
				Methods:    []*MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "mytool"}}},
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
		{"is_android true", map[string]any{"is_android": true}, func(c *Condition) bool {
			return c.IsAndroid != nil && *c.IsAndroid == true
		}},
		{"is_android false", map[string]any{"is_android": false}, func(c *Condition) bool {
			return c.IsAndroid != nil && *c.IsAndroid == false
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
		"target_family": []any{"unix"},
		"libc":          []any{"glibc"},
		"init_system":   []any{"systemd"},
		"is_wsl":        false,
		"is_container":  false,
		"is_android":    true,
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
	if len(cond.TargetFamily) != 1 || cond.TargetFamily[0] != "unix" {
		t.Errorf("TargetFamily: expected [unix], got %v", cond.TargetFamily)
	}
	if cond.IsWSL == nil || *cond.IsWSL != false {
		t.Errorf("IsWSL: expected false, got %v", cond.IsWSL)
	}
	if cond.IsContainer == nil || *cond.IsContainer != false {
		t.Errorf("IsContainer: expected false, got %v", cond.IsContainer)
	}
	if cond.IsAndroid == nil || *cond.IsAndroid != true {
		t.Errorf("IsAndroid: expected true, got %v", cond.IsAndroid)
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
	s, err := ParseProjectSchema(p, fixedMap())
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
	s, err := ParseProjectSchema(p, fixedMap())
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

func TestBuildMethodsMarksImplicitNativeCandidate(t *testing.T) {
	methods := buildMethods("fzf", map[string]any{
		"go": "github.com/junegunn/fzf",
	})
	if len(methods) != 2 {
		t.Fatalf("buildMethods returned %d methods, want 2: %+v", len(methods), methods)
	}
	var nativeMethod, goMethod *MethodCandidate
	for _, method := range methods {
		switch method.Kind {
		case "native":
			nativeMethod = method
		case "go":
			goMethod = method
		}
	}
	if nativeMethod == nil || !nativeMethod.Inferred {
		t.Fatalf("implicit native candidate = %+v, want Inferred=true", nativeMethod)
	}
	if goMethod == nil || goMethod.Inferred {
		t.Fatalf("explicit go candidate = %+v, want Inferred=false", goMethod)
	}
}

func TestBuildMethodsMarksExplicitNativeCandidate(t *testing.T) {
	for name, values := range map[string]map[string]any{
		"native block":     {"native": map[string]any{"pkg": "fzf"}},
		"manager override": {"apt": "fzf"},
	} {
		t.Run(name, func(t *testing.T) {
			methods := buildMethods("fzf", values)
			if len(methods) != 1 || methods[0].Kind != "native" {
				t.Fatalf("methods = %+v, want one native candidate", methods)
			}
			if methods[0].Inferred {
				t.Fatalf("explicit native candidate unexpectedly marked inferred: %+v", methods[0])
			}
		})
	}
}

func TestNormalizeSimpleToolMarksCandidateInferred(t *testing.T) {
	tools, err := normalizeTools("", map[string]any{"simple": []any{"jq"}}, Defaults{Manager: "native"})
	if err != nil {
		t.Fatalf("normalizeTools: %v", err)
	}
	tool := tools["jq"]
	if tool == nil || len(tool.Methods) != 1 || !tool.Methods[0].Inferred {
		t.Fatalf("simple tool candidate = %+v, want one inferred method", tool)
	}
}

func TestParseProjectSchemaBindsProjectRootToMethods(t *testing.T) {
	path := writeSchema(t, `
[tools.demo.local]
local_path = "vendor/demo"
`)
	s, err := ParseProjectSchema(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Dir(path)
	if s.ProjectRoot != want {
		t.Fatalf("ProjectRoot = %q, want %q", s.ProjectRoot, want)
	}
	tool := s.Tools["demo"]
	if tool == nil || len(tool.Methods) != 1 {
		t.Fatalf("demo methods = %#v", tool)
	}
	if got := tool.Methods[0].ProjectRoot; got != want {
		t.Fatalf("method ProjectRoot = %q, want %q", got, want)
	}
}
