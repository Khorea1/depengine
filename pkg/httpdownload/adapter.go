package httpdownload

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"depengine/pkg/downloadcache"
	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// HTTPAdapter implements exec.Adapter for HTTP(S) downloads.
// Supports archive extraction, checksum verification, and {latest}
// resolution via GitHub releases API.
type HTTPAdapter struct{}

// NewHTTPAdapter creates an HTTP download adapter.
func NewHTTPAdapter() *HTTPAdapter {
	return &HTTPAdapter{}
}

func (a *HTTPAdapter) Kind() string { return "http" }

// Available returns true — Go net/http is always available; curl/wget
// are detected lazily on actual download.
func (a *HTTPAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return true
}

// Check verifies if the tool appears already installed. Looks at:
//   - extract_to directory (must exist and have contents)
//   - binary in PATH (from config)
func (a *HTTPAdapter) Check(ctx context.Context, rn run.Runner, _ *schema.Tool, mc *schema.MethodCandidate) bool {
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		res := rn.Run(ctx, "test", "-d", extractTo)
		if res.Err == nil && res.ExitCode == 0 {
			// Directory exists and has contents.
			res2 := rn.Run(ctx, "ls", "-A", extractTo)
			return res2.Err == nil && res2.ExitCode == 0 && len(res2.Stdout) > 0
		}
	}
	if binary, ok := mc.Config["binary"].(string); ok && binary != "" {
		res := rn.Run(ctx, "which", binary)
		return res.Err == nil && res.ExitCode == 0
	}
	return false
}

