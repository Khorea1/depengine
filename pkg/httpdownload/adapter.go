package httpdownload

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/downloadcache"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
)

// HTTPAdapter implements exec.Adapter for HTTP(S) downloads.
// Supports archive extraction, checksum verification, and {latest}
// resolution via GitHub releases API.
type HTTPAdapter struct{}

// NewHTTPAdapter creates an HTTP download adapter.
func NewHTTPAdapter() *HTTPAdapter {
	return &HTTPAdapter{}
}

func init() {
	exec.Register(NewHTTPAdapter())
}

func (a *HTTPAdapter) Kind() string { return "http" }

// Available returns true — Go net/http is always available; curl/wget
// are detected lazily on actual download.
func (a *HTTPAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return true
}

// Check verifies if the tool appears already installed. The check is
// file-based, never directory-based:
//   - extract_to configured → the extracted TARGET FILE must exist inside it
//     (extract_to/<binary> when the install record names a binary, otherwise
//     extract_to/<tool name>). A bare directory is NOT proof of installation:
//     an unrelated operation (e.g. another tool's clone) can create the
//     directory while this tool's file was never downloaded.
//   - binary configured without extract_to → binary must be reachable on PATH.
func (a *HTTPAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	extractTo, _ := mc.Config["extract_to"].(string)
	binary, _ := mc.Config["binary"].(string)

	if extractTo != "" {
		target := binary
		if target == "" {
			if tool == nil {
				return false
			}
			target = tool.Name
		}
		res := rn.Run(ctx, "test", "-f", filepath.Join(extractTo, target))
		return res.Err == nil && res.ExitCode == 0
	}

	if binary != "" {
		res := rn.Run(ctx, "which", binary)
		return res.Err == nil && res.ExitCode == 0
	}
	return false
}

// Install downloads a file from URL, optionally verifies its checksum,
// and extracts it based on file type.
func (a *HTTPAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	urlRaw, ok := mc.Config["url"].(string)
	if !ok || urlRaw == "" {
		return fmt.Errorf("http: no url configured for tool %q", tool.Name)
	}

	// Resolve {latest} in URL.
	resolvedURL, err := ResolveLatest(ctx, urlRaw, rn)
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
		} else {
			fromCache = true
		}
	}

	if !fromCache {
		// Download from remote.
		dl := SelectDownloader(ctx, rn)
		if err := retryWithBackoff(ctx, 3, time.Second, 10*time.Second, func(retryCtx context.Context) error {
			return dl.Download(retryCtx, resolvedURL, tmpFile)
		}); err != nil {
			return fmt.Errorf("http: download %s: %w", tool.Name, err)
		}
	}

	// Verify checksum if configured.
	if checksum, ok := mc.Config["checksum"].(string); ok && checksum != "" {
		if err := a.verifyChecksum(ctx, rn, tmpFile, resolvedURL, checksum, mc.Config); err != nil {
			// If we used a cached file and checksum fails, re-download fresh.
			if fromCache {
				log.Default.Warn("cached copy failed checksum, re-downloading", "tool", tool.Name)
				downloadcache.Remove(resolvedURL)
				dl := SelectDownloader(ctx, rn)
				if err2 := retryWithBackoff(ctx, 3, time.Second, 10*time.Second, func(retryCtx context.Context) error {
					return dl.Download(retryCtx, resolvedURL, tmpFile)
				}); err2 != nil {
					return fmt.Errorf("http: download %s (re-download): %w", tool.Name, err2)
				}
				// Retry checksum verification on fresh download.
				if err2 := a.verifyChecksum(ctx, rn, tmpFile, resolvedURL, checksum, mc.Config); err2 != nil {
					return fmt.Errorf("http: checksum: %w", err2)
				}
			} else {
				return fmt.Errorf("http: checksum: %w", err)
			}
		}
	}

	// Extract the downloaded file before caching — Store uses os.Rename and
	// moves tmpFile away, so extraction must happen first.
	extractTo := "/usr/local/bin" // default
	if e, ok := mc.Config["extract_to"].(string); ok && e != "" {
		extractTo = e
	}
	// Check sudo policy for dpkg installs.
	sudoRequired := true
	if v, ok := mc.Config["sudo_required"].(bool); ok {
		sudoRequired = v
	}
	if err := Extract(ctx, tmpFile, extractTo, ext, rn, sudoRequired, tool.Name); err != nil {
		return fmt.Errorf("http: extract: %w", err)
	}

	// Store in cache after extraction (Store may move tmpFile via os.Rename).
	if !fromCache {
		if _, err := downloadcache.Store(resolvedURL, tmpFile); err != nil {
			// Cache write failure is non-fatal; the install continues.
			log.Default.Warn("cache write failed", "error", err, "url", resolvedURL)
		}
	} else {
		// Cache invalidation: we removed the old entry and re-downloaded.
		// Restock the cache with the freshly verified file.
		if _, err := downloadcache.Store(resolvedURL, tmpFile); err != nil {
			log.Default.Warn("cache write failed", "error", err, "url", resolvedURL)
		}
	}

	return nil
}

