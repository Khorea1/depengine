package downloadcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testURL = "https://github.com/example/repo/releases/download/v1.0/example-linux-amd64.deb"

func TestCacheDirDefault(t *testing.T) {
	// Ensure XDG_CACHE_HOME is not set for this test.
	os.Unsetenv("XDG_CACHE_HOME")
	dir := CacheDir()
	if !strings.Contains(dir, ".cache") {
		t.Fatalf("expected .cache in path, got %q", dir)
	}
	if !strings.Contains(dir, "depengine") {
		t.Fatalf("expected depengine in path, got %q", dir)
	}
	if !strings.Contains(dir, "downloads") {
		t.Fatalf("expected downloads in path, got %q", dir)
	}
}

func TestCacheDirRespectsXDG(t *testing.T) {
	os.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	defer os.Unsetenv("XDG_CACHE_HOME")
	dir := CacheDir()
	want := "/tmp/xdg-cache/depengine/downloads"
	if dir != want {
		t.Fatalf("CacheDir = %q, want %q", dir, want)
	}
}

func TestKeyDeterministic(t *testing.T) {
	a := key(testURL)
	b := key(testURL)
	if a != b {
		t.Fatalf("key not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars", len(a))
	}
}

func TestKeyDifferentInputs(t *testing.T) {
	a := key(testURL)
	b := key(testURL + "/")
	if a == b {
		t.Fatal("key should differ for different URLs")
	}
}

func TestPathEndsWithKey(t *testing.T) {
	k := key(testURL)
	p := Path(testURL)
	if !strings.HasSuffix(p, k) {
		t.Fatalf("Path %q should end with key %q", p, k)
	}
}

func TestLookupMissing(t *testing.T) {
	// URL that has never been cached.
	got := Lookup("https://example.com/nonexistent-file-" + t.Name())
	if got != "" {
		t.Fatalf("expected empty for missing cache entry, got %q", got)
	}
}

func TestStoreAndLookup(t *testing.T) {
	dir := t.TempDir()
	// Override cache dir to use temp dir so we don't pollute real cache.
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Unsetenv("XDG_CACHE_HOME")

	// Create a "downloaded" file.
	src := filepath.Join(dir, "downloaded-file.bin")
	content := "some binary content for " + t.Name()
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	url := testURL + "#StoreAndLookup"
	cachedPath, err := Store(url, src)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify Lookup finds it.
	found := Lookup(url)
	if found == "" {
		t.Fatal("Lookup returned empty after Store")
	}
	if found != cachedPath {
		t.Fatalf("Lookup path %q != Store path %q", found, cachedPath)
	}

	// Verify content survived.
	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("cached content = %q, want %q", string(data), content)
	}
}

func TestStoreOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Unsetenv("XDG_CACHE_HOME")

	url := testURL + "#Overwrite"

	// First store.
	src1 := filepath.Join(dir, "first.bin")
	os.WriteFile(src1, []byte("first"), 0o644)
	if _, err := Store(url, src1); err != nil {
		t.Fatal(err)
	}

	// Second store with different content.
	src2 := filepath.Join(dir, "second.bin")
	os.WriteFile(src2, []byte("second"), 0o644)
	if _, err := Store(url, src2); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(Path(url))
	if string(data) != "second" {
		t.Fatalf("expected overwritten content 'second', got %q", string(data))
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Unsetenv("XDG_CACHE_HOME")

	url := testURL + "#Remove"
	src := filepath.Join(dir, "src.bin")
	os.WriteFile(src, []byte("data"), 0o644)
	Store(url, src)

	if err := Remove(url); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if Lookup(url) != "" {
		t.Fatal("entry still exists after Remove")
	}

	// Remove on non-existent entry should not error.
	if err := Remove("https://example.com/never-cached"); err != nil {
		t.Fatalf("Remove on missing entry: %v", err)
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Unsetenv("XDG_CACHE_HOME")

	// Store two entries.
	for i, suffix := range []string{"a", "b"} {
		url := testURL + "#Clear" + suffix
		src := filepath.Join(dir, "src")
		os.WriteFile(src, []byte("data"), 0o644)
		Store(url, src)
		_ = i
	}

	count, err := Clear()
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 removed files, got %d", count)
	}

	// Second clear should be a no-op.
	count, err = Clear()
	if err != nil {
		t.Fatalf("Clear (empty): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 removed on empty cache, got %d", count)
	}
}

func TestClearNonExistentDir(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Unsetenv("XDG_CACHE_HOME")

	// Cache dir doesn't exist yet — Clear should be a no-op.
	count, err := Clear()
	if err != nil {
		t.Fatalf("Clear on non-existent dir: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestStoreCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Unsetenv("XDG_CACHE_HOME")

	// Remove the depengine/downloads subdir so Store must create it.
	os.RemoveAll(filepath.Join(dir, "depengine"))

	url := testURL + "#CreateDirs"
	src := filepath.Join(dir, "src.bin")
	os.WriteFile(src, []byte("data"), 0o644)

	if _, err := Store(url, src); err != nil {
		t.Fatalf("Store (create dirs): %v", err)
	}
	if Lookup(url) == "" {
		t.Fatal("entry missing after Store with dir creation")
	}
}
