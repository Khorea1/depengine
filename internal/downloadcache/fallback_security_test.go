package downloadcache

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookupRejectsInsecureCacheDirectoryAndEntry(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	url := "https://example.test/private-cache-check"
	path := Path(url)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := Lookup(url); got != "" {
			t.Fatalf("Lookup accepted entry in group/world-accessible directory: %q", got)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Lookup(url); got != "" {
			t.Fatalf("Lookup accepted group/world-readable cache entry: %q", got)
		}
	}
}

func TestStoreMakesCacheEntryOwnerOnly(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	src := filepath.Join(t.TempDir(), "download")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	url := "https://example.test/private-cache-store"
	path, err := Store(url, src)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("cache entry mode = %04o, want 0600", info.Mode().Perm())
		}
	}
	if got := Lookup(url); got != path {
		t.Fatalf("Lookup(%q) = %q, want %q", url, got, path)
	}
}

func TestCacheFallbackIsPerUserTempPath(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("OS resolves a home directory independently of HOME")
	}

	dir := CacheDir()
	rel, err := filepath.Rel(os.Getenv("TMPDIR"), dir)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 4 || parts[0] != "depengine-"+current.Uid || parts[1] != "cache" || parts[2] != "depengine" || parts[3] != "downloads" {
		t.Fatalf("cache fallback path is not isolated under a per-user temp root: %q", dir)
	}
	info, err := os.Stat(filepath.Join(os.Getenv("TMPDIR"), parts[0]))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("fallback root permissions = %04o, want owner-only", info.Mode().Perm())
	}
}
