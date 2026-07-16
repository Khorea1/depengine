package ghrelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"depengine/pkg/run"
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
