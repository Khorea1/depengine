package ghrelease

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
)

// redirectTripper is an http.RoundTripper that rewrites requests to a test
// server while preserving the original URL path and headers. Used to mock
// the GitHub API in tests without changing the production code path.
type redirectTripper struct {
	testURL string
}

func (r *redirectTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.testURL)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestResolveLatestNoPlaceholder(t *testing.T) {
	url := "https://github.com/user/repo/releases/download/v1.0/file.tar.gz"
	got, err := ResolveLatest(context.Background(), url, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != url {
		t.Fatalf("ResolveLatest(%q) = %q, want %q", url, got, url)
	}
}

func TestResolveLatestNonGitHub(t *testing.T) {
	url := "https://gitlab.com/user/repo/-/releases/{latest}/file.tar.gz"
	got, err := ResolveLatest(context.Background(), url, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "https://gitlab.com/user/repo/-/releases/latest/file.tar.gz"
	if got != expected {
		t.Fatalf("ResolveLatest = %q, want %q (fallback 'latest')", got, expected)
	}
}

func TestResolveLatestEmptyURL(t *testing.T) {
	got, err := ResolveLatest(context.Background(), "", run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error on empty URL: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestIsGitHubURL(t *testing.T) {
	if !IsGitHubURL("https://github.com/user/repo") {
		t.Fatal("should be GitHub URL")
	}
	if !IsGitHubURL("https://github.com/user/repo/releases/latest") {
		t.Fatal("should be GitHub URL")
	}
	if IsGitHubURL("https://gitlab.com/user/repo") {
		t.Fatal("should not be GitHub URL")
	}
}

func TestResolveLatestWithHTTPMock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "v1.2.3"}`))
	}))
	t.Cleanup(ts.Close)

	httpClientMu.Lock()
	origClient := httpClient
	httpClient = &http.Client{
		Transport: &redirectTripper{testURL: ts.URL},
	}
	httpClientMu.Unlock()
	t.Cleanup(func() {
		httpClientMu.Lock()
		httpClient = origClient
		httpClientMu.Unlock()
	})

	url := "https://github.com/mock-owner/mock-repo/releases/download/{latest}/file.tar.gz"
	got, err := ResolveLatest(context.Background(), url, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "https://github.com/mock-owner/mock-repo/releases/download/v1.2.3/file.tar.gz"
	if got != expected {
		t.Fatalf("ResolveLatest = %q, want %q", got, expected)
	}
}

func TestLookupReleaseHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	httpClientMu.Lock()
	origClient := httpClient
	httpClient = &http.Client{
		Transport: &redirectTripper{testURL: ts.URL},
	}
	httpClientMu.Unlock()
	t.Cleanup(func() {
		httpClientMu.Lock()
		httpClient = origClient
		httpClientMu.Unlock()
	})

	url := "https://github.com/error-owner/error-repo/releases/download/{latest}/file.tar.gz"
	_, err := ResolveLatest(context.Background(), url, run.OSExecRunner{})
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

func TestResolveLatestTagNonGitHub(t *testing.T) {
	url := "https://gitlab.com/user/repo/-/releases/{latest}/file.tar.gz"
	got, err := ResolveLatestTag(context.Background(), url, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "latest" {
		t.Fatalf("ResolveLatestTag = %q, want %q (fallback matches ResolveLatest's)", got, "latest")
	}
}

func TestResolveLatestTagWithHTTPMock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "v9.9.9"}`))
	}))
	t.Cleanup(ts.Close)

	httpClientMu.Lock()
	origClient := httpClient
	httpClient = &http.Client{
		Transport: &redirectTripper{testURL: ts.URL},
	}
	httpClientMu.Unlock()
	t.Cleanup(func() {
		httpClientMu.Lock()
		httpClient = origClient
		httpClientMu.Unlock()
	})

	// Distinct owner/repo from other tests so the shared cache can't mask a
	// broken implementation with a stale hit.
	url := "https://github.com/tag-owner/tag-repo/releases/download/{latest}/file.tar.gz"
	got, err := ResolveLatestTag(context.Background(), url, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v9.9.9" {
		t.Fatalf("ResolveLatestTag = %q, want bare tag %q (not a resolved URL)", got, "v9.9.9")
	}

	// The same tag, applied by the caller, must reproduce what ResolveLatest
	// would have returned directly — this is the invariant pkg/lock relies on.
	viaResolveLatest, err := ResolveLatest(context.Background(), url, run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	viaTagSubstitution := strings.ReplaceAll(url, "{latest}", got)
	if viaTagSubstitution != viaResolveLatest {
		t.Fatalf("manual substitution with ResolveLatestTag's result = %q, want %q (should match ResolveLatest)", viaTagSubstitution, viaResolveLatest)
	}
}

func TestResolveAssetURLLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/asset-owner/asset-repo/releases/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "v1.0.0", "assets": [{"name": "tool-linux-x86_64", "browser_download_url": "https://example.com/tool-linux-x86_64"}]}`))
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	url, tag, err := ResolveAssetURL(context.Background(), "asset-owner/asset-repo", "tool-linux-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "v1.0.0" {
		t.Errorf("tag = %q, want v1.0.0", tag)
	}
	if url != "https://example.com/tool-linux-x86_64" {
		t.Errorf("url = %q, want the matched asset URL", url)
	}
}

func TestResolveAssetURLMatchesOSSynonyms(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.0.0","assets":[{"name":"tool-macos-amd64","browser_download_url":"https://example.com/tool"}]}`))
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	url, _, err := ResolveAssetURL(context.Background(), "synonym-owner/synonym-repo", "tool-{os_any}-{arch_any}", "x86_64", "darwin", "", run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAssetURL: %v", err)
	}
	if url != "https://example.com/tool" {
		t.Fatalf("url = %q", url)
	}
}

func TestResolveAssetURLNoMatchListsAvailableAssets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.0.0","assets":[{"name":"checksums.txt"},{"name":"tool-linux-arm64"}]}`))
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	_, _, err := ResolveAssetURL(context.Background(), "zero-owner/zero-repo", "tool-{os_any}-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err == nil || !strings.Contains(err.Error(), "available assets: checksums.txt, tool-linux-arm64") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveAssetURLRejectsMultipleMatches(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.0.0","assets":[{"name":"tool-linux-x86_64"},{"name":"tool-linux-amd64"},{"name":"tool-linux-arm64"}]}`))
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	_, _, err := ResolveAssetURL(context.Background(), "multi-owner/multi-repo", "tool-{os_any}-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err == nil {
		t.Fatal("expected multiple matches to fail")
	}
	for _, name := range []string{"tool-linux-x86_64", "tool-linux-amd64"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not list match %q: %v", name, err)
		}
	}
	if strings.Contains(err.Error(), "tool-linux-arm64") {
		t.Errorf("error lists non-matching asset: %v", err)
	}
}

func TestResolveAssetURLWithSlashInRefEscapesTagPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantEscaped := "/repos/slash-owner/slash-repo/releases/tags/release%2Fv1"
		if got := r.URL.EscapedPath(); got != wantEscaped {
			t.Errorf("escaped path = %q, want %q", got, wantEscaped)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"release/v1","assets":[{"name":"tool-linux-x86_64","browser_download_url":"https://example.com/tool"}]}`))
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	gotURL, tag, err := ResolveAssetURL(context.Background(), "slash-owner/slash-repo", "tool-linux-{arch_any}", "x86_64", "linux", "release/v1", run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAssetURL: %v", err)
	}
	if tag != "release/v1" || gotURL != "https://example.com/tool" {
		t.Fatalf("ResolveAssetURL = (%q, %q), want asset URL and slash tag", gotURL, tag)
	}
}

