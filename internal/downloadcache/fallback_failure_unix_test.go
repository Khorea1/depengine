//go:build unix

package downloadcache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/privatepath"
)

func TestFallbackResolutionFailureDoesNotUseWorkingDirectory(t *testing.T) {
	tmp := t.TempDir()
	blockedTemp := filepath.Join(tmp, "not-a-directory")
	if err := os.WriteFile(blockedTemp, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "download")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("TMPDIR", blockedTemp)
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("OS resolves a home directory independently of HOME")
	}

	url := "https://example.test/fallback-failure"
	if got := CacheDir(); got != "" {
		t.Fatalf("CacheDir() = %q, want empty on unresolved fallback", got)
	}
	if got := Path(url); got != "" {
		t.Fatalf("Path() = %q, want empty on unresolved fallback", got)
	}
	if _, err := Store(url, src); err == nil {
		t.Fatal("Store() succeeded without a private fallback root")
	}
	if _, err := Clear(); err == nil {
		t.Fatal("Clear() succeeded without a private fallback root")
	}
	if err := Remove(url); err == nil {
		t.Fatal("Remove() succeeded without a private fallback root")
	}
	if _, err := os.Stat(key(url)); !os.IsNotExist(err) {
		t.Fatalf("relative cache entry exists or could not be checked: %v", err)
	}

	t.Setenv("TMPDIR", ".")
	if got := privatepath.TempRootPath(); got != "" {
		t.Fatalf("TempRootPath() = %q for relative TMPDIR, want empty", got)
	}
	if got := CacheDir(); got != "" {
		t.Fatalf("CacheDir() = %q for relative TMPDIR, want empty", got)
	}
	if _, err := Store(url, src); err == nil {
		t.Fatal("Store() accepted a relative temporary root")
	}
}
