package lock

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/containerregistry"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/pelletier/go-toml/v2"
)

func TestNPMLatestLockReplaysConcretePackageVersion(t *testing.T) {
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{
		"pkg": "@example/tool", "registry": "https://registry.example.test",
	}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": tool}}
	resolver := &run.FakeRunner{Stdout: `"1.2.3"`}
	fresh, err := ResolveLegacyV1(context.Background(), schema, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolver.Calls) != 1 || resolver.Calls[0].Name != "npm" || strings.Join(resolver.Calls[0].Args, " ") != "view @example/tool dist-tags.latest --json --registry https://registry.example.test" {
		t.Fatalf("resolution calls = %#v", resolver.Calls)
	}
	path := filepath.Join(t.TempDir(), "depengine.lock")
	if err := Save(path, fresh); err != nil {
		t.Fatal(err)
	}
	locked, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozen(schema, locked); err != nil {
		t.Fatalf("frozen validation: %v", err)
	}
	ApplyLegacyV1(schema, locked)
	if method.LockedVersion != "1.2.3" {
		t.Fatalf("locked version = %q", method.LockedVersion)
	}
	if _, present := method.Config["version"]; present {
		t.Fatal("lock rewrote requested version intent")
	}
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["npm"])
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{Err: os.ErrPermission}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Identity.Version != "1.2.3" {
		t.Fatalf("resolved version = %q", resolved.Identity.Version)
	}
	runner := &run.FakeRunner{LookPaths: map[string]bool{"npm": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.Calls {
		if call.Name == "npm" && len(call.Args) > 0 && call.Args[0] == "view" {
			t.Fatalf("install re-resolved latest: %#v", runner.Calls)
		}
	}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "npm" || strings.Join(last.Args, " ") != "install -g @example/tool@1.2.3 --registry https://registry.example.test" {
		t.Fatalf("install = %#v", last)
	}
	method.Config["registry"] = "https://other.example.test"
	if err := ValidateFrozen(schema, locked); err == nil || !strings.Contains(err.Error(), "package or registry changed") {
		t.Fatalf("registry drift error = %v", err)
	}
	method.Config["registry"] = "https://registry.example.test"
	method.Config["pkg"] = "@example/other"
	if err := ValidateFrozen(schema, locked); err == nil || !strings.Contains(err.Error(), "package or registry changed") {
		t.Fatalf("package drift error = %v", err)
	}
	method.Config["pkg"] = "@example/tool"
	method.Config["version"] = "1.2.3"
	if err := ValidateFrozen(schema, locked); err == nil || !strings.Contains(err.Error(), "package version request changed") {
		t.Fatalf("version intent drift error = %v", err)
	}
}
func TestPNPMLatestLockReplaysConcretePackageVersion(t *testing.T) {
	method := &config.MethodCandidate{Kind: "pnpm", Config: map[string]any{"pkg": "@example/tool"}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": tool}}
	resolver := &run.FakeRunner{Stdout: `"2.3.4"`}
	locked, err := ResolveLegacyV1(context.Background(), schema, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolver.Calls) != 1 || resolver.Calls[0].Name != "pnpm" || strings.Join(resolver.Calls[0].Args, " ") != "view @example/tool dist-tags.latest --json" {
		t.Fatalf("resolution calls = %#v", resolver.Calls)
	}
	if err := ValidateFrozen(schema, locked); err != nil {
		t.Fatalf("frozen validation: %v", err)
	}
	ApplyLegacyV1(schema, locked)
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["pnpm"])
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{Err: os.ErrPermission}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Identity.Version != "2.3.4" {
		t.Fatalf("resolved version = %q", resolved.Identity.Version)
	}
	runner := &run.FakeRunner{LookPaths: map[string]bool{"pnpm": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatal(err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "pnpm" || strings.Join(last.Args, " ") != "add -g @example/tool@2.3.4" {
		t.Fatalf("install = %#v", last)
	}
	method.Config["version"] = "2.3.4"
	if err := ValidateFrozen(schema, locked); err == nil || !strings.Contains(err.Error(), "package version request changed") {
		t.Fatalf("version intent drift error = %v", err)
	}
}
func TestYarnLatestLockReplaysConcretePackageVersion(t *testing.T) {
	method := &config.MethodCandidate{Kind: "yarn", Config: map[string]any{"pkg": "@example/tool"}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": tool}}
	resolver := &run.FakeRunner{Stdout: `{"type":"inspect","data":"3.4.5"}`}
	locked, err := ResolveLegacyV1(context.Background(), schema, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolver.Calls) != 1 || resolver.Calls[0].Name != "yarn" || strings.Join(resolver.Calls[0].Args, " ") != "info @example/tool version --json" {
		t.Fatalf("resolution calls = %#v", resolver.Calls)
	}
	if err := ValidateFrozen(schema, locked); err != nil {
		t.Fatalf("frozen validation: %v", err)
	}
	ApplyLegacyV1(schema, locked)
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["yarn"])
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{Err: os.ErrPermission}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	runner := &run.FakeRunner{LookPaths: map[string]bool{"yarn": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatal(err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "yarn" || strings.Join(last.Args, " ") != "global add @example/tool@3.4.5" {
		t.Fatalf("install = %#v", last)
	}
}

func TestNPMVersionPinRejectsMutableOrMalformedVersion(t *testing.T) {
	for _, version := range []string{"latest", "^1.2.3", "1.2.3 --prefix=/tmp", "01.2.3", "1.2.3-..", "1.2.3-01"} {
		if err := validatePackagePin(ToolPin{PackageVersion: version, PackageSelector: strings.Repeat("a", 64)}); err == nil {
			t.Errorf("accepted %q", version)
		}
	}
}

func TestDefaultPath(t *testing.T) {
	got := DefaultPath("/home/user/dotfiles/schema.toml")
	want := filepath.Join("/home/user/dotfiles", "depengine.lock")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}

	got2 := DefaultPath("schema.toml")
	want2 := "depengine.lock"
	if got2 != want2 {
		t.Fatalf("DefaultPath = %q, want %q", got2, want2)
	}

	got3 := DefaultPath("depends.toml")
	want3 := "depends.toml"
	if got3 == want3 {
		t.Fatalf("DefaultPath(depends.toml) should not equal input, got %q", got3)
	}

	got4 := DefaultPath("/tmp/foo.yaml")
	want4 := filepath.Join("/tmp", "depengine.lock")
	if got4 != want4 {
		t.Fatalf("DefaultPath(/tmp/foo.yaml) = %q, want %q", got4, want4)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "depengine.lock")

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"ctpv/git/0":        {Latest: "v1.0.0"},
			"ff/http/0":         {Latest: "v2.1.0"},
			"other/git/0":       {Latest: "v0.5.0"},
			"tool/http/0":       {Latest: "v3.0.0", Checksum: "sha256:abc123"},
			"image/container/0": {ContainerTag: "stable", ContainerDigest: "sha256:" + strings.Repeat("d", 64)},
		},
		MethodsHash: map[string]string{"tool": "method-hash"},
		SourceHash:  map[string]string{"tool/http/0": "source-hash"},
	}

	if err := Save(path, l); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.Tools["ctpv/git/0"].Latest != "v1.0.0" {
		t.Errorf("ctpv/git/0.Latest = %q, want v1.0.0", got.Tools["ctpv/git/0"].Latest)
	}
	if got.Tools["ff/http/0"].Latest != "v2.1.0" {
		t.Errorf("ff/http/0.Latest = %q, want v2.1.0", got.Tools["ff/http/0"].Latest)
	}
	if got.Tools["tool/http/0"].Checksum != "sha256:abc123" {
		t.Errorf("tool/http/0.Checksum = %q, want sha256:abc123", got.Tools["tool/http/0"].Checksum)
	}
	if pin := got.Tools["image/container/0"]; pin.ContainerTag != "stable" || pin.ContainerDigest != "sha256:"+strings.Repeat("d", 64) {
		t.Errorf("image/container/0 = %+v, want persisted tag+digest", pin)
	}
	if got.MethodsHash["tool"] != "method-hash" {
		t.Errorf("MethodsHash[tool] = %q, want method-hash", got.MethodsHash["tool"])
	}
	if got.SourceHash["tool/http/0"] != "source-hash" {
		t.Errorf("SourceHash[tool/http/0] = %q, want source-hash", got.SourceHash["tool/http/0"])
	}
}

func TestLoadMissingFileIsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.lock")

	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load of missing file should not error: %v", err)
	}
	if l != nil {
		t.Fatal("expected nil lock for missing file")
	}
}

func TestLoadRejectsNonCanonicalKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depengine.lock")
	data := []byte("version = 1\n[tools.'DepartureMono/http']\nlatest = 'v3.4.0'\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected obsolete lock key to be rejected")
	}
}

