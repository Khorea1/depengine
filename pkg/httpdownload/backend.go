package httpdownload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Khorea1/depengine/pkg/ghrelease"
	"github.com/Khorea1/depengine/pkg/run"
)

// downloadUserAgent identifies depengine to CDNs (GitHub releases,
// Cloudflare, etc.) that reject requests with no User-Agent header at all.
// This is the exact string ghrelease sends on its GitHub API calls
// (see ghrelease.UserAgent), so the asset-download path and the
// {latest}-resolution path present consistently as the same client.
const downloadUserAgent = ghrelease.UserAgent

// Downloader abstracts the HTTP download backend.
type Downloader interface {
	Download(ctx context.Context, url, dest string) error
}

// GoDownloader uses Go's net/http stdlib (always available in Go binaries).
type GoDownloader struct {
	client *http.Client
	rn     run.Runner
}

// NewGoDownloader creates a downloader using Go's net/http.
//
// client.Timeout is deliberately left unset (zero value): http.Client.Timeout
// covers the ENTIRE request, including reading the response body, so a fixed
// value here would abort any asset (toolchain, large binary, tarball) that
// takes longer to transfer than that value — regardless of network speed or
// whether the transfer is still making progress. Deadline policy for the
// whole install (this download included) is already centralized in
// pkg/exec's per-method timeout and propagated via ctx down to
// http.NewRequestWithContext; that's the single source of truth. Only ctx
// cancellation/timeout bounds this request.
//
// rn is used to resolve a GitHub token (env vars, then `gh auth token`) for
// downloads of private-repo release assets — see Download. It is nil-safe:
// LookPath-style callers may pass nil, in which case no token is attempted.
func NewGoDownloader(rn run.Runner) *GoDownloader {
	return &GoDownloader{client: &http.Client{}, rn: rn}
}

func (d *GoDownloader) Download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("http: request: %w", err)
	}

	// Without a User-Agent, some CDNs (GitHub releases, Cloudflare, etc.)
	// reject the request outright with 403, even though the same host
	// happily serves ghrelease's API calls, which do set one.
	req.Header.Set("User-Agent", downloadUserAgent)

	// Attach a GitHub token for github.com asset downloads (e.g. private-repo
	// release assets), mirroring what ghrelease already does for the
	// releases-API call that resolves {latest}. This is Go-downloader-only
	// by design: the header lives in process memory here, whereas passing a
	// token to curl/wget would put it on the command line, visible to any
	// local user via `ps aux` / /proc/<pid>/cmdline.
	if d.rn != nil && ghrelease.IsGitHubURL(url) {
		if token := ghrelease.GithubToken(ctx, d.rn); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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
	return run.CheckResult(res, "curl")
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
	return run.CheckResult(res, "wget")
}

// SelectDownloader returns the best available download backend.
// curl is tried first (handles redirects, SSL, and progress display well);
// wget is the first fallback; Go net/http is the universal fallback
// (always available in Go binaries).
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
	return NewGoDownloader(rn)
}

// fileExtension returns a recognizable extension for the URL path.
func fileExtension(url string) string {
	url = strings.Split(url, "?")[0] // strip query params
	url = strings.Split(url, "#")[0] // strip fragment
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tgz", ".zip", ".deb", ".tar"} {
		if strings.HasSuffix(url, ext) {
			return ext
		}
	}
	return "" // binary
}