// checksumConfig holds parsed checksum-related configuration.
type checksumConfig struct {
	algorithm string // "sha256", "md5", "sha1", "sha512"
	url       string // explicit checksum URL from checksum_url config
	format    string // "sha256sum", "bsd", or "raw" from checksum_file_format config
}

// extractChecksumConfig extracts checksum-related config from a checksum string
// and method config.
func extractChecksumConfig(checksum string, config map[string]any) *checksumConfig {
	_, algorithm, err := parseChecksumPrefix(checksum)
	if err != nil {
		return nil
	}

	cc := &checksumConfig{algorithm: algorithm}
	if v, ok := config["checksum_url"].(string); ok {
		cc.url = v
	}
	if v, ok := config["checksum_file_format"].(string); ok {
		cc.format = v
	}
	return cc
}

// detectAlgorithmFromURL detects the checksum algorithm from a checksum URL
// filename. Returns empty string if no algorithm can be determined.
func detectAlgorithmFromURL(checksumURL string) string {
	base := strings.ToUpper(filepath.Base(checksumURL))
	switch {
	case strings.Contains(base, "SHA256"):
		return "sha256"
	case strings.Contains(base, "SHA512"):
		return "sha512"
	case strings.Contains(base, "SHA1"):
		return "sha1"
	case strings.Contains(base, "MD5"):
		return "md5"
	}
	return ""
}

// verifyChecksum resolves checksum verification. When the checksum string
// ends with ":auto", it tries to resolve the hash from a companion checksum
// file using config-driven URL and format options.
func (a *HTTPAdapter) verifyChecksum(ctx context.Context, rn run.Runner, filePath, downloadURL, checksum string, config map[string]any) error {
	// Handle :auto suffix — resolve checksum from a companion file.
	if strings.HasSuffix(checksum, ":auto") {
		cc := extractChecksumConfig(checksum, config)
		if cc == nil {
			return fmt.Errorf("http: checksum: invalid checksum format: %q", checksum)
		}
		return a.resolveAutoChecksum(ctx, rn, filePath, downloadURL, cc, config)
	}

	// Plain checksum — verify directly.
	return VerifyChecksum(filePath, checksum)
}