func TestResolveLegacyV1NoLatest(t *testing.T) {
	// Schema with no {latest} URLs — captures concrete checksums only.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"zsh": {
				Name: "zsh",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "zsh"}},
				},
			},
			"ctpv": {
				Name: "ctpv",
				Methods: []*config.MethodCandidate{
					{Kind: "git", Config: map[string]any{"url": "https://github.com/user/repo.git"}},
				},
			},
			"tool-with-checksum": {
				Name: "tool-with-checksum",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://example.com/tool.tar.gz",
							"checksum": "sha256:def456",
						},
					},
				},
			},
		},
	}

	l, err := ResolveLegacyV1(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}
	if l == nil || l.Version != 1 {
		t.Fatalf("expected valid lock, got %+v", l)
	}

	// No {latest} URLs, but tool-with-checksum has a concrete checksum.
	if got := len(l.Tools); got != 1 {
		t.Errorf("expected 1 pinned tool (checksum), got %d", got)
	}

	pin, ok := l.Tools["tool-with-checksum/http/0"]
	if !ok {
		t.Fatal("expected tool-with-checksum/http/0 to have a pin")
	}
	if pin.Checksum != "sha256:def456" {
		t.Errorf("tool-with-checksum/http/0.Checksum = %q, want sha256:def456", pin.Checksum)
	}
	if pin.Latest != "" {
		t.Errorf("expected no Latest, got %q", pin.Latest)
	}
}

func TestResolveLegacyV1PinsGitHubLatestReleases(t *testing.T) {
	original := resolveLatestReleaseTag
	resolveLatestReleaseTag = func(_ context.Context, repo string, _ run.Runner) (string, error) {
		return "v1.2.3-" + filepath.Base(repo), nil
	}
	t.Cleanup(func() { resolveLatestReleaseTag = original })

	s := &config.Schema{Tools: map[string]*config.Tool{
		"implicit": {Name: "implicit", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/implicit", "asset": "tool-{arch_any}"}}}},
		"latest":   {Name: "latest", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/latest", "asset": "tool-{arch_any}", "release": "latest"}}}},
		"release":  {Name: "release", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/release", "asset": "tool-{arch_any}", "release": "nightly"}}}},
		"branch":   {Name: "branch", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/branch", "asset": "tool-{arch_any}", "branch": "edge"}}}},
	}}

	l, err := ResolveLegacyV1(context.Background(), s, &run.FakeRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}
	if got := l.Tools["implicit/github/0"].Latest; got != "v1.2.3-implicit" {
		t.Errorf("implicit latest pin = %q", got)
	}
	if got := l.Tools["latest/github/0"].Latest; got != "v1.2.3-latest" {
		t.Errorf("explicit latest pin = %q", got)
	}
	if _, ok := l.Tools["release/github/0"]; ok {
		t.Error("named release must not be pinned as latest")
	}
	if _, ok := l.Tools["branch/github/0"]; ok {
		t.Error("named branch must not be pinned as latest")
	}
}

func TestReleasePinDispatchIsKindAgnostic(t *testing.T) {
	original := resolveLatestReleaseTag
	resolveLatestReleaseTag = func(_ context.Context, repo string, _ run.Runner) (string, error) {
		return "v9.9.9-" + filepath.Base(repo), nil
	}
	t.Cleanup(func() { resolveLatestReleaseTag = original })

	// An http method addressing its asset by repo must pin through the same
	// GitHub release path as a github method: dispatch keys on the repo
	// reference, never on the method kind.
	s := &config.Schema{Tools: map[string]*config.Tool{
		"httprepo": {Name: "httprepo", Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"repo": "owner/httprepo", "asset": "tool.tar.gz"}}}},
	}}
	l, err := ResolveLegacyV1(context.Background(), s, &run.FakeRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}
	if got := l.Tools["httprepo/http/0"].Latest; got != "v9.9.9-httprepo" {
		t.Fatalf("repo-backed http pin = %q, want v9.9.9-httprepo", got)
	}
	ApplyLegacyV1(s, l)
	if got := s.Tools["httprepo"].Methods[0].Config["release"]; got != "v9.9.9-httprepo" {
		t.Fatalf("applied release = %v, want v9.9.9-httprepo", got)
	}
}

func TestApplyPinsGitHubReleaseWithoutOverwritingExplicitRefs(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"implicit": {Name: "implicit", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/implicit", "asset": "tool.tar.gz"}}}},
		"latest":   {Name: "latest", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/latest", "asset": "tool.tar.gz", "release": "latest"}}}},
		"release":  {Name: "release", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/release", "asset": "tool.tar.gz", "release": "nightly"}}}},
		"branch":   {Name: "branch", Methods: []*config.MethodCandidate{{Kind: "github", Config: map[string]any{"repo": "owner/branch", "asset": "tool.tar.gz", "branch": "edge"}}}},
	}}
	l := &Lock{Version: 1, Tools: map[string]ToolPin{
		"implicit/github/0": {Latest: "v1.0.0"},
		"latest/github/0":   {Latest: "v2.0.0"},
		"release/github/0":  {Latest: "v3.0.0"},
		"branch/github/0":   {Latest: "v4.0.0"},
	}}

	ApplyLegacyV1(s, l)

	if got := s.Tools["implicit"].Methods[0].Config["release"]; got != "v1.0.0" {
		t.Errorf("implicit release = %v", got)
	}
	if got := s.Tools["latest"].Methods[0].Config["release"]; got != "v2.0.0" {
		t.Errorf("latest release = %v", got)
	}
	if got := s.Tools["release"].Methods[0].Config["release"]; got != "nightly" {
		t.Errorf("named release overwritten: %v", got)
	}
	if got := s.Tools["branch"].Methods[0].Config["branch"]; got != "edge" {
		t.Errorf("branch overwritten: %v", got)
	}
}

func TestApplyPinsURLs(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"ff": {
				Name: "ff",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://github.com/user/fastfetch/releases/download/{latest}/ff.deb",
							"checksum": "sha256:auto",
						},
					},
				},
			},
		},
	}

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"ff/http/0": {Latest: "v3.0.0", Checksum: "sha256:abc123"},
		},
	}

	ApplyLegacyV1(s, l)

	mc := s.Tools["ff"].Methods[0]
	got := mc.Config["url"].(string)
	want := "https://github.com/user/fastfetch/releases/download/v3.0.0/ff.deb"
	if got != want {
		t.Fatalf("Apply URL = %q, want %q", got, want)
	}
	gotChecksum := mc.Config["checksum"].(string)
	wantChecksum := "sha256:abc123"
	if gotChecksum != wantChecksum {
		t.Fatalf("Apply checksum = %q, want %q", gotChecksum, wantChecksum)
	}
}

func TestApplySkipsMethodsWithoutLockEntry(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"ff": {
				Name: "ff",
				Methods: []*config.MethodCandidate{
					{
						Kind: "git",
						Config: map[string]any{
							"url": "https://github.com/user/repo.git",
						},
					},
				},
			},
		},
	}

	// Lock has no entry for ff/git.
	l := &Lock{Version: 1, Tools: map[string]ToolPin{}}
	ApplyLegacyV1(s, l)

	mc := s.Tools["ff"].Methods[0]
	got := mc.Config["url"].(string)
	want := "https://github.com/user/repo.git"
	if got != want {
		t.Fatalf("URL was modified when it shouldn't have been: %q", got)
	}
}

