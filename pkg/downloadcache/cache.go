// Package downloadcache provides a content-addressable disk cache for HTTP
// downloads. Downloaded files are stored by URL hash in XDG_CACHE_HOME so that
// repeated installations (or re-installation after removal) reuse the same
// .deb, .AppImage, or archive without re-downloading.
//
// Cache layout:
//
//	~/.cache/depengine/downloads/<sha256-of-url>
//
// Cache entries are evicted automatically: whenever a new entry is stored,
// if the total size of the cache exceeds maxCacheBytes (1 GiB by default,
// overridable via the DEPENGINE_CACHE_MAX_BYTES environment variable), the
// least-recently-used entries are removed until the cache is back under the
// limit. Recency is tracked by modification time, refreshed on both store
// and lookup, so frequently re-used artifacts are the last to go. Entries
// are also overwritten on re-download (which happens when a checksum
// mismatch is detected or when the caller requests a fresh download). For
// most schemas this means each artifact is downloaded exactly once.
package downloadcache

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// defaultCacheMaxBytes is the default maximum total size of the download
// cache before least-recently-used entries are evicted (1 GiB).
const defaultCacheMaxBytes int64 = 1 << 30

// maxCacheBytes returns the cache size limit in bytes. It honors the
// DEPENGINE_CACHE_MAX_BYTES environment variable; a value of 0 disables
// automatic eviction. Unset, empty, or unparseable values fall back to
// defaultCacheMaxBytes so a bad override can never break downloads.
func maxCacheBytes() int64 {
	if v := os.Getenv("DEPENGINE_CACHE_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return defaultCacheMaxBytes
}

// evict removes the least-recently-used regular files in dir (by
// modification time, oldest first) until the total size of the remaining
// files is at or below max. It is best-effort: files that fail to stat or
// remove are skipped, and it returns the number of files removed.
func evict(dir string, max int64) int {
	if max <= 0 {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0 // missing or unreadable cache: nothing to evict
	}
	type item struct {
		name string
		size int64
		mod  time.Time
	}
	items := make([]item, 0, len(entries))
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{name: e.Name(), size: info.Size(), mod: info.ModTime()})
		total += info.Size()
	}
	if total <= max {
		return 0
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	var removed int
	for _, it := range items {
		if total <= max {
			break
		}
		if err := os.Remove(filepath.Join(dir, it.name)); err != nil {
			continue // entry may already be gone; keep evicting
		}
		total -= it.size
		removed++
	}
	return removed
}

// CacheDir returns the download cache directory, respecting XDG_CACHE_HOME.
func CacheDir() string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Last resort fallback — should never happen.
			home = "/tmp"
		}
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "depengine", "downloads")
}

// key returns a hex-encoded SHA-256 hash of the URL, used as the cache filename.
func key(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h)
}

// Path returns the full cache path for a given URL.
func Path(url string) string {
	return filepath.Join(CacheDir(), key(url))
}

// Lookup returns the cached file path if a valid entry exists for url.
// It returns empty string when the cache has no entry for url. A hit
// refreshes the entry's modification time so eviction treats it as
// recently used.
func Lookup(url string) string {
	p := Path(url)
	if _, err := os.Stat(p); err == nil {
		now := time.Now()
		os.Chtimes(p, now, now) // best-effort LRU refresh
		return p
	}
	return ""
}

// Store moves src into the download cache under a key derived from url.
// The cache directory is created if necessary. If src and the cache live on
// different filesystems (cross-device rename), Store falls back to copy+remove.
// Returns the path of the cached file.
func Store(url, src string) (string, error) {
	dir := CacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("downloadcache: mkdir: %w", err)
	}

	dst := Path(url)
	now := time.Now()

	// Try rename first (fast, atomic within the same filesystem).
	if err := os.Rename(src, dst); err == nil {
		os.Chtimes(dst, now, now) // mark the fresh entry as most recently used
		evict(dir, maxCacheBytes())
		return dst, nil
	}

	// Cross-device or other failure — fall back to copy.
	if err := CopyFile(src, dst); err != nil {
		return "", fmt.Errorf("downloadcache: store: %w", err)
	}
	os.Remove(src) // best-effort cleanup
	os.Chtimes(dst, now, now)
	evict(dir, maxCacheBytes())
	return dst, nil
}

// Remove deletes a single cache entry for the given URL. No error is returned
// if the entry does not exist.
func Remove(url string) error {
	p := Path(url)
	err := os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Clear removes all cached download files. It does not remove the directory
// itself. Returns the number of files removed.
func Clear() (int, error) {
	dir := CacheDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("downloadcache: clear: %w", err)
	}
	var count int
	for _, e := range entries {
		if !e.IsDir() {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return count, fmt.Errorf("downloadcache: clear: remove %s: %w", e.Name(), err)
			}
			count++
		}
	}
	return count, nil
}

// CopyFile copies a file from src to dst, preserving permissions.
func CopyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer s.Close()

	fi, err := s.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	d, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}
