package lock

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestDefaultPath(t *testing.T) {
	got := DefaultPath("/home/user/dotfiles/schema.toml")
	want := "/home/user/dotfiles/depengine.lock"
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
	want4 := "/tmp/depengine.lock"
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
			"ctpv/git":  {Latest: "v1.0.0"},
			"ff/http":   {Latest: "v2.1.0"},
			"other/git": {Latest: "v0.5.0"},
			"tool/http": {Latest: "v3.0.0", Checksum: "sha256:abc123"},
		},
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
	if got.Tools["ctpv/git"].Latest != "v1.0.0" {
		t.Errorf("ctpv/git.Latest = %q, want v1.0.0", got.Tools["ctpv/git"].Latest)
	}
	if got.Tools["ff/http"].Latest != "v2.1.0" {
		t.Errorf("ff/http.Latest = %q, want v2.1.0", got.Tools["ff/http"].Latest)
	}
	if got.Tools["tool/http"].Checksum != "sha256:abc123" {
		t.Errorf("tool/http.Checksum = %q, want sha256:abc123", got.Tools["tool/http"].Checksum)
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

func TestResolveAllNoLatest(t *testing.T) {
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

	l, err := ResolveAll(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
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

	Apply(s, l)

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
	Apply(s, l)

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

	Apply(s, l)

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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte("v1.0.0")) {
		t.Errorf("TOML should contain pinned version, got:\n%s", data)
	}
}

func TestResolveAllCapturesChecksumResolved(t *testing.T) {
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

	l, err := ResolveAll(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
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

func TestResolveAllSkipsAutoChecksum(t *testing.T) {
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

	l, err := ResolveAll(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
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

	Apply(s, l)

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

	Apply(s, l)

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

	Apply(s, l)

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

func TestResolveAllDuplicateMethodKinds(t *testing.T) {
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

	l, err := ResolveAll(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
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
	// Verify that ResolveAll populates MethodsHash for tools with methods,
	// and that Apply can read it without warning when the hash matches.
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

	l, err := ResolveAll(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	// MethodsHash should be populated.
	h := l.MethodsHash["tool"]
	if h == "" {
		t.Fatal("MethodsHash should not be empty after ResolveAll")
	}

	// Verify hash is deterministic.
	expected := computeMethodsHash(s.Tools["tool"].Methods)
	if h != expected {
		t.Fatalf("MethodsHash = %q, want %q", h, expected)
	}

	// Apply with matching hash should not warn.
	saved := log.Default
	cap := log.NewTestLogger(t)
	log.Default = cap.Logger
	defer func() { log.Default = saved }()

	Apply(s, l)

	cap.AssertNotContains(t, "method ordering changed")
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
	l, err := ResolveAll(context.Background(), s, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}

	// Now reorder the schema methods.
	s.Tools["tool"].Methods[0], s.Tools["tool"].Methods[1] = s.Tools["tool"].Methods[1], s.Tools["tool"].Methods[0]

	// Capture log output.
	saved := log.Default
	cap := log.NewTestLogger(t)
	log.Default = cap.Logger
	defer func() { log.Default = saved }()

	Apply(s, l)

	cap.AssertContains(t, "method ordering changed")
	cap.AssertContains(t, "tool")
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