func TestApplyChecksumOnlyPin(t *testing.T) {
	// Lock has a checksum pin but no Latest — only checksum should be applied.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://example.com/tool.tar.gz",
							"checksum": "sha256:auto",
						},
					},
				},
			},
		},
	}

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"tool/http/0": {Checksum: "sha256:f00bar"},
		},
	}

	ApplyLegacyV1(s, l)

	mc := s.Tools["tool"].Methods[0]
	// URL should be unchanged.
	gotURL := mc.Config["url"].(string)
	if gotURL != "https://example.com/tool.tar.gz" {
		t.Fatalf("URL was modified: %q", gotURL)
	}
	// Checksum should be pinned.
	gotChecksum := mc.Config["checksum"].(string)
	if gotChecksum != "sha256:f00bar" {
		t.Fatalf("Apply checksum = %q, want sha256:f00bar", gotChecksum)
	}
}

func TestSaveLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "depengine.lock")

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"test/git/0": {Latest: "v1.0.0"},
		},
	}

	if err := Save(path, l); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Verify file exists and is valid TOML.
	data, err := os.ReadFile(path) // #nosec G304 -- fixture-controlled path; no untrusted runtime input crosses this test boundary.
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte("v1.0.0")) {
		t.Errorf("TOML should contain pinned version, got:\n%s", data)
	}
}

func TestResolveLegacyV1CapturesChecksumResolved(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":                "https://example.com/tool.tar.gz",
							"checksum":           "sha256:auto",
							"_checksum_resolved": "sha256:resolved123",
						},
					},
				},
			},
		},
	}

	l, err := ResolveLegacyV1(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}
	if l == nil {
		t.Fatal("expected non-nil lock")
	}

	pin, ok := l.Tools["tool/http/0"]
	if !ok {
		t.Fatal("expected tool/http/0 to have a pin")
	}
	// Should prefer _checksum_resolved over checksum.
	if pin.Checksum != "sha256:resolved123" {
		t.Errorf("Checksum = %q, want sha256:resolved123", pin.Checksum)
	}
}

func TestResolveLegacyV1SkipsAutoChecksum(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://example.com/tool.tar.gz",
							"checksum": "sha256:auto",
						},
					},
				},
			},
		},
	}

	l, err := ResolveLegacyV1(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}

	// :auto checksums should NOT be captured as pins.
	if _, ok := l.Tools["tool/http/0"]; ok {
		t.Error("tool/http/0 should not have a pin when checksum is :auto")
	}
}

func TestChecksumPinRoundTrip(t *testing.T) {
	// Start with :auto checksum, apply a lock with concrete checksum,
	// verify the schema now holds the concrete hash.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://example.com/tool.tar.gz",
							"checksum": "sha256:auto",
						},
					},
				},
			},
		},
	}

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"tool/http/0": {Checksum: "sha256:pinned789"},
		},
	}

	ApplyLegacyV1(s, l)

	mc := s.Tools["tool"].Methods[0]
	got := mc.Config["checksum"].(string)
	if got != "sha256:pinned789" {
		t.Fatalf("checksum = %q, want sha256:pinned789", got)
	}
}

// TestApplySurvivesURLTemplateChange is the regression test for the bug this
// change fixes: previously Apply overwrote "url" wholesale with the fully
// resolved URL captured at `depengine update` time. If the schema.toml URL
// template was edited afterwards (fixed asset name, new arch suffix, etc.)
// WITHOUT re-running `depengine update`, that edit was silently discarded on
// the next `depengine install` — the stale, fully-baked URL from the lock
// won every time. Pinning only the bare tag and substituting it into
// whatever template is currently in the schema fixes this: the version stays
// reproducible, but template edits take effect immediately.
func TestApplySurvivesURLTemplateChange(t *testing.T) {
	// Lock was written when the schema pointed at "ff-linux.deb".
	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"ff/http/0": {Latest: "v3.0.0"},
		},
	}

	// schema.toml has SINCE been edited to a corrected asset name.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"ff": {
				Name: "ff",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url": "https://github.com/user/fastfetch/releases/download/{latest}/ff-linux-amd64.deb",
						},
					},
				},
			},
		},
	}

	ApplyLegacyV1(s, l)

	mc := s.Tools["ff"].Methods[0]
	got := mc.Config["url"].(string)
	want := "https://github.com/user/fastfetch/releases/download/v3.0.0/ff-linux-amd64.deb"
	if got != want {
		t.Fatalf("Apply URL = %q, want %q (template edit should be preserved, version should stay pinned)", got, want)
	}
}

func TestApplyDuplicateMethodKinds(t *testing.T) {
	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"tool/http/0": {Latest: "v1.0.0"},
			"tool/http/1": {Latest: "v2.0.0"},
		},
	}

	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url": "https://github.com/owner/repo-a/releases/download/{latest}/tool-a.deb",
						},
					},
					{
						Kind: "http",
						Config: map[string]any{
							"url": "https://github.com/owner/repo-b/releases/download/{latest}/tool-b.deb",
						},
					},
				},
			},
		},
	}

	ApplyLegacyV1(s, l)

	mc0 := s.Tools["tool"].Methods[0]
	got0 := mc0.Config["url"].(string)
	want0 := "https://github.com/owner/repo-a/releases/download/v1.0.0/tool-a.deb"
	if got0 != want0 {
		t.Fatalf("method[0] URL = %q, want %q", got0, want0)
	}

	mc1 := s.Tools["tool"].Methods[1]
	got1 := mc1.Config["url"].(string)
	want1 := "https://github.com/owner/repo-b/releases/download/v2.0.0/tool-b.deb"
	if got1 != want1 {
		t.Fatalf("method[1] URL = %q, want %q", got1, want1)
	}

	// Verify they got DIFFERENT tags (not the same tag leaked).
	if got0 == got1 {
		t.Errorf("methods got the same resolved URL; expected different tags: both = %q", got0)
	}
}

func TestResolveLegacyV1DuplicateMethodKinds(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://example.com/tool-a.tar.gz",
							"checksum": "sha256:aaaa1111",
						},
					},
					{
						Kind: "http",
						Config: map[string]any{
							"url":      "https://example.com/tool-b.tar.gz",
							"checksum": "sha256:bbbb2222",
						},
					},
				},
			},
		},
	}

	l, err := ResolveLegacyV1(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}

	// Both methods should have distinct lock entries.
	pin0, ok0 := l.Tools["tool/http/0"]
	if !ok0 {
		t.Fatal("expected pin for tool/http/0")
	}
	if pin0.Checksum != "sha256:aaaa1111" {
		t.Errorf("pin0.Checksum = %q, want sha256:aaaa1111", pin0.Checksum)
	}

	pin1, ok1 := l.Tools["tool/http/1"]
	if !ok1 {
		t.Fatal("expected pin for tool/http/1")
	}
	if pin1.Checksum != "sha256:bbbb2222" {
		t.Errorf("pin1.Checksum = %q, want sha256:bbbb2222", pin1.Checksum)
	}

	// Verify they are different entries.
	if pin0.Checksum == pin1.Checksum {
		t.Error("both methods got the same checksum; expected different values")
	}
}

func TestMethodHashRoundTrip(t *testing.T) {
	// Verify that ResolveLegacyV1 populates MethodsHash for tools with methods,
	// and that ApplyLegacyV1 can read it without warning when the hash matches.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{Kind: "http", Config: map[string]any{"url": "https://example.com/a.tar.gz", "checksum": "sha256:aaaa"}},
					{Kind: "git", Config: map[string]any{"url": "https://example.com/b.git"}},
				},
			},
		},
	}

	l, err := ResolveLegacyV1(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}

	// MethodsHash should be populated.
	h := l.MethodsHash["tool"]
	if h == "" {
		t.Fatal("MethodsHash should not be empty after ResolveLegacyV1")
	}

	// Verify hash is deterministic.
	expected := computeMethodsHash(s.Tools["tool"].Methods)
	if h != expected {
		t.Fatalf("MethodsHash = %q, want %q", h, expected)
	}

	// Apply with matching hash should not warn.
	saved := log.Default
	capture := log.NewTestLogger(t)
	log.Default = capture.Logger
	defer func() { log.Default = saved }()

	ApplyLegacyV1(s, l)

	capture.AssertNotContains(t, "method ordering changed")
}

