package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
)

// githubRedirectTripper is an http.RoundTripper that rewrites any request's
// scheme/host to point at a local httptest.Server while leaving the rest of
// the request (path, headers) untouched. This lets tests exercise the
// github.com-only code paths in GoDownloader.Download (User-Agent + token
// attachment) against a URL that satisfies ghrelease.IsGitHubURL without
// making a real network call.
type githubRedirectTripper struct {
	testURL string
}

func (r *githubRedirectTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.testURL)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestGoDownloaderDownloadSuccess(t *testing.T) {
	t.Parallel()
	const body = "hello from the mock CDN"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)

	dl := NewGoDownloader(&run.FakeRunner{})
	dest := filepath.Join(t.TempDir(), "out.bin")

	if err := dl.Download(context.Background(), ts.URL, dest); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != body {
		t.Fatalf("downloaded content = %q, want %q", got, body)
	}
}

func TestGoDownloaderDownloadNonOKStatus(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	dl := NewGoDownloader(&run.FakeRunner{})
	dest := filepath.Join(t.TempDir(), "out.bin")

	if err := dl.Download(context.Background(), ts.URL, dest); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

func TestGoDownloaderDownloadEmptyBody(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 OK with no body — should still be treated as a failed download.
	}))
	t.Cleanup(ts.Close)

	dl := NewGoDownloader(&run.FakeRunner{})
	dest := filepath.Join(t.TempDir(), "out.bin")

	err := dl.Download(context.Background(), ts.URL, dest)
	if err == nil {
		t.Fatal("expected error for empty response body, got nil")
	}
}

func TestGoDownloaderDownloadSetsUserAgent(t *testing.T) {
	t.Parallel()
	var gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte("ok"))
	}))
	t.Cleanup(ts.Close)

	dl := NewGoDownloader(&run.FakeRunner{})
	dest := filepath.Join(t.TempDir(), "out.bin")

	if err := dl.Download(context.Background(), ts.URL, dest); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}
	if gotUA == "" {
		t.Fatal("expected a non-empty User-Agent header; CDNs like GitHub releases and Cloudflare reject bare requests")
	}
	if gotUA != downloadUserAgent {
		t.Fatalf("User-Agent = %q, want %q", gotUA, downloadUserAgent)
	}
}

// TestGoDownloaderClientHasNoOverallTimeout is a regression test for the bug
// where http.Client{Timeout: 30 * time.Second} aborted any transfer —
// including a still-progressing one — past 30s, regardless of file size or
// network health. Deadline policy belongs to the caller's context, not a
// fixed field on the client.
func TestGoDownloaderClientHasNoOverallTimeout(t *testing.T) {
	t.Parallel()
	dl := NewGoDownloader(&run.FakeRunner{})
	if dl.client.Timeout != 0 {
		t.Fatalf("GoDownloader.client.Timeout = %v, want 0 (unset) — the whole-request timeout must come from ctx, not a fixed client field", dl.client.Timeout)
	}
}

func TestGoDownloaderDownloadAttachesGithubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-123")
	t.Setenv("GH_TOKEN", "")

	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte("ok"))
	}))
	t.Cleanup(ts.Close)

	dl := NewGoDownloader(&run.FakeRunner{})
	dl.client.Transport = &githubRedirectTripper{testURL: ts.URL}
	dest := filepath.Join(t.TempDir(), "out.bin")

	// A github.com URL with a rewritten Transport still satisfies
	// ghrelease.IsGitHubURL (it only inspects the URL, not the Transport),
	// so this exercises the real attach-token branch in Download.
	githubURL := "https://github.com/owner/private-repo/releases/download/v1.0/asset.tar.gz"
	if err := dl.Download(context.Background(), githubURL, dest); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}
	if gotAuth != "Bearer test-token-123" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-token-123")
	}
}

func TestGoDownloaderDownloadNoTokenForNonGithubHost(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-123")
	t.Setenv("GH_TOKEN", "")

	var authHeaderSeen bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderSeen = r.Header.Get("Authorization") != ""
		w.Write([]byte("ok"))
	}))
	t.Cleanup(ts.Close)

	dl := NewGoDownloader(&run.FakeRunner{})
	dest := filepath.Join(t.TempDir(), "out.bin")

	// ts.URL is a plain http://127.0.0.1:PORT URL, not github.com — the
	// GitHub token must never leak to an arbitrary third-party host.
	if err := dl.Download(context.Background(), ts.URL, dest); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}
	if authHeaderSeen {
		t.Fatal("Authorization header must not be sent to a non-GitHub host")
	}
}

func TestGoDownloaderDownloadNoTokenWithNilRunner(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-123")
	t.Setenv("GH_TOKEN", "")

	var authHeaderSeen bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderSeen = r.Header.Get("Authorization") != ""
		w.Write([]byte("ok"))
	}))
	t.Cleanup(ts.Close)

	// nil Runner must be safe (no panic) and simply skip token attachment —
	// exercised separately from the real-runner case above.
	dl := NewGoDownloader(nil)
	dl.client.Transport = &githubRedirectTripper{testURL: ts.URL}
	dest := filepath.Join(t.TempDir(), "out.bin")

	githubURL := "https://github.com/owner/private-repo/releases/download/v1.0/asset.tar.gz"
	if err := dl.Download(context.Background(), githubURL, dest); err != nil {
		t.Fatalf("unexpected Download error: %v", err)
	}
	if authHeaderSeen {
		t.Fatal("Authorization header must not be sent when Runner is nil")
	}
}

func TestSelectDownloaderGoDownloaderCarriesRunner(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1} // curl/wget both "not found" -> falls back to Go
	dl := SelectDownloader(context.Background(), fr)
	gd, ok := dl.(*GoDownloader)
	if !ok {
		t.Fatalf("expected GoDownloader, got %T", dl)
	}
	if gd.rn == nil {
		t.Fatal("SelectDownloader must pass the Runner through to GoDownloader so it can resolve a GitHub token")
	}
}
