package httpdownload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
const (
	downloadUserAgent             = ghrelease.UserAgent
	defaultMaxArtifactBytes int64 = 4 << 30
)

// maxArtifactDownloadBytes is an ingress limit, independent of cache eviction.
// Invalid, zero, or negative overrides retain the safe default.
func maxArtifactDownloadBytes() int64 {
	if raw := os.Getenv("DEPENGINE_DOWNLOAD_MAX_BYTES"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxArtifactBytes
}

type downloadLimitWriter struct {
	w       io.Writer
	limit   int64
	written int64
}

func (w *downloadLimitWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.written
	if remaining <= 0 {
		return 0, fmt.Errorf("download exceeds %d-byte limit", w.limit)
	}
	if int64(len(p)) > remaining {
		n, err := w.w.Write(p[:int(remaining)])
		w.written += int64(n)
		if err != nil {
			return n, err
		}
		return n, fmt.Errorf("download exceeds %d-byte limit", w.limit)
	}
	n, err := w.w.Write(p)
	w.written += int64(n)
	return n, err
}

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

func (d *GoDownloader) download(ctx context.Context, url, dest, bearerCredential string) (retErr error) {
	created := false
	defer func() {
		if retErr != nil && created {
			_ = os.Remove(dest)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("http: request: %w", run.RedactError(err))
	}

	// Without a User-Agent, some CDNs (GitHub releases, Cloudflare, etc.)
	// reject the request outright with 403, even though the same host
	// happily serves ghrelease's API calls, which do set one.
	req.Header.Set("User-Agent", downloadUserAgent)

	// Attach either the caller-supplied typed credential or the existing
	// GitHub compatibility token. Any credential selected here must satisfy
	// the same protected-transport and redirect policy before the request is
	// allowed to leave the process.
	authorizationCredential := bearerCredential
	if authorizationCredential == "" && d.rn != nil && ghrelease.IsGitHubURL(url) {
		authorizationCredential = ghrelease.GithubToken(ctx, d.rn)
	}
	if authorizationCredential != "" {
		if err := artifact.ValidateAuthenticatedURL(url); err != nil {
			return fmt.Errorf("http: authenticated URL: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+authorizationCredential)
	}

	client := d.client
	if authorizationCredential != "" {
		// CheckRedirect belongs to this request's client copy. This makes the
		// credential policy explicit without changing a shared client's state.
		clientCopy := *d.client
		priorCheckRedirect := clientCopy.CheckRedirect
		initialURL := req.URL
		clientCopy.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
			if err := artifact.ValidateAuthenticatedURL(redirectReq.URL.String()); err != nil {
				return fmt.Errorf("authenticated redirect: %w", err)
			}
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{
			URL:        run.RedactSensitiveText(url),
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}
	limit := maxArtifactDownloadBytes()
	if resp.ContentLength > limit {
		return fmt.Errorf("http: response from %s exceeds %d-byte download limit", run.RedactSensitiveText(url), limit)
	}

	out, err := os.Create(dest) // #nosec G304 -- Downloader destinations are depengine-managed staging/install paths chosen by the caller.
	if err != nil {
		return fmt.Errorf("http: create %s: %w", dest, err)
	}
	created = true
	defer func() {
		_ = out.Close()
	}()

	limited := &downloadLimitWriter{w: out, limit: limit}
	written, err := io.Copy(limited, resp.Body)
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