func TestSourceHashRoundTripAndFrozenDriftDetection(t *testing.T) {
	method := &config.MethodCandidate{
		Kind:   "native",
		Config: map[string]any{"pkg": "demo"},
		Sources: []config.Source{{
			Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git",
			SecretRef: &config.SecretReference{Provider: "env", Name: "CORP_TOKEN"},
		}},
	}
	s := &config.Schema{Tools: map[string]*config.Tool{
		"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}},
	}}
	l, err := ResolveLegacyV1(context.Background(), s, &run.FakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	key := "demo/native/0"
	if l.SourceHash[key] == "" {
		t.Fatalf("SourceHash[%q] is empty", key)
	}
	if err := ValidateFrozen(s, l); err != nil {
		t.Fatalf("ValidateFrozen() rejected matching source identity: %v", err)
	}

	// Authentication reference changes do not alter persisted source identity.
	method.Sources[0].SecretRef = &config.SecretReference{Provider: "env", Name: "OTHER_TOKEN"}
	if err := ValidateFrozen(s, l); err != nil {
		t.Fatalf("ValidateFrozen() treated secret reference as lock identity: %v", err)
	}

	method.Sources[0].URL = "https://mirror.test/vendor/tools.git"
	err = ValidateFrozen(s, l)
	if err == nil || !strings.Contains(err.Error(), "package sources changed") {
		t.Fatalf("ValidateFrozen() error = %v, want package-source drift", err)
	}
}

func TestValidateFrozenRejectsMissingSourceIdentity(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"demo": {
			Name: "demo",
			Methods: []*config.MethodCandidate{{
				Kind: "native", Config: map[string]any{"pkg": "demo"},
				Sources: []config.Source{{Kind: "apt-ppa", Name: "ppa:vendor/stable"}},
			}},
		},
	}}
	l := frozenTestLock(s, map[string]ToolPin{})
	delete(l.SourceHash, "demo/native/0")
	err := ValidateFrozen(s, l)
	if err == nil || !strings.Contains(err.Error(), "missing package-source identity") {
		t.Fatalf("ValidateFrozen() error = %v, want missing package-source identity", err)
	}
}

func TestValidateFrozenRejectsRemovedSourceIdentity(t *testing.T) {
	method := &config.MethodCandidate{
		Kind:    "native",
		Config:  map[string]any{"pkg": "demo"},
		Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"}},
	}
	s := &config.Schema{Tools: map[string]*config.Tool{
		"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}},
	}}
	l := frozenTestLock(s, map[string]ToolPin{})
	method.Sources = nil

	err := ValidateFrozen(s, l)
	if err == nil || !strings.Contains(err.Error(), "package sources changed") {
		t.Fatalf("ValidateFrozen() error = %v, want removed package-source drift", err)
	}
}

func TestApplyMethodReorderingWarning(t *testing.T) {
	// Create a lock with a known MethodsHash, then call Apply with a schema
	// whose methods are reordered (different kind sequence) and verify a warning.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"tool": {
				Name: "tool",
				Methods: []*config.MethodCandidate{
					{Kind: "http", Config: map[string]any{"url": "https://example.com/a.tar.gz"}},
					{Kind: "git", Config: map[string]any{"url": "https://example.com/b.git"}},
				},
			},
		},
	}

	// Create a lock with the correct hash for the schema.
	l, err := ResolveLegacyV1(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveLegacyV1: %v", err)
	}

	// Now reorder the schema methods.
	s.Tools["tool"].Methods[0], s.Tools["tool"].Methods[1] = s.Tools["tool"].Methods[1], s.Tools["tool"].Methods[0]

	// Capture log output.
	saved := log.Default
	capture := log.NewTestLogger(t)
	log.Default = capture.Logger
	defer func() { log.Default = saved }()

	ApplyLegacyV1(s, l)

	capture.AssertContains(t, "method ordering changed")
	capture.AssertContains(t, "tool")
}

func TestMethodHashDifferentTools(t *testing.T) {
	// Different method lists should produce different hashes.
	methodsA := []*config.MethodCandidate{
		{Kind: "native"},
		{Kind: "git"},
	}
	methodsB := []*config.MethodCandidate{
		{Kind: "git"},
		{Kind: "native"},
	}
	methodsC := []*config.MethodCandidate{
		{Kind: "native"},
		{Kind: "http"},
	}

	hashA := computeMethodsHash(methodsA)
	hashB := computeMethodsHash(methodsB)
	hashC := computeMethodsHash(methodsC)

	if hashA == hashB {
		t.Error("reordered same kinds should produce different hashes")
	}
	if hashA == hashC {
		t.Error("different kind sequences should produce different hashes")
	}
	if hashB == hashC {
		t.Error("different kind sequences should produce different hashes")
	}

	// Determinism check.
	if computeMethodsHash(methodsA) != hashA {
		t.Error("hash should be deterministic")
	}
}

func TestMethodHashDetectsSameKindReordering(t *testing.T) {
	// Two methods sharing a Kind ("http") but distinguished by Label, as
	// happens with mirrors (e.g. "http-musl" and "http-glibc"). Apply keys
	// pins by kind+index-within-kind, so swapping these two changes which
	// pin each one receives — the hash must change too, or the reordering
	// warning in Apply can never fire for this (most common) case.
	before := []*config.MethodCandidate{
		{Kind: "http", Label: "http-musl"},
		{Kind: "http", Label: "http-glibc"},
	}
	after := []*config.MethodCandidate{
		{Kind: "http", Label: "http-glibc"},
		{Kind: "http", Label: "http-musl"},
	}

	if computeMethodsHash(before) == computeMethodsHash(after) {
		t.Error("swapping same-kind methods with different labels should change the hash")
	}
}

