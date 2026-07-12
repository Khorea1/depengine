package lock

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

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
			"ctpv/git":    {Latest: "v1.0.0"},
			"ff/http":     {Latest: "v2.1.0"},
			"other/git":   {Latest: "v0.5.0"},
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
	// Schema with no {latest} URLs — should return empty lock.
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
		},
	}

	l, err := ResolveAll(context.Background(), s)
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	if l == nil || l.Version != 1 {
		t.Fatalf("expected valid lock, got %+v", l)
	}
	if len(l.Tools) != 0 {
		t.Errorf("expected 0 pinned tools, got %d", len(l.Tools))
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
							"url": "https://github.com/user/fastfetch/releases/download/{latest}/ff.deb",
						},
					},
				},
			},
		},
	}

	l := &Lock{
		Version: 1,
		Tools: map[string]ToolPin{
			"ff/http": {Latest: "https://github.com/user/fastfetch/releases/download/v3.0.0/ff.deb"},
		},
	}

	Apply(s, l)

	mc := s.Tools["ff"].Methods[0]
	got := mc.Config["url"].(string)
	want := "https://github.com/user/fastfetch/releases/download/v3.0.0/ff.deb"
	if got != want {
		t.Fatalf("Apply URL = %q, want %q", got, want)
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

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
