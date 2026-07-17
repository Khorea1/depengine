package lock

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func TestDefaultPath(t *testing.T) {
	got := DefaultPath("/home/user/dotfiles/schema.toml")
	want := "/home/user/dotfiles/schema.lock"
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}

	got2 := DefaultPath("schema.toml")
	want2 := "schema.lock"
	if got2 != want2 {
		t.Fatalf("DefaultPath = %q, want %q", got2, want2)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.lock")

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
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"zsh": {
				Name: "zsh",
				Methods: []*schema.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "zsh"}},
				},
			},
			"ctpv": {
				Name: "ctpv",
				Methods: []*schema.MethodCandidate{
					{Kind: "git", Config: map[string]any{"url": "https://github.com/user/repo.git"}},
				},
			},
			"tool-with-checksum": {
				Name: "tool-with-checksum",
				Methods: []*schema.MethodCandidate{
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

	pin, ok := l.Tools["tool-with-checksum/http"]
	if !ok {
		t.Fatal("expected tool-with-checksum/http to have a pin")
	}
	if pin.Checksum != "sha256:def456" {
		t.Errorf("tool-with-checksum/http.Checksum = %q, want sha256:def456", pin.Checksum)
	}
	if pin.Latest != "" {
		t.Errorf("expected no Latest, got %q", pin.Latest)
	}
}

func TestApplyPinsURLs(t *testing.T) {
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"ff": {
				Name: "ff",
				Methods: []*schema.MethodCandidate{
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
			"ff/http": {Latest: "https://github.com/user/fastfetch/releases/download/v3.0.0/ff.deb", Checksum: "sha256:abc123"},
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
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"ff": {
				Name: "ff",
				Methods: []*schema.MethodCandidate{
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
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"tool": {
				Name: "tool",
				Methods: []*schema.MethodCandidate{
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
			"tool/http": {Checksum: "sha256:f00bar"},
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
	path := filepath.Join(dir, "subdir", "schema.lock")

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"test/git": {Latest: "v1.0.0"},
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
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"tool": {
				Name: "tool",
				Methods: []*schema.MethodCandidate{
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

	pin, ok := l.Tools["tool/http"]
	if !ok {
		t.Fatal("expected tool/http to have a pin")
	}
	// Should prefer _checksum_resolved over checksum.
	if pin.Checksum != "sha256:resolved123" {
		t.Errorf("Checksum = %q, want sha256:resolved123", pin.Checksum)
	}
}

func TestResolveAllSkipsAutoChecksum(t *testing.T) {
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"tool": {
				Name: "tool",
				Methods: []*schema.MethodCandidate{
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
	if _, ok := l.Tools["tool/http"]; ok {
		t.Error("tool/http should not have a pin when checksum is :auto")
	}
}

func TestChecksumPinRoundTrip(t *testing.T) {
	// Start with :auto checksum, apply a lock with concrete checksum,
	// verify the schema now holds the concrete hash.
	s := &schema.Schema{
		Tools: map[string]*schema.Tool{
			"tool": {
				Name: "tool",
				Methods: []*schema.MethodCandidate{
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
			"tool/http": {Checksum: "sha256:pinned789"},
		},
	}

	Apply(s, l)

	mc := s.Tools["tool"].Methods[0]
	got := mc.Config["checksum"].(string)
	if got != "sha256:pinned789" {
		t.Fatalf("checksum = %q, want sha256:pinned789", got)
	}
}