func TestResolveLegacyV1PinsLocalArtifactContentWithoutAbsolutePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "demo"), []byte("payload"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	s := &config.Schema{
		ProjectRoot: root,
		Tools: map[string]*config.Tool{
			"demo": {
				Name: "demo",
				Methods: []*config.MethodCandidate{{
					Kind:        "local",
					ProjectRoot: root,
					Config:      map[string]any{"local_path": "vendor/demo"},
				}},
			},
		},
	}

	l, err := ResolveLegacyV1(context.Background(), s, &run.FakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	pin, ok := l.Tools["demo/local/0"]
	if !ok || pin.Checksum == "" {
		t.Fatalf("local pin = %#v, present=%t", pin, ok)
	}
	if filepath.IsAbs(pin.Checksum) || bytes.Contains([]byte(pin.Checksum), []byte(root)) {
		t.Fatalf("local pin leaked project root: %#v", pin)
	}

	ApplyLegacyV1(s, l)
	got, _ := s.Tools["demo"].Methods[0].Config["checksum"].(string)
	if got != pin.Checksum {
		t.Fatalf("applied checksum = %q, want %q", got, pin.Checksum)
	}
}

func TestApplyLocalPinDoesNotOverrideExplicitChecksum(t *testing.T) {
	explicit := "sha256:" + strings.Repeat("a", 64)
	locked := "sha256:" + strings.Repeat("b", 64)
	s := &config.Schema{Tools: map[string]*config.Tool{
		"demo": {
			Name: "demo",
			Methods: []*config.MethodCandidate{{
				Kind:   "local",
				Config: map[string]any{"local_path": "vendor/demo", "checksum": explicit},
			}},
		},
	}}
	ApplyLegacyV1(s, &Lock{Version: 1, Tools: map[string]ToolPin{"demo/local/0": {Checksum: locked}}})
	if got := s.Tools["demo"].Methods[0].Config["checksum"]; got != explicit {
		t.Fatalf("explicit checksum overridden: got %v want %s", got, explicit)
	}
}

func frozenTestLock(s *config.Schema, pins map[string]ToolPin) *Lock {
	l := &Lock{
		Version:     1,
		Tools:       pins,
		MethodsHash: make(map[string]string),
		SourceHash:  make(map[string]string),
	}
	for name, tool := range s.Tools {
		if tool != nil && len(tool.Methods) > 0 {
			l.MethodsHash[name] = computeMethodsHash(tool.Methods)
			kindCount := make(map[string]int)
			for _, method := range tool.Methods {
				idx := kindCount[method.Kind]
				kindCount[method.Kind] = idx + 1
				if sourceHash := computeSourceHash(method); sourceHash != "" {
					l.SourceHash[toolKey(name, method.Kind, idx)] = sourceHash
				}
			}
		}
	}
	return l
}

func TestValidateFrozenAcceptsCompleteSupportedCoverage(t *testing.T) {
	explicitChecksum := "sha256:" + strings.Repeat("a", 64)
	s := &config.Schema{Tools: map[string]*config.Tool{
		"repo": {
			Name: "repo",
			Methods: []*config.MethodCandidate{{
				Kind:   "appimage",
				Config: map[string]any{"repo": "owner/tool", "asset": "tool.AppImage"},
			}},
		},
		"url": {
			Name: "url",
			Methods: []*config.MethodCandidate{{
				Kind:   "http",
				Config: map[string]any{"url": "https://example.test/{latest}/tool.tar.gz"},
			}},
		},
		"local": {
			Name: "local",
			Methods: []*config.MethodCandidate{{
				Kind:   "local",
				Config: map[string]any{"local_path": "vendor/tool"},
			}},
		},
		"auto": {
			Name: "auto",
			Methods: []*config.MethodCandidate{{
				Kind:   "http",
				Config: map[string]any{"url": "https://example.test/tool.tar.gz", "checksum": "sha256:auto"},
			}},
		},
		"fixed": {
			Name: "fixed",
			Methods: []*config.MethodCandidate{{
				Kind:   "local",
				Config: map[string]any{"local_path": "vendor/fixed", "checksum": explicitChecksum},
			}},
		},
	}}
	l := frozenTestLock(s, map[string]ToolPin{
		"repo/appimage/0": {Latest: "v1.2.3"},
		"url/http/0":      {Latest: "v2.0.0"},
		"local/local/0":   {Checksum: "sha256:" + strings.Repeat("b", 64)},
		"auto/http/0":     {Checksum: "sha256:" + strings.Repeat("c", 64)},
	})
	if err := ValidateFrozen(s, l); err != nil {
		t.Fatalf("ValidateFrozen() error = %v", err)
	}
}

func TestValidateFrozenRejectsMissingSupportedPins(t *testing.T) {
	cases := []struct {
		name   string
		method *config.MethodCandidate
		want   string
	}{
		{
			name:   "repo-backed latest release",
			method: &config.MethodCandidate{Kind: "appimage", Config: map[string]any{"repo": "owner/tool", "asset": "tool.AppImage"}},
			want:   "missing resolved release pin",
		},
		{
			name:   "url latest placeholder",
			method: &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": "https://example.test/{latest}/tool.tar.gz"}},
			want:   "missing resolved release pin",
		},
		{
			name:   "implicit local checksum",
			method: &config.MethodCandidate{Kind: "local", Config: map[string]any{"local_path": "vendor/tool"}},
			want:   "missing resolved checksum pin",
		},
		{
			name:   "remote auto checksum",
			method: &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": "https://example.test/tool.tar.gz", "checksum": "sha256:auto"}},
			want:   "missing resolved checksum pin",
		},
		{
			name:   "direct git branch",
			method: &config.MethodCandidate{Kind: "git", Config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"}},
			want:   "missing resolved Git revision pin",
		},
		{
			name:   "cargo git tag",
			method: &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"git": "https://example.test/tool.git", "tag": "v1"}},
			want:   "missing resolved Git revision pin",
		},
		{
			name:   "container tag",
			method: &config.MethodCandidate{Kind: "container", Config: map[string]any{"manager": "docker", "source": "example/tool", "tag": "stable"}},
			want:   "missing resolved container digest pin",
		},
		{
			name:   "mutable npm package selector",
			method: &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "@example/tool"}},
			want:   "missing resolved package version",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &config.Schema{Tools: map[string]*config.Tool{
				"tool": {Name: "tool", Methods: []*config.MethodCandidate{tc.method}},
			}}
			l := frozenTestLock(s, map[string]ToolPin{})
			err := ValidateFrozen(s, l)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateFrozen() error = %v, want containing %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "tool/"+tc.method.Kind+"/0") {
				t.Fatalf("ValidateFrozen() error = %v, want canonical lock key", err)
			}
		})
	}
}

func TestValidateFrozenRejectsMethodIdentityDrift(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{
				{Kind: "http", Label: "http-primary", Config: map[string]any{"url": "https://example.test/tool.tar.gz"}},
				{Kind: "git", Label: "git-fallback", Config: map[string]any{"url": "https://example.test/tool.git"}},
			},
		},
	}}
	l := frozenTestLock(s, map[string]ToolPin{})
	s.Tools["tool"].Methods[0], s.Tools["tool"].Methods[1] = s.Tools["tool"].Methods[1], s.Tools["tool"].Methods[0]

	err := ValidateFrozen(s, l)
	if err == nil || !strings.Contains(err.Error(), "methods changed for tool") {
		t.Fatalf("ValidateFrozen() error = %v, want method drift", err)
	}
}

func TestValidateFrozenRejectsMethodLabelDrift(t *testing.T) {
	method := &config.MethodCandidate{Kind: "http", Label: "primary", Config: map[string]any{"url": "https://example.test/tool.tar.gz"}}
	s := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	l := frozenTestLock(s, map[string]ToolPin{})

	method.Label = "mirror"
	if err := ValidateFrozen(s, l); err == nil || !strings.Contains(err.Error(), "methods changed for tool") {
		t.Fatalf("ValidateFrozen() error = %v, want method label drift", err)
	}
}

func TestValidateFrozenAllowsSelectorsOutsideLegacyLockCoverage(t *testing.T) {
	explicitChecksum := "sha256:" + strings.Repeat("a", 64)
	s := &config.Schema{Tools: map[string]*config.Tool{
		"snap": {
			Name: "snap",
			Methods: []*config.MethodCandidate{{
				Kind:   "snap",
				Config: map[string]any{"pkg": "tool", "channel": "stable"},
			}},
		},
		"release": {
			Name: "release",
			Methods: []*config.MethodCandidate{{
				Kind:   "github",
				Config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "release": "v1.2.3"},
			}},
		},
		"branch": {
			Name: "branch",
			Methods: []*config.MethodCandidate{{
				Kind:   "github",
				Config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "branch": "edge"},
			}},
		},
		"local-fixed": {
			Name: "local-fixed",
			Methods: []*config.MethodCandidate{{
				Kind:   "local",
				Config: map[string]any{"local_path": "vendor/tool", "checksum": explicitChecksum},
			}},
		},
	}}
	if err := ValidateFrozen(s, frozenTestLock(s, map[string]ToolPin{})); err != nil {
		t.Fatalf("ValidateFrozen() rejected selector outside legacy lock coverage: %v", err)
	}
}

func TestValidateFrozenRejectsMissingMethodIdentity(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {
			Name: "tool",
			Methods: []*config.MethodCandidate{{
				Kind:   "native",
				Config: map[string]any{"pkg": "tool"},
			}},
		},
	}}
	l := &Lock{Version: 1, Tools: map[string]ToolPin{}, MethodsHash: map[string]string{}}
	err := ValidateFrozen(s, l)
	if err == nil || !strings.Contains(err.Error(), "missing method identity") {
		t.Fatalf("ValidateFrozen() error = %v, want missing method identity", err)
	}
}

func TestValidateFrozenReportsToolsDeterministically(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"z-tool": {Name: "z-tool", Methods: []*config.MethodCandidate{{Kind: "native"}}},
		"a-tool": {Name: "a-tool", Methods: []*config.MethodCandidate{{Kind: "native"}}},
	}}
	l := &Lock{Version: 1, Tools: map[string]ToolPin{}, MethodsHash: map[string]string{}}
	for i := 0; i < 20; i++ {
		err := ValidateFrozen(s, l)
		if err == nil || !strings.Contains(err.Error(), `tool "a-tool"`) {
			t.Fatalf("ValidateFrozen() error = %v, want deterministic a-tool first", err)
		}
	}
}