// Install downloads a file from URL, optionally verifies its checksum,
// and extracts it based on file type.
func (a *HTTPAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	urlRaw, ok := mc.Config["url"].(string)
	if !ok || urlRaw == "" {
		return fmt.Errorf("http: no url configured for tool %q", tool.Name)
	}

	// Resolve {latest} in URL.
	resolvedURL, err := ResolveLatest(ctx, urlRaw)
	if err != nil {
		return fmt.Errorf("http: resolve latest: %w", err)
	}

	// Determine file extension.
	ext := fileExtension(resolvedURL)
	tmpDir, err := os.MkdirTemp("", "depengine-http-*")
	if err != nil {
		return fmt.Errorf("http: temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Determine the actual filename from the download URL.
	fileName := "download" + ext
	if parsedURL, err := url.Parse(resolvedURL); err == nil && parsedURL.Path != "" {
		if base := filepath.Base(parsedURL.Path); base != "" && base != "." && base != "/" {
			if filepath.Ext(base) == "" {
				base += ext
			}
			fileName = base
		}
	}
	tmpFile := tmpDir + "/" + fileName

	// --- Download cache ---
	// Check if the file is already cached by its resolved URL.
	cachedPath := downloadcache.Lookup(resolvedURL)
	fromCache := false

	if cachedPath != "" {
		// Copy the cached file to the temp location.
		if err := copyLocalFile(cachedPath, tmpFile); err != nil {
			// Cache read error is non-fatal — fall through to download.
			cachedPath = ""
		} else {
			fromCache = true
		}
	}

	if !fromCache {
		// Download from remote.
		dl := SelectDownloader(ctx, rn)
		if err := dl.Download(ctx, resolvedURL, tmpFile); err != nil {
			return fmt.Errorf("http: download %s: %w", tool.Name, err)
		}
	}

	// Verify checksum if configured.
	if checksum, ok := mc.Config["checksum"].(string); ok && checksum != "" {
		if err := a.verifyChecksum(ctx, rn, tmpFile, urlRaw, checksum); err != nil {
			// If we used a cached file and checksum fails, re-download fresh.
			if fromCache {
				fmt.Fprintf(os.Stderr, "  ⚠  cached copy failed checksum, re-downloading %s\n", tool.Name)
				downloadcache.Remove(resolvedURL)
				dl := SelectDownloader(ctx, rn)
				if err2 := dl.Download(ctx, resolvedURL, tmpFile); err2 != nil {
					return fmt.Errorf("http: download %s (re-download): %w", tool.Name, err2)
				}
				// Retry checksum verification on fresh download.
				if err2 := a.verifyChecksum(ctx, rn, tmpFile, urlRaw, checksum); err2 != nil {
					return fmt.Errorf("http: checksum: %w", err2)
				}
			} else {
				return fmt.Errorf("http: checksum: %w", err)
			}
		}
	}

	// Store in cache if we downloaded fresh (or update cache with verified file).
	if !fromCache {
		if _, err := downloadcache.Store(resolvedURL, tmpFile); err != nil {
			// Cache write failure is non-fatal; the install continues.
			fmt.Fprintf(os.Stderr, "  ⚠  cache write failed: %v\n", err)
		}
	} else {
		// When coming from cache, the file is already in the cache at cachedPath.
		// tmpFile is a copy; we need to keep tmpFile for extraction below.
	}

	// Determine extract destination.
	extractTo := "/usr/local/bin" // default
	if e, ok := mc.Config["extract_to"].(string); ok && e != "" {
		extractTo = e
	}
	// Check sudo policy for dpkg installs.
	sudoRequired := true
	if v, ok := mc.Config["sudo_required"].(bool); ok {
		sudoRequired = v
	}

	// Extract or copy.
	if err := Extract(ctx, tmpFile, extractTo, ext, rn, sudoRequired); err != nil {
		return fmt.Errorf("http: extract: %w", err)
	}

	return nil
}

// verifyChecksum resolves checksum verification. When "sha256:auto" is used,
// it downloads the companion .sha256 file (appending .sha256 to the URL),
// parses it, and verifies the downloaded file's hash.
func (a *HTTPAdapter) verifyChecksum(ctx context.Context, rn run.Runner, filePath, url, checksum string) error {
	const autoPrefix = "sha256:auto"
	if strings.HasPrefix(checksum, autoPrefix) {
		// Warn about TOFU — the checksum is downloaded from the same server as the file.
		fmt.Fprintf(os.Stderr, "  ⚠  sha256:auto: checksum is fetched from the same server as the download (TOFU). Consider pinning the explicit hash after first download.\n")
		// Try to download the companion .sha256 file.
		shaURL := url + ".sha256"
		tmpDir, err := os.MkdirTemp("", "depengine-checksum-*")
		if err != nil {
			return fmt.Errorf("auto-checksum: temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		shaFile := tmpDir + "/checksum.sha256"

		dl := SelectDownloader(ctx, rn)
		if err := dl.Download(ctx, shaURL, shaFile); err != nil {
			return fmt.Errorf("sha256:auto: downloading %s: %w", shaURL, err)
		}

		f, err := os.Open(shaFile)
		if err != nil {
			return fmt.Errorf("sha256:auto: open: %w", err)
		}
		defer f.Close()

		checksums, err := ParseChecksumFile(f)
		if err != nil {
			return fmt.Errorf("sha256:auto: parse: %w", err)
		}

		// Determine the expected filename from the downloaded file path.
		wantName := filepath.Base(filePath)
		expectedHash, ok := checksums[wantName]
		if !ok {
			return fmt.Errorf("sha256:auto: no checksum found for %q in %s", wantName, shaURL)
		}

		return VerifyChecksum(filePath, "sha256:"+expectedHash)
	}
	return VerifyChecksum(filePath, checksum)
}

// Ensure HTTPAdapter implements exec.Adapter.
var _ exec.Adapter = (*HTTPAdapter)(nil)

// copyLocalFile copies a file from src to dst, preserving permissions.
// Used by the download cache to materialize cached files into temp locations.
func copyLocalFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open cached file: %w", err)
	}
	defer s.Close()

	fi, err := s.Stat()
	if err != nil {
		return fmt.Errorf("stat cached file: %w", err)
	}

	d, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return fmt.Errorf("copy from cache: %w", err)
	}
	return d.Close()
}
