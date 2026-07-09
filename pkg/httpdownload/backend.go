package httpdownload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"depengine/pkg/run"
)

// Downloader abstracts the HTTP download backend.
type Downloader interface {
	Download(ctx context.Context, url, dest string) error
}

// GoDownloader uses Go's net/http stdlib (always available in Go binaries).
type GoDownloader struct {
	client *http.Client
}

// NewGoDownloader creates a downloader using Go's net/http.
func NewGoDownloader() *GoDownloader {
	return &GoDownloader{client: &http.Client{}}
}

func (d *GoDownloader) Download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("http: request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http: %s returned %s", url, resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("http: create %s: %w", dest, err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("http: download %s: %w", url, err)
	}
	if written == 0 {
		return fmt.Errorf("http: empty response from %s", url)
	}

	return nil
}

// CurlDownloader uses `curl -fsSL -o {dest} {url}`.
type CurlDownloader struct {
	rn run.Runner
}

// NewCurlDownloader creates a downloader using curl.
func NewCurlDownloader(rn run.Runner) *CurlDownloader {
	return &CurlDownloader{rn: rn}
}

func (d *CurlDownloader) Download(ctx context.Context, url, dest string) error {
	res := d.rn.Run(ctx, "curl", "-fsSL", "-o", dest, url)
	if res.Err != nil {
		return fmt.Errorf("curl: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("curl: exited %d", res.ExitCode)
	}
	return nil
}

// WgetDownloader uses `wget -q -O {dest} {url}`.
type WgetDownloader struct {
	rn run.Runner
}

// NewWgetDownloader creates a downloader using wget.
func NewWgetDownloader(rn run.Runner) *WgetDownloader {
	return &WgetDownloader{rn: rn}
}

func (d *WgetDownloader) Download(ctx context.Context, url, dest string) error {
	res := d.rn.Run(ctx, "wget", "-q", "-O", dest, url)
	if res.Err != nil {
		return fmt.Errorf("wget: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("wget: exited %d", res.ExitCode)
	}
	return nil
}

// SelectDownloader returns the best available download backend.
// Go net/http is preferred (always available in Go binaries); curl
// and wget are fallbacks for edge cases.
func SelectDownloader(ctx context.Context, rn run.Runner) Downloader {
	// Try curl first (handles redirects, SSL, etc. well).
	if res := rn.Run(ctx, "which", "curl"); res.Err == nil && res.ExitCode == 0 {
		return NewCurlDownloader(rn)
	}
	// Fall back to wget.
	if res := rn.Run(ctx, "which", "wget"); res.Err == nil && res.ExitCode == 0 {
		return NewWgetDownloader(rn)
	}
	// Go net/http is always available.
	return NewGoDownloader()
}

// fileExtension returns a recognizable extension for the URL path.
func fileExtension(url string) string {
	url = strings.Split(url, "?")[0] // strip query params
	url = strings.Split(url, "#")[0] // strip fragment
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".zip", ".deb", ".tar"} {
		if strings.HasSuffix(url, ext) {
			return ext
		}
	}
	return "" // binary
}