func TestValidateFrozenRejectsInvalidInputs(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{}}
	if err := ValidateFrozen(nil, &Lock{Version: 1}); err == nil {
		t.Fatal("ValidateFrozen(nil, lock) unexpectedly succeeded")
	}
	if err := ValidateFrozen(s, nil); err == nil {
		t.Fatal("ValidateFrozen(schema, nil) unexpectedly succeeded")
	}
	if err := ValidateFrozen(s, &Lock{Version: CurrentVersion}); err == nil || !strings.Contains(err.Error(), "universal projection is unavailable") {
		t.Fatalf("ValidateFrozen(current version without projection) error = %v", err)
	}

	invalidProjection := &Lock{Version: CurrentVersion, UniversalProjection: "not json"}
	if err := ValidateFrozen(s, invalidProjection); err == nil || !strings.Contains(err.Error(), "invalid universal projection") {
		t.Fatalf("ValidateFrozen(invalid projection) error = %v", err)
	}

	withNilTool := &config.Schema{Tools: map[string]*config.Tool{"tool": nil}}
	if err := ValidateFrozen(withNilTool, &Lock{Version: 1}); err == nil {
		t.Fatal("ValidateFrozen(nil tool) unexpectedly succeeded")
	}
	withNilMethod := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{nil}},
	}}
	if err := ValidateFrozen(withNilMethod, &Lock{Version: 1}); err == nil {
		t.Fatal("ValidateFrozen(nil method) unexpectedly succeeded")
	}
}

func frozenV2TestLock(t *testing.T, schema *config.Schema, names ...string) *Lock {
	t.Helper()
	methods, sources, err := SnapshotIntentMetadata(schema)
	if err != nil {
		t.Fatal(err)
	}
	plans := make([]plan.ResolvedInstallPlan, 0, len(names))
	for _, name := range names {
		tool := schema.Tools[name]
		resolved := plan.New(name, tool.Methods[0].Kind, true)
		resolved.Identity.Version = "1.0.0"
		plans = append(plans, resolved)
	}
	doc, err := plan.BuildLockDocument(plans)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := NewUniversal(doc, methods, sources)
	if err != nil {
		t.Fatal(err)
	}
	return frozen
}

func TestValidateFrozenV2AcceptsProjectionWithoutLegacyTools(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{{
			Kind: "http", Config: map[string]any{"url": "https://example.test/{latest}/tool.tar.gz"},
		}}},
	}}
	frozen := frozenV2TestLock(t, schema, "tool")
	if len(frozen.Tools) != 0 {
		t.Fatalf("legacy Tools = %#v, want empty", frozen.Tools)
	}
	if err := ValidateFrozen(schema, frozen); err != nil {
		t.Fatalf("ValidateFrozen() rejected v2 projection without legacy pins: %v", err)
	}
}

func TestValidateFrozenV2RejectsRequestedMethodAndSourceDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*config.MethodCandidate)
	}{
		{
			name: "method label",
			mutate: func(method *config.MethodCandidate) {
				method.Label = "changed-label"
			},
		},
		{
			name: "source identity",
			mutate: func(method *config.MethodCandidate) {
				method.Sources[0].URL = "https://example.test/changed.git"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := &config.MethodCandidate{
				Kind:    "http",
				Config:  map[string]any{"url": "https://example.test/tool.tar.gz"},
				Sources: []config.Source{{Kind: "brew", Name: "core", URL: "https://example.test/core.git"}},
			}
			schema := &config.Schema{Tools: map[string]*config.Tool{
				"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}},
			}}
			frozen := frozenV2TestLock(t, schema, "tool")
			tc.mutate(method)
			if err := ValidateFrozen(schema, frozen); err == nil {
				t.Fatal("ValidateFrozen() accepted changed requested identity")
			}
		})
	}
}

func TestValidateFrozenV2RejectsMissingProjectionEntry(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"selected": {Name: "selected", Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"url": "https://example.test/tool.tar.gz"}}}},
		"outside":  {Name: "outside", Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"url": "https://example.test/outside.tar.gz"}}}},
	}}
	frozen := frozenV2TestLock(t, schema, "outside")
	filtered := &config.Schema{Tools: map[string]*config.Tool{"selected": schema.Tools["selected"]}}
	if err := ValidateFrozen(filtered, frozen); err == nil {
		t.Fatal("ValidateFrozen() accepted a concrete candidate missing from the projection")
	}
}

func TestValidateFrozenV2AllowsExtraProjectionEntries(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"selected": {Name: "selected", Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"url": "https://example.test/tool.tar.gz"}}}},
		"outside":  {Name: "outside", Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"url": "https://example.test/outside.tar.gz"}}}},
	}}
	frozen := frozenV2TestLock(t, schema, "selected", "outside")
	filtered := &config.Schema{Tools: map[string]*config.Tool{"selected": schema.Tools["selected"]}}
	if err := ValidateFrozen(filtered, frozen); err != nil {
		t.Fatalf("ValidateFrozen() rejected extra lock entry outside effective schema: %v", err)
	}
}

func TestMergeNilInputs(t *testing.T) {
	if got := Merge(nil, nil); got != nil {
		t.Fatalf("Merge(nil, nil) = %#v, want nil", got)
	}

	existing := &Lock{
		Version:     1,
		Tools:       map[string]ToolPin{"a/http/0": {Latest: "v1.0.0"}},
		MethodsHash: map[string]string{"a": "hash-a"},
	}
	if got := Merge(existing, nil); got != existing {
		t.Fatalf("Merge(existing, nil) = %#v, want existing unchanged", got)
	}

	fresh := &Lock{
		Version:     1,
		Tools:       map[string]ToolPin{"b/http/0": {Latest: "v2.0.0"}},
		MethodsHash: map[string]string{"b": "hash-b"},
	}
	if got := Merge(nil, fresh); got != fresh {
		t.Fatalf("Merge(nil, fresh) = %#v, want fresh unchanged", got)
	}
}

func TestMergeFreshValueWins(t *testing.T) {
	existing := &Lock{Version: 1, Tools: map[string]ToolPin{
		"tool/http/0": {Latest: "v1.0.0", Checksum: "sha256:" + strings.Repeat("a", 64)},
	}}
	fresh := &Lock{Version: 1, Tools: map[string]ToolPin{
		"tool/http/0": {Latest: "v2.0.0", Checksum: "sha256:" + strings.Repeat("b", 64)},
	}, MethodsHash: map[string]string{"tool": "fresh-hash"}}

	got := Merge(existing, fresh)
	if got != fresh {
		t.Fatalf("Merge returned %p, want fresh %p", got, fresh)
	}
	pin := got.Tools["tool/http/0"]
	if pin.Latest != "v2.0.0" {
		t.Fatalf("Latest = %q, want fresh v2.0.0", pin.Latest)
	}
	if want := "sha256:" + strings.Repeat("b", 64); pin.Checksum != want {
		t.Fatalf("Checksum = %q, want fresh %q", pin.Checksum, want)
	}
	if old := existing.Tools["tool/http/0"]; old.Latest != "v1.0.0" || old.Checksum != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("Merge mutated existing: %#v", old)
	}
	if _, ok := got.MethodsHash["tool"]; !ok || got.MethodsHash["tool"] != "fresh-hash" {
		t.Fatalf("MethodsHash = %v, want fresh map passed through untouched", got.MethodsHash)
	}
}

func TestMergeKeepsExistingFieldWhenFreshEmpty(t *testing.T) {
	materialized := "sha256:" + strings.Repeat("c", 64)
	cases := []struct {
		name     string
		existing ToolPin
		fresh    ToolPin
		want     ToolPin
	}{
		{
			// The `:auto` regression: fresh re-resolves Latest but skips the
			// materialized checksum, so the old checksum must survive.
			name:     "fresh latest-only keeps materialized checksum",
			existing: ToolPin{Latest: "v1.2.3", Checksum: materialized},
			fresh:    ToolPin{Latest: "v2.0.0"},
			want:     ToolPin{Latest: "v2.0.0", Checksum: materialized},
		},
		{
			name:     "fresh checksum-only keeps existing latest",
			existing: ToolPin{Latest: "v1.2.3", Checksum: materialized},
			fresh:    ToolPin{Checksum: "sha256:" + strings.Repeat("d", 64)},
			want:     ToolPin{Latest: "v1.2.3", Checksum: "sha256:" + strings.Repeat("d", 64)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			existing := &Lock{Version: 1, Tools: map[string]ToolPin{"tool/http/0": tc.existing}}
			fresh := &Lock{Version: 1, Tools: map[string]ToolPin{"tool/http/0": tc.fresh}}

			got := Merge(existing, fresh)
			if pin := got.Tools["tool/http/0"]; pin != tc.want {
				t.Fatalf("merged pin = %#v, want %#v", pin, tc.want)
			}
		})
	}
}

