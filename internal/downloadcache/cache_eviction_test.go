package downloadcache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testEvictURL = "https://github.com/example/repo/releases/download/v1.0/example-linux-amd64.deb"

func TestMaxCacheBytesDefault(t *testing.T) {
	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "")
	if got := maxCacheBytes(); got != defaultCacheMaxBytes {
		t.Fatalf("maxCacheBytes = %d, want default %d", got, defaultCacheMaxBytes)
	}
}

func TestMaxCacheBytesEnv(t *testing.T) {
	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "4096")
	if got := maxCacheBytes(); got != 4096 {
		t.Fatalf("maxCacheBytes = %d, want 4096", got)
	}

	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "not-a-number")
	if got := maxCacheBytes(); got != defaultCacheMaxBytes {
		t.Fatalf("maxCacheBytes = %d for invalid env, want default %d", got, defaultCacheMaxBytes)
	}

	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "0")
	if got := maxCacheBytes(); got != 0 {
		t.Fatalf("maxCacheBytes = %d for env 0, want 0 (eviction disabled)", got)
	}
}

// seedEntry writes size bytes directly into the cache directory, bypassing
// Store so no eviction runs, and stamps it with the given mtime.
func seedEntry(t *testing.T, url string, size int, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(CacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Path(url)
	if err := os.WriteFile(p, bytes.Repeat([]byte{'x'}, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// cacheTotalSize sums the size of every regular file in the cache directory.
func cacheTotalSize(t *testing.T) int64 {
	t.Helper()
	var total int64
	entries, err := os.ReadDir(CacheDir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	return total
}

func TestStoreEvictsOldestBeyondLimit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "100")

	now := time.Now()
	oldest := testEvictURL + "#Evict-oldest"
	older := testEvictURL + "#Evict-older"
	recent := testEvictURL + "#Evict-recent"
	seedEntry(t, oldest, 40, now.Add(-3*time.Hour))
	seedEntry(t, older, 40, now.Add(-2*time.Hour))
	seedEntry(t, recent, 40, now.Add(-1*time.Hour))

	// Storing a 4th entry pushes the cache to 160 bytes, well past the
	// 100-byte limit: the two oldest entries must be evicted.
	fresh := testEvictURL + "#Evict-fresh"
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, bytes.Repeat([]byte{'y'}, 40), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Store(fresh, src); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if Lookup(oldest) != "" {
		t.Error("oldest entry should have been evicted")
	}
	if Lookup(older) != "" {
		t.Error("second-oldest entry should have been evicted")
	}
	if Lookup(recent) == "" {
		t.Error("recent entry should have survived eviction")
	}
	if Lookup(fresh) == "" {
		t.Error("freshly stored entry should survive eviction")
	}
	if total := cacheTotalSize(t); total > 100 {
		t.Errorf("cache size %d exceeds limit 100 after eviction", total)
	}
}

func TestNoEvictionUnderLimit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "1000")

	now := time.Now()
	a := testEvictURL + "#Under-a"
	b := testEvictURL + "#Under-b"
	seedEntry(t, a, 40, now.Add(-2*time.Hour))
	seedEntry(t, b, 40, now.Add(-1*time.Hour))

	c := testEvictURL + "#Under-c"
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, bytes.Repeat([]byte{'y'}, 40), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Store(c, src); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if Lookup(a) == "" || Lookup(b) == "" || Lookup(c) == "" {
		t.Error("no entries should be evicted while the cache is under the limit")
	}
}

func TestLookupRefreshesRecency(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DEPENGINE_CACHE_MAX_BYTES", "100")

	now := time.Now()
	accessed := testEvictURL + "#Lru-accessed"
	untouched := testEvictURL + "#Lru-untouched"
	seedEntry(t, accessed, 40, now.Add(-3*time.Hour))
	seedEntry(t, untouched, 40, now.Add(-2*time.Hour))

	// Accessing the old entry refreshes its mtime, making it the most
	// recently used entry; the untouched entry must be evicted instead.
	if Lookup(accessed) == "" {
		t.Fatal("seeded entry not found")
	}

	fresh := testEvictURL + "#Lru-fresh"
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, bytes.Repeat([]byte{'y'}, 40), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Store(fresh, src); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if Lookup(untouched) != "" {
		t.Error("untouched entry should have been evicted")
	}
	if Lookup(accessed) == "" {
		t.Error("recently looked-up entry should survive eviction")
	}
	if Lookup(fresh) == "" {
		t.Error("freshly stored entry should survive eviction")
	}
}
