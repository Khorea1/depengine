package httpdownload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// Available checks for curl or wget, falling back to Go net/http
// (always available in Go binaries).
func (a *HTTPAdapter) Available(ctx context.Context, rn run.Runner) bool {
	// Go net/http is always available; curl/wget are nice-to-have.
	// We consider HTTP always available since Go stdlib is built-in.
	_ = SelectDownloader(ctx, rn) // warm up selection
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

	// Determine file extension and destination.
	ext := fileExtension(resolvedURL)
	tmpDir, err := os.MkdirTemp("", "depengine-http-*")
	if err != nil {
		return fmt.Errorf("http: temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := tmpDir + "/download" + ext

	// Select download backend and download.
	dl := SelectDownloader(ctx, rn)
	if err := dl.Download(ctx, resolvedURL, tmpFile); err != nil {
		return fmt.Errorf("http: download %s: %w", tool.Name, err)
	}
	// Verify checksum if configured.
	if checksum, ok := mc.Config["checksum"].(string); ok && checksum != "" {
		if err := a.verifyChecksum(ctx, rn, tmpFile, resolvedURL, checksum); err != nil {
			return fmt.Errorf("http: checksum: %w", err)
		}
	}

	// Determine extract destination.
	extractTo := "/usr/local/bin" // default
	if e, ok := mc.Config["extract_to"].(string); ok && e != "" {
		extractTo = e
	}

	// Extract or copy.
	if err := Extract(ctx, tmpFile, extractTo, ext, rn); err != nil {
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