func TestMergeCarriesToolsAbsentFromFresh(t *testing.T) {
	materialized := "sha256:" + strings.Repeat("c", 64)
	existing := &Lock{Version: 1,
		Tools: map[string]ToolPin{
			"outside/http/0": {Latest: "v9.9.9", Checksum: materialized},
			"auto/http/0":    {Checksum: materialized},
		},
		MethodsHash: map[string]string{"outside": "outside-hash"},
	}
	fresh := &Lock{Version: 1,
		Tools:       map[string]ToolPin{"profiled/http/0": {Checksum: "sha256:" + strings.Repeat("b", 64)}},
		MethodsHash: map[string]string{"profiled": "profiled-hash"},
	}

	got := Merge(existing, fresh)
	if pin := got.Tools["outside/http/0"]; pin != (ToolPin{Latest: "v9.9.9", Checksum: materialized}) {
		t.Fatalf("outside pin = %#v, want preserved wholesale", pin)
	}
	if pin := got.Tools["auto/http/0"]; pin != (ToolPin{Checksum: materialized}) {
		t.Fatalf("auto pin = %#v, want preserved wholesale", pin)
	}
	if _, ok := got.Tools["profiled/http/0"]; !ok {
		t.Fatal("fresh pin dropped by merge")
	}
	// MethodsHash is not merged: the old hash for the absent tool must be
	// carried by the caller's identity policy, never by Merge itself.
	if _, ok := got.MethodsHash["outside"]; ok {
		t.Fatalf("Merge leaked existing MethodsHash entries: %v", got.MethodsHash)
	}
}

func v2TestLockWithProjection(t *testing.T, tools map[string]ToolPin) *Lock {
	t.Helper()
	p := plan.New("demo", "http", true)
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}
	p.Identity.Version = "1.2.3"
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{p})
	if err != nil {
		t.Fatal(err)
	}
	l := &Lock{Version: 1, Tools: tools, MethodsHash: map[string]string{}, SourceHash: map[string]string{}}
	if err := l.SetProjection(doc); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestMergePreservesV2UniversalProjection(t *testing.T) {
	existing := v2TestLockWithProjection(t, map[string]ToolPin{
		"outside/http/0": {Latest: "v9.9.9"},
	})
	wantProjection := existing.UniversalProjection
	fresh := &Lock{
		Version:     1,
		Tools:       map[string]ToolPin{"profiled/http/0": {Latest: "v2.0.0"}},
		MethodsHash: map[string]string{"profiled": "fresh-hash"},
	}

	got := Merge(existing, fresh)
	if got.Version != CurrentVersion {
		t.Fatalf("merged version = %d, want %d (v2 downgrade)", got.Version, CurrentVersion)
	}
	if got.UniversalProjection != wantProjection {
		t.Fatal("merged universal projection was regenerated or dropped; want verbatim preservation")
	}
	if pin := got.Tools["outside/http/0"]; pin.Latest != "v9.9.9" {
		t.Fatalf("outside pin = %#v, want preserved for partial/profile scope", pin)
	}
	if pin := got.Tools["profiled/http/0"]; pin.Latest != "v2.0.0" {
		t.Fatalf("fresh pin = %#v, want kept", pin)
	}
}

func TestMergeKeepsFreshProjectionWhenPresent(t *testing.T) {
	existing := v2TestLockWithProjection(t, nil)
	fresh := v2TestLockWithProjection(t, nil)
	// Make fresh distinct by rebuilding with a different tool identity.
	other := plan.New("other", "http", true)
	other.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "9.9.9"}
	other.Identity.Version = "9.9.9"
	otherDoc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{other})
	if err != nil {
		t.Fatal(err)
	}
	otherData, err := plan.EncodeLockDocument(otherDoc)
	if err != nil {
		t.Fatal(err)
	}
	fresh.UniversalProjection = string(otherData)

	got := Merge(existing, fresh)
	if got.UniversalProjection != fresh.UniversalProjection {
		t.Fatal("Merge overwrote a fresh v2 projection with the existing one")
	}
	if got.Version != CurrentVersion {
		t.Fatalf("merged version = %d, want %d", got.Version, CurrentVersion)
	}
}

func TestResolveLegacyV1PinsMutableGitSelectors(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name   string
		method *config.MethodCandidate
		want   string
		stdout string
	}{
		{
			name:   "direct branch",
			method: &config.MethodCandidate{Kind: "git", Config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"}},
			want:   "branch:main",
			stdout: commit + "\trefs/heads/main\n",
		},
		{
			name:   "cargo annotated tag",
			method: &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"git": "https://example.test/tool.git", "tag": "v1"}},
			want:   "tag:v1",
			stdout: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/tags/v1\n" + commit + "\trefs/tags/v1^{}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := &config.Schema{Tools: map[string]*config.Tool{
				"tool": {Name: "tool", Methods: []*config.MethodCandidate{tc.method}},
			}}
			lk, err := ResolveLegacyV1(context.Background(), schema, &run.FakeRunner{Stdout: tc.stdout})
			if err != nil {
				t.Fatal(err)
			}
			pin := lk.Tools["tool/"+tc.method.Kind+"/0"]
			if pin.Selector != tc.want || pin.Revision != commit {
				t.Fatalf("pin = %+v, want selector %q revision %s", pin, tc.want, commit)
			}
		})
	}
}

func TestApplyMutableGitPinReusesCommitWithoutChangingSelector(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	method := &config.MethodCandidate{Kind: "git", Config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	ApplyLegacyV1(schema, &Lock{Version: 1, Tools: map[string]ToolPin{
		"tool/git/0": {Selector: "branch:main", Revision: commit},
	}})
	if method.LockedRevision != commit {
		t.Fatalf("LockedRevision = %q, want %q", method.LockedRevision, commit)
	}
	if got := method.Config["branch"]; got != "main" {
		t.Fatalf("branch = %#v, want original selector preserved", got)
	}
}

func TestValidateFrozenRejectsMutableGitSelectorDrift(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"git": "https://example.test/tool.git", "branch": "develop"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	lk := frozenTestLock(schema, map[string]ToolPin{
		"tool/cargo/0": {Selector: "branch:main", Revision: commit},
	})
	if err := ValidateFrozen(schema, lk); err == nil || !strings.Contains(err.Error(), "Git selector changed") {
		t.Fatalf("ValidateFrozen() error = %v, want selector drift", err)
	}
}

func TestMergeClearsRemovedMutableGitPin(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	existing := &Lock{Version: 1, Tools: map[string]ToolPin{
		"tool/git/0": {Selector: "branch:main", Revision: commit, Checksum: "sha256:abc"},
	}}
	fresh := &Lock{Version: 1, Tools: map[string]ToolPin{}, clearGitRevision: map[string]struct{}{"tool/git/0": {}}}
	merged := Merge(existing, fresh)
	pin := merged.Tools["tool/git/0"]
	if pin.Revision != "" || pin.Selector != "" || pin.Checksum != "sha256:abc" {
		t.Fatalf("merged pin = %+v, want only non-Git fields retained", pin)
	}
}

func TestLoadRejectsMalformedGitPin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depengine.lock")
	data := []byte("version = 1\n[tools.'tool/git/0']\nselector = 'branch:main'\nrevision = 'main'\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "40- or 64-hex") {
		t.Fatalf("Load() error = %v, want malformed revision rejection", err)
	}
}

