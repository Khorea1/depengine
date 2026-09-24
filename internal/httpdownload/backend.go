package httpdownload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Khorea1/depengine/internal/artifact"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/run"
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
// internal/exec's per-method timeout and propagated via ctx down to
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
	return d.download(ctx, url, dest, "")
}

// DownloadWithBearer downloads one request with a caller-supplied Bearer
// credential. The credential is attached only to this request and is never
// retained on GoDownloader or exposed through the Downloader interface.
func (d *GoDownloader) DownloadWithBearer(ctx context.Context, rawURL, dest, credential string) error {
	if credential == "" {
		return fmt.Errorf("http: empty Bearer credential")
	}
	if err := artifact.ValidateAuthenticatedURL(rawURL); err != nil {
		return fmt.Errorf("http: authenticated URL: %w", err)
	}
	return d.download(ctx, rawURL, dest, credential)
}

func (d *GoDownloader) download(ctx context.Context, url, dest, bearerCredential string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("http: request: %w", run.RedactError(err))
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
	if bearerCredential != "" {
		req.Header.Set("Authorization", "Bearer "+bearerCredential)
	} else if d.rn != nil && ghrelease.IsGitHubURL(url) {
		if token := ghrelease.GithubToken(ctx, d.rn); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	client := d.client
	if bearerCredential != "" {
		// CheckRedirect belongs to this request's client copy. This makes the
		// credential policy explicit without changing a shared client's state.
		clientCopy := *d.client
		priorCheckRedirect := clientCopy.CheckRedirect
		initialURL := req.URL
		clientCopy.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
			if !sameOrigin(initialURL, redirectReq.URL) {
				redirectReq.Header.Del("Authorization")
			}
			if priorCheckRedirect != nil {
				return priorCheckRedirect(redirectReq, via)
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		}
		client = &clientCopy
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http: get: %w", run.RedactError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		safeURL := run.RedactSensitiveText(url)
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("http: %s returned %s (hint: check this tool's arch_map/os_map — the upstream release asset may use a different spelling of arch/os than this machine's own)", safeURL, resp.Status)
		}
		return fmt.Errorf("http: %s returned %s", safeURL, resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("http: create %s: %w", dest, err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("http: download %s: %w", run.RedactSensitiveText(url), run.RedactError(err))
	}
	if written == 0 {
		return fmt.Errorf("http: empty response from %s", run.RedactSensitiveText(url))
	}

	return nil
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if value := u.Port(); value != "" {
			return value
		}
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		default:
			return ""
		}
	}
	return port(left) == port(right)
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

// SelectDownloader returns the best available download backend when no URL-
// specific capability is required. curl is preferred, then wget, with Go's
// net/http as the universal fallback.
func SelectDownloader(ctx context.Context, rn run.Runner) Downloader {
	if run.LookPath(ctx, rn, "curl") {
		return NewCurlDownloader(rn)
	}
	if run.LookPath(ctx, rn, "wget") {
		return NewWgetDownloader(rn)
	}
	return NewGoDownloader(rn)
}

// SelectDownloaderForURL chooses a backend that can satisfy the security
// requirements of a concrete URL. When a GitHub credential is available,
// downloads from github.com must use GoDownloader because it can attach the
// Authorization header in-process. Passing the token to curl/wget would expose
// it in argv/process listings, while choosing curl/wget without the header can
// silently turn an authenticated private-release request into an anonymous one.
func SelectDownloaderForURL(ctx context.Context, rn run.Runner, rawURL string) Downloader {
	if rn != nil && ghrelease.IsGitHubURL(rawURL) && ghrelease.GithubToken(ctx, rn) != "" {
		return NewGoDownloader(rn)
	}
	return SelectDownloader(ctx, rn)
}

// SelectDownloaderForAuthenticatedURL selects the in-process backend for a
// request that requires caller-supplied authentication. The credential itself
// is deliberately not part of backend selection.
func SelectDownloaderForAuthenticatedURL(rn run.Runner) Downloader {
	return NewGoDownloader(rn)
}

// downloadErrorWithHint appends the arch_map/os_map hint to a download
// error when it looks like a 404, for downloaders (curl, wget) that don't
// give us a typed status code the way GoDownloader does — we only have
// their stderr text to go on. GoDownloader already attaches the hint
// itself (see its precise resp.StatusCode check above), so this checks for
// that marker to avoid appending the hint twice.
func downloadErrorWithHint(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "arch_map/os_map") {
		return err // GoDownloader already attached the hint
	}
	if strings.Contains(msg, "404") {
		return fmt.Errorf("%w (hint: check this tool's arch_map/os_map — the upstream release asset may use a different spelling of arch/os than this machine's own)", err)
	}
	return err
}

// fileExtension returns a recognizable extension for the URL path.
func fileExtension(url string) string {
	url = strings.Split(url, "?")[0] // strip query params
	url = strings.Split(url, "#")[0] // strip fragment
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tgz", ".zip", ".deb", ".tar", ".bz2"} {
		if strings.HasSuffix(url, ext) {
			return ext
		}
	}
	return "" // binary
}