// resolveAutoChecksum handles :auto checksum resolution by trying to fetch
// a companion checksum file and extracting the expected hash.
func (a *HTTPAdapter) resolveAutoChecksum(ctx context.Context, rn run.Runner, filePath, downloadURL string, cc *checksumConfig, config map[string]any) error {
	log.Default.Warn("checksum fetched from server (TOFU)", "algorithm", cc.algorithm, "hint", "use checksum_url for a separate source, or pin the hash in depengine.lock")

	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("%s:auto: invalid download URL %q: %w", cc.algorithm, downloadURL, err)
	}
	wantName := filepath.Base(parsedURL.Path)
	if wantName == "" || wantName == "." || wantName == "/" {
		return fmt.Errorf("%s:auto: cannot determine filename from URL %q", cc.algorithm, downloadURL)
	}

	// Build the list of checksum URLs to try.
	var checksumURLs []string
	if cc.url != "" {
		checksumURLs = []string{cc.url}
	} else {
		// Try companion URL patterns.
		dir := ""
		if idx := strings.LastIndex(parsedURL.Path, "/"); idx >= 0 {
			dir = parsedURL.Path[:idx]
		}
		baseURL := parsedURL.Scheme + "://" + parsedURL.Host
		algoUpper := strings.ToUpper(cc.algorithm)
		checksumURLs = []string{
			downloadURL + "." + cc.algorithm,
			baseURL + dir + "/" + algoUpper + "SUMS",
			baseURL + dir + "/checksums.txt",
		}
	}

	var lastErr error
	for _, checksumURL := range checksumURLs {
		resolvedHash, err := a.fetchChecksumFromURL(ctx, rn, checksumURL, wantName, cc, config)
		if err != nil {
			lastErr = err
			continue
		}
		// Store resolved checksum in config so the lockfile mechanism
		// can capture the pinned hash later.
		if _, ok := config["_checksum_resolved"]; !ok {
			config["_checksum_resolved"] = cc.algorithm + ":" + resolvedHash
		}
		return VerifyChecksum(filePath, cc.algorithm+":"+resolvedHash)
	}
	return fmt.Errorf("%s:auto: could not resolve checksum: %w", cc.algorithm, lastErr)
}

// fetchChecksumFromURL downloads a checksum file from the given URL and
// extracts the hash for the wanted filename.
func (a *HTTPAdapter) fetchChecksumFromURL(ctx context.Context, rn run.Runner, checksumURL, wantName string, cc *checksumConfig, config map[string]any) (string, error) {
	tmpDir, err := os.MkdirTemp("", "depengine-checksum-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	checksumFile := tmpDir + "/checksum"
	dl := SelectDownloader(ctx, rn)
	if err := dl.Download(ctx, checksumURL, checksumFile); err != nil {
		return "", fmt.Errorf("downloading %s: %w", checksumURL, err)
	}

	// --- GPG signature verification of checksum file ---
	if sigURL, ok := config["signature_url"].(string); ok && sigURL != "" {
		signingKey, _ := config["signing_key"].(string)
		sigFile := tmpDir + "/checksum.sig"
		if err := dl.Download(ctx, sigURL, sigFile); err != nil {
			return "", fmt.Errorf("downloading signature %s: %w", sigURL, err)
		}
		if err := GPGVerify(ctx, rn, checksumFile, sigFile, signingKey); err != nil {
			return "", fmt.Errorf("gpg: %w", err)
		}
	}

	// If format is "raw", the entire file content is the hash.
	if cc.format == "raw" {
		data, err := os.ReadFile(checksumFile)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", checksumURL, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	f, err := os.Open(checksumFile)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var checksums map[string]string
	switch cc.format {
	case "bsd":
		checksums, err = ParseChecksumFileBSDExtended(f)
	case "sha256sum":
		checksums, err = ParseChecksumFile(f)
	default:
		checksums, err = ParseChecksumFileAuto(f)
	}
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", checksumURL, err)
	}

	hash, ok := checksums[wantName]
	if !ok {
		return "", fmt.Errorf("no checksum for %q in %s", wantName, checksumURL)
	}
	return hash, nil
}

// Ensure HTTPAdapter implements exec.Adapter.
var _ exec.Adapter = (*HTTPAdapter)(nil)

// copyLocalFile copies a file from src to dst, preserving permissions.
// Used by the download cache to materialize cached files into temp locations.
func copyLocalFile(src, dst string) error {
	return downloadcache.CopyFile(src, dst)
}