func TestResolveAssetURLWithRef(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/repos/ref-owner/ref-repo/releases/tags/nightly"
		if r.URL.Path != want {
			t.Errorf("unexpected path: %s, want %s", r.URL.Path, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "nightly", "assets": [{"name": "tool-linux-aarch64", "browser_download_url": "https://example.com/tool-linux-aarch64"}]}`))
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	url, tag, err := ResolveAssetURL(context.Background(), "ref-owner/ref-repo", "tool-linux-{arch_any}", "aarch64", "linux", "nightly", run.OSExecRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "nightly" {
		t.Errorf("tag = %q, want nightly", tag)
	}
	if url != "https://example.com/tool-linux-aarch64" {
		t.Errorf("url = %q, want the matched asset URL", url)
	}
}

func TestResolveAssetURLRefNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	_, _, err := ResolveAssetURL(context.Background(), "missing-owner/missing-repo", "tool-linux-{arch_any}", "x86_64", "linux", "does-not-exist", run.OSExecRunner{})
	if err == nil {
		t.Fatal("expected error for a ref/tag that doesn't exist as a release")
	}
}

func TestFetchReleaseByTagCachesSeparatelyFromLatest(t *testing.T) {
	var latestHits, tagHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/cache-owner/cache-repo/releases/latest":
			latestHits++
			w.Write([]byte(`{"tag_name": "v2.0.0", "assets": []}`))
		case "/repos/cache-owner/cache-repo/releases/tags/v1.0.0":
			tagHits++
			w.Write([]byte(`{"tag_name": "v1.0.0", "assets": []}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(ts.Close)
	swapHTTPClient(t, ts.URL)

	if _, err := fetchLatestRelease(context.Background(), "cache-owner", "cache-repo", run.OSExecRunner{}); err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if _, err := fetchReleaseByTag(context.Background(), "cache-owner", "cache-repo", "v1.0.0", run.OSExecRunner{}); err != nil {
		t.Fatalf("fetchReleaseByTag: %v", err)
	}
	// Second calls should hit the cache, not the server again.
	if _, err := fetchLatestRelease(context.Background(), "cache-owner", "cache-repo", run.OSExecRunner{}); err != nil {
		t.Fatalf("fetchLatestRelease (cached): %v", err)
	}
	if _, err := fetchReleaseByTag(context.Background(), "cache-owner", "cache-repo", "v1.0.0", run.OSExecRunner{}); err != nil {
		t.Fatalf("fetchReleaseByTag (cached): %v", err)
	}

	if latestHits != 1 {
		t.Errorf("latest endpoint hit %d times, want 1 (second call should be cached)", latestHits)
	}
	if tagHits != 1 {
		t.Errorf("tag endpoint hit %d times, want 1 (second call should be cached)", tagHits)
	}
}

// swapHTTPClient points the package's httpClient at ts for the duration of
// the calling test, restoring the original client on cleanup. Shared helper
// for tests that mock the GitHub API via redirectTripper.
func swapHTTPClient(t *testing.T, testURL string) {
	t.Helper()
	httpClientMu.Lock()
	orig := httpClient
	httpClient = &http.Client{Transport: &redirectTripper{testURL: testURL}}
	httpClientMu.Unlock()
	t.Cleanup(func() {
		httpClientMu.Lock()
		httpClient = orig
		httpClientMu.Unlock()
	})
}

func TestIsGitHubURLCaseInsensitive(t *testing.T) {
	for _, raw := range []string{
		"HTTPS://GITHUB.COM/owner/repo/releases/download/v1/tool",
		"https://GitHub.Com/owner/repo",
	} {
		if !IsGitHubURL(raw) {
			t.Errorf("IsGitHubURL(%q) = false, want true", raw)
		}
	}
}

func TestSplitRepoAcceptsCanonicalGitForms(t *testing.T) {
	for _, input := range []string{
		"owner/repo",
		"github.com/owner/repo",
		"HTTPS://GITHUB.COM/owner/repo",
		"https://github.com/owner/repo.git",
	} {
		owner, repo, ok := splitRepo(input)
		if !ok || owner != "owner" || repo != "repo" {
			t.Errorf("splitRepo(%q) = (%q, %q, %v), want owner/repo", input, owner, repo, ok)
		}
	}
}

func TestGithubTokenNilRunnerWithoutEnvIsSafe(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	ResetGhTokenCache()
	if got := GithubToken(context.Background(), nil); got != "" {
		t.Fatalf("GithubToken(nil) = %q, want empty", got)
	}
}

func TestIsGitHubURLHandlesDefaultPortWithoutTrustingOtherPorts(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/owner/repo/releases/download/v1/tool",
		"HTTPS://GITHUB.COM:443/owner/repo/releases/download/v1/tool",
		"http://github.com:80/owner/repo",
	} {
		if !IsGitHubURL(raw) {
			t.Errorf("IsGitHubURL(%q) = false, want true", raw)
		}
	}
	for _, raw := range []string{
		"https://github.com:444/owner/repo",
		"https://github.com.evil.example/owner/repo",
		"https://example.com/github.com/owner/repo",
	} {
		if IsGitHubURL(raw) {
			t.Errorf("IsGitHubURL(%q) = true, want false", raw)
		}
	}
}

func TestResolveLatestTagHandlesExplicitDefaultGitHubPort(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tag_name":"v9.8.7"}`)
	}))
	defer server.Close()
	swapHTTPClient(t, server.URL)

	tag, err := ResolveLatestTag(context.Background(), "https://github.com:443/owner/repo/releases/download/{latest}/tool", &run.FakeRunner{ExitCode: 1})
	if err != nil {
		t.Fatalf("ResolveLatestTag: %v", err)
	}
	if tag != "v9.8.7" {
		t.Fatalf("tag = %q, want v9.8.7", tag)
	}
	if gotPath != "/repos/owner/repo/releases/latest" {
		t.Fatalf("API path = %q", gotPath)
	}
}
