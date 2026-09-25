package downloadcache

import (
	"os"
	"path/filepath"
	"runtime"
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
	want := filepath.Join("/tmp/xdg-cache", "depengine", "downloads")
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

func TestStoreTightensCacheDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs are not represented by Unix permission bits")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	cacheDir := CacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil { // #nosec G301 -- Fixture intentionally starts permissive to verify tightening.
		t.Fatal(err)
	}
	src := filepath.Join(dir, "download.bin")
	if err := os.WriteFile(src, []byte("private artifact"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Store(testURL+"#PrivateDir", src); err != nil {
		t.Fatalf("Store: %v", err)
	}
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("cache dir mode = %04o, want 0700", got)
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
	for _, suffix := range []string{"a", "b"} {
		url := testURL + "#Clear" + suffix
		src := filepath.Join(dir, "src")
		os.WriteFile(src, []byte("data"), 0o644)
		Store(url, src)
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

func TestCopyFileAtomicPreservesExistingEntryOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "cached")
	if err := os.WriteFile(dst, []byte("known-good"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Opening a directory succeeds on Unix, but copying bytes from it fails.
	// The failed staging copy must not truncate or replace the existing cache
	// entry. On platforms that reject opening the directory earlier, the same
	// preservation invariant still applies.
	if err := copyFileAtomic(t.TempDir(), dst); err == nil {
		t.Fatal("copyFileAtomic(directory, dst) = nil, want error")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "known-good" {
		t.Fatalf("existing cache entry changed after failed copy: %q", got)
	}

	matches, err := filepath.Glob(filepath.Join(dir, ".depengine-download-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("staging files leaked after failed copy: %v", matches)
	}
}

func TestCopyFileAtomicCommitsCompleteFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new-complete-content"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFileAtomic(src, dst); err != nil {
		t.Fatalf("copyFileAtomic: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-complete-content" {
		t.Fatalf("committed content = %q", got)
	}
}

func TestLookupRejectsNonRegularEntries(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	url := testURL + "#non-regular"
	p := Path(url)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Lookup(url); got != "" {
		t.Fatalf("Lookup(directory) = %q, want miss", got)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("not cache-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if got := Lookup(url); got != "" {
		t.Fatalf("Lookup(symlink) = %q, want miss", got)
	}
}

func TestEvictIgnoresStagingFiles(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := os.MkdirAll(CacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(CacheDir(), ".depengine-download-active")
	if err := os.WriteFile(staging, []byte("in-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	url := testURL + "#evict-real-entry"
	entry := Path(url)
	if err := os.WriteFile(entry, []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}

	if removed := evict(CacheDir(), 1); removed != 1 {
		t.Fatalf("evict removed %d finalized entries, want 1", removed)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging file was removed by eviction: %v", err)
	}
}

func TestClearIgnoresStagingFiles(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := os.MkdirAll(CacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(CacheDir(), ".depengine-download-active")
	if err := os.WriteFile(staging, []byte("in-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := Path(testURL + "#clear-real-entry")
	if err := os.WriteFile(entry, []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := Clear()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Clear removed %d cache entries, want 1", count)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging file was removed by Clear: %v", err)
	}
}

func TestCopyFilePreservesPermissionsWhenDestinationExists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("destination mode = %o, want 755", got)
	}
}

func TestCopyFileAtomicPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("executable"), 0o751); err != nil {
		t.Fatal(err)
	}

	if err := copyFileAtomic(src, dst); err != nil {
		t.Fatalf("copyFileAtomic: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o751 {
		t.Fatalf("destination mode = %o, want 751", got)
	}
}
