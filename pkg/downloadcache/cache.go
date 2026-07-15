// Package downloadcache provides a content-addressable disk cache for HTTP
// downloads. Downloaded files are stored by URL hash in XDG_CACHE_HOME so that
// repeated installations (or re-installation after removal) reuse the same
// .deb, .AppImage, or archive without re-downloading.
//
// Cache layout:
//
//	~/.cache/depengine/downloads/<sha256-of-url>
//
// Cache entries are NOT automatically evicted — they are overwritten on
// re-download (which happens when a checksum mismatch is detected or when
// the caller requests a fresh download). For most schemas this means each
// artifact is downloaded exactly once.
package downloadcache

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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
// It returns empty string when the cache has no entry for url.
func Lookup(url string) string {
	p := Path(url)
	if _, err := os.Stat(p); err == nil {
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

	// Try rename first (fast, atomic within the same filesystem).
	if err := os.Rename(src, dst); err == nil {
		return dst, nil
	}

	// Cross-device or other failure — fall back to copy.
	if err := CopyFile(src, dst); err != nil {
		return "", fmt.Errorf("downloadcache: store: %w", err)
	}
	os.Remove(src) // best-effort cleanup
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
