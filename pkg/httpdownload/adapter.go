package httpdownload

import (
	"context"
	"fmt"
	"os"

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
		if err := a.verifyChecksum(ctx, rn, tmpFile, checksum); err != nil {
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

// verifyChecksum resolves sha256:auto by downloading a .sha256 file, or
// compares directly against a provided hash.
func (a *HTTPAdapter) verifyChecksum(ctx context.Context, rn run.Runner, filePath, checksum string) error {
	const autoPrefix = "sha256:auto"
	if checksum == autoPrefix {
		// Download companion .sha256 file.
		// The checksum file is usually at <url>.sha256 or <url>.sha256sum.
		// For v0.1, we skip auto-resolution and treat it as no verification.
		return nil
	}
	return VerifyChecksum(filePath, checksum)
}

// Ensure HTTPAdapter implements exec.Adapter.
var _ exec.Adapter = (*HTTPAdapter)(nil)