func TestResolveLegacyV1PinsMutableContainerTag(t *testing.T) {
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	original := resolveContainerTagDigest
	resolveContainerTagDigest = func(_ context.Context, source, tag string, credentials *containerregistry.Credentials) (string, error) {
		if source != "registry.example.test/team/tool" || tag != "stable" {
			t.Fatalf("resolve args = (%q, %q)", source, tag)
		}
		if credentials != nil {
			t.Fatalf("unexpected credentials: %+v", credentials)
		}
		return digest, nil
	}
	t.Cleanup(func() { resolveContainerTagDigest = original })

	method := &config.MethodCandidate{Kind: "container", Config: map[string]any{
		"manager": "docker", "source": "registry.example.test/team/tool", "tag": "stable",
	}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	lk, err := ResolveLegacyV1(context.Background(), schema, &run.FakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	pin := lk.Tools["tool/container/0"]
	if pin.ContainerTag != "stable" || pin.ContainerDigest != digest {
		t.Fatalf("pin = %+v", pin)
	}
}

func TestResolveLegacyV1PinsImplicitLatestContainerTag(t *testing.T) {
	const digest = "sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	original := resolveContainerTagDigest
	resolveContainerTagDigest = func(_ context.Context, _, tag string, _ *containerregistry.Credentials) (string, error) {
		if tag != "latest" {
			t.Fatalf("tag = %q, want latest", tag)
		}
		return digest, nil
	}
	t.Cleanup(func() { resolveContainerTagDigest = original })

	method := &config.MethodCandidate{Kind: "container", Config: map[string]any{"manager": "podman", "source": "redis"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"redis": {Name: "redis", Methods: []*config.MethodCandidate{method}}}}
	lk, err := ResolveLegacyV1(context.Background(), schema, &run.FakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if pin := lk.Tools["redis/container/0"]; pin.ContainerTag != "latest" || pin.ContainerDigest != digest {
		t.Fatalf("pin = %+v", pin)
	}
}

func TestApplyMutableContainerPinReusesDigestWithoutChangingTag(t *testing.T) {
	const digest = "sha256:2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	method := &config.MethodCandidate{Kind: "container", Config: map[string]any{"manager": "docker", "source": "example/tool", "tag": "edge"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	ApplyLegacyV1(schema, &Lock{Version: 1, Tools: map[string]ToolPin{
		"tool/container/0": {ContainerTag: "edge", ContainerDigest: digest},
	}})
	if method.LockedDigest != digest {
		t.Fatalf("LockedDigest = %q, want %q", method.LockedDigest, digest)
	}
	if got := method.Config["tag"]; got != "edge" {
		t.Fatalf("tag = %#v, want original selector preserved", got)
	}
}

func TestValidateFrozenRejectsMutablePackageSelectorDrift(t *testing.T) {
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "@example/old"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	selector, ok := packageMutableSelector("tool", method)
	if !ok {
		t.Fatal("npm package was not recognized as a mutable selector")
	}
	l := frozenTestLock(schema, map[string]ToolPin{"tool/npm/0": {PackageSelector: selector, PackageVersion: "1.2.3"}})
	method.Config["pkg"] = "@example/new"

	if err := ValidateFrozen(schema, l); err == nil || !strings.Contains(err.Error(), "package or registry changed") {
		t.Fatalf("ValidateFrozen() error = %v, want package selector drift", err)
	}
}
func TestValidateFrozenRejectsMutableContainerTagDrift(t *testing.T) {
	const digest = "sha256:3123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	method := &config.MethodCandidate{Kind: "container", Config: map[string]any{"manager": "docker", "source": "example/tool", "tag": "develop"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	lk := frozenTestLock(schema, map[string]ToolPin{
		"tool/container/0": {ContainerTag: "main", ContainerDigest: digest},
	})
	if err := ValidateFrozen(schema, lk); err == nil || !strings.Contains(err.Error(), "container tag changed") {
		t.Fatalf("ValidateFrozen() error = %v, want tag drift", err)
	}
}

func TestValidateFrozenRequiresMutableContainerDigestPin(t *testing.T) {
	method := &config.MethodCandidate{Kind: "container", Config: map[string]any{"manager": "docker", "source": "example/tool"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	if err := ValidateFrozen(schema, frozenTestLock(schema, map[string]ToolPin{})); err == nil || !strings.Contains(err.Error(), "missing resolved container digest pin") {
		t.Fatalf("ValidateFrozen() error = %v, want missing digest pin", err)
	}
}

func TestMergeClearsRemovedMutableContainerPin(t *testing.T) {
	const digest = "sha256:4123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	existing := &Lock{Version: 1, Tools: map[string]ToolPin{
		"tool/container/0": {ContainerTag: "latest", ContainerDigest: digest, Checksum: "sha256:abc"},
	}}
	fresh := &Lock{Version: 1, Tools: map[string]ToolPin{}, clearContainerDigest: map[string]struct{}{"tool/container/0": {}}}
	merged := Merge(existing, fresh)
	pin := merged.Tools["tool/container/0"]
	if pin.ContainerTag != "" || pin.ContainerDigest != "" || pin.Checksum != "sha256:abc" {
		t.Fatalf("merged pin = %+v, want only non-container fields retained", pin)
	}
}

func TestLoadRejectsMalformedContainerPin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depengine.lock")
	data := []byte("version = 1\n[tools.'tool/container/0']\ncontainer_tag = 'latest'\ncontainer_digest = 'sha256:nope'\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "container digest") {
		t.Fatalf("Load() error = %v, want malformed container digest rejection", err)
	}
}

func TestResolveLegacyV1ContainerTagPassesEnvCredentialWithoutPersistingSecret(t *testing.T) {
	const (
		digest      = "sha256:5123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		secretValue = "registry-secret-value"
	)
	t.Setenv("DEPENGINE_CONTAINER_TEST_PASSWORD", secretValue)
	original := resolveContainerTagDigest
	resolveContainerTagDigest = func(_ context.Context, source, tag string, credentials *containerregistry.Credentials) (string, error) {
		if source != "registry.example.test/team/tool" || tag != "stable" {
			t.Fatalf("resolve args = (%q, %q)", source, tag)
		}
		if credentials == nil || credentials.Username != "ci-user" || credentials.Secret != secretValue {
			t.Fatalf("credentials = %#v", credentials)
		}
		return digest, nil
	}
	t.Cleanup(func() { resolveContainerTagDigest = original })

	method := &config.MethodCandidate{
		Kind: "container",
		Config: map[string]any{
			"manager": "docker", "source": "registry.example.test/team/tool", "tag": "stable", "auth_username": "ci-user",
		},
		SecretRef: &config.SecretReference{Provider: "env", Name: "DEPENGINE_CONTAINER_TEST_PASSWORD"},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	lk, err := ResolveLegacyV1(context.Background(), schema, &run.FakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	pin := lk.Tools["tool/container/0"]
	if pin.ContainerTag != "stable" || pin.ContainerDigest != digest {
		t.Fatalf("pin = %+v", pin)
	}
	encoded, err := toml.Marshal(lk)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretValue) {
		t.Fatal("lock serialization leaked registry secret")
	}
}

func TestValidateFrozenRejectsBrewTapRevisionDrift(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	method := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo"}, Sources: []config.Source{{Kind: "brew-tap", Name: "corp/tools", URL: "https://example.test/tools.git", Revision: revision}}}
	s := &config.Schema{Tools: map[string]*config.Tool{"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}}}}
	l := frozenTestLock(s, nil)
	method.Sources[0].Revision = "abcdef0123456789abcdef0123456789abcdef01"
	if err := ValidateFrozen(s, l); err == nil || !strings.Contains(err.Error(), "package sources changed") {
		t.Fatalf("ValidateFrozen() error = %v, want package sources changed", err)
	}
}

func TestApplyLegacyV1IgnoresV2ToolPins(t *testing.T) {
	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{"url": "https://example.test/{latest}/tool.tar.gz"}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}}}}
	lk := &Lock{Version: CurrentVersion, Tools: map[string]ToolPin{"tool/http/0": {Latest: "v9.9.9"}}}
	ApplyLegacyV1(schema, lk)
	if got := method.Config["url"]; got != "https://example.test/{latest}/tool.tar.gz" {
		t.Fatalf("v2 compatibility ToolPin changed schema URL to %v", got)
	}
}
