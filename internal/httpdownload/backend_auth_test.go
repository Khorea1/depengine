package httpdownload

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
)

func TestGoDownloaderDownloadWithBearerScopesCredentialToRequest(t *testing.T) {
	const credential = "runtime-only-sentinel"
	var authHeaders []string
	var requestURLs []string
	var userAgents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		requestURLs = append(requestURLs, r.URL.String())
		userAgents = append(userAgents, r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte("artifact"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	dl := NewGoDownloader(nil)
	if err := dl.DownloadWithBearer(context.Background(), server.URL+"/artifact", dest, credential); err != nil {
		t.Fatalf("DownloadWithBearer() error = %v", err)
	}
	if err := dl.Download(context.Background(), server.URL+"/anonymous", dest); err != nil {
		t.Fatalf("Download() after authenticated request error = %v", err)
	}

	if len(authHeaders) != 2 || authHeaders[0] != "Bearer "+credential || authHeaders[1] != "" {
		t.Errorf("Authorization headers = %q, want the Bearer credential on only the first request", authHeaders)
	}
	if strings.Contains(strings.Join(requestURLs, " "), credential) {
		t.Errorf("credential appeared in request URL: %q", requestURLs)
	}
	if userAgents[0] != downloadUserAgent {
		t.Errorf("User-Agent = %q, want %q", userAgents[0], downloadUserAgent)
	}
	// #nosec G304 -- dest is rooted in the test-owned t.TempDir().
	if body, err := os.ReadFile(dest); err != nil || string(body) != "artifact" {
		t.Errorf("downloaded body = %q, error = %v", body, err)
	}
}

func TestGoDownloaderDownloadWithBearerRejectsEmptyCredential(t *testing.T) {
	err := NewGoDownloader(nil).DownloadWithBearer(context.Background(), "https://example.com/file", "unused", "")
	if err == nil || !strings.Contains(err.Error(), "empty Bearer credential") {
		t.Fatalf("DownloadWithBearer() error = %v, want empty credential error", err)
	}
}

func TestGoDownloaderBearerRedirectPolicy(t *testing.T) {
	const credential = "redirect-sentinel"
	var sameOriginHeader string
	sameOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		sameOriginHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("same-origin"))
	}))
	defer sameOrigin.Close()
	var crossOriginHeader string
	otherOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossOriginHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("cross-origin"))
	}))
	defer otherOrigin.Close()
	originRedirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, otherOrigin.URL+"/final", http.StatusFound)
	}))
	defer originRedirect.Close()
	dl := NewGoDownloader(nil)
	if err := dl.DownloadWithBearer(context.Background(), sameOrigin.URL+"/start", filepath.Join(t.TempDir(), "same"), credential); err != nil {
		t.Fatalf("same-origin redirect: %v", err)
	}
	if sameOriginHeader != "Bearer "+credential {
		t.Errorf("same-origin Authorization = %q, want credential retained", sameOriginHeader)
	}
	if err := dl.DownloadWithBearer(context.Background(), originRedirect.URL+"/start", filepath.Join(t.TempDir(), "cross"), credential); err != nil {
		t.Fatalf("cross-origin redirect: %v", err)
	}
	if crossOriginHeader != "" {
		t.Errorf("cross-origin Authorization = %q, want absent", crossOriginHeader)
	}
}

func TestGoDownloaderBearerReturnsUnauthorizedStatus(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			err := NewGoDownloader(nil).DownloadWithBearer(context.Background(), server.URL, filepath.Join(t.TempDir(), "artifact"), "never-print-this")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "never-print-this") {
				t.Fatalf("download error = %v, want status %d without credential", err, status)
			}
		})
	}
}

func TestRetryRetainsBearerForEachPrimaryRequest(t *testing.T) {
	const credential = "retry-sentinel"
	attempts := 0
	var headers []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		headers = append(headers, r.Header.Get("Authorization"))
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("artifact"))
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "artifact")
	dl := NewGoDownloader(nil)
	err := retryWithBackoff(context.Background(), 1, 0, 0, func(ctx context.Context) error {
		return dl.DownloadWithBearer(ctx, server.URL, dest, credential)
	})
	if err != nil {
		t.Fatalf("retry download: %v", err)
	}
	if attempts != 2 || len(headers) != 2 || headers[0] != "Bearer "+credential || headers[1] != "Bearer "+credential {
		t.Fatalf("requests = %d, Authorization headers = %q, want two authenticated attempts", attempts, headers)
	}
}

func TestChecksumSidecarDoesNotReceiveArtifactBearer(t *testing.T) {
	const credential = "primary-only-sentinel"
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(strings.Repeat("0", 64)))
	}))
	defer server.Close()

	ctx := exec.WithHTTPArtifactBearer(context.Background(), credential)
	adapter := NewHTTPAdapter()
	runner := &run.FakeRunner{LookPaths: map[string]bool{"curl": false, "wget": false}}
	_, err := adapter.fetchChecksumFromURL(ctx, runner, server.URL+"/checksum", "artifact", &checksumConfig{algorithm: "sha256", format: "raw"}, map[string]any{})
	if err != nil {
		t.Fatalf("fetch checksum sidecar: %v", err)
	}
	if authorization != "" {
		t.Fatalf("checksum sidecar Authorization = %q, want absent", authorization)
	}
}

func TestChecksumSidecarUsesOnlyItsOwnBearerForExplicitURL(t *testing.T) {
	artifactCredential := "artifact-" + t.Name()
	checksumCredential := "checksum-" + t.Name()
	headers := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers[r.URL.Path] = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(strings.Repeat("0", 64)))
	}))
	defer server.Close()

	ctx := exec.WithHTTPBearer(context.Background(), exec.HTTPBearerArtifact, artifactCredential)
	ctx = exec.WithHTTPBearer(ctx, exec.HTTPBearerChecksum, checksumCredential)
	adapter := NewHTTPAdapter()
	runner := &run.FakeRunner{LookPaths: map[string]bool{"curl": false, "wget": false}}
	checks := []struct {
		url        string
		configured string
		wantAuth   string
	}{
		{url: server.URL + "/explicit", configured: server.URL + "/explicit", wantAuth: "Bearer " + checksumCredential},
		{url: server.URL + "/inferred", wantAuth: ""},
	}
	for _, check := range checks {
		cc := &checksumConfig{algorithm: "sha256", format: "raw", url: check.configured}
		if _, err := adapter.fetchChecksumFromURL(ctx, runner, check.url, "artifact", cc, map[string]any{}); err != nil {
			t.Fatalf("fetch checksum %s: %v", check.url, err)
		}
		path := strings.TrimPrefix(check.url, server.URL)
		if got := headers[path]; got != check.wantAuth {
			t.Errorf("Authorization for %s = %q, want %q", path, got, check.wantAuth)
		}
	}
}

func TestSignatureSidecarUsesOnlyItsOwnBearer(t *testing.T) {
	const (
		artifactCredential  = "artifact-only-sentinel"
		signatureCredential = "signature-only-sentinel"
	)
	var checksumAuth, signatureAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksum":
			checksumAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(strings.Repeat("0", 64)))
		case "/signature":
			signatureAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("signature"))
		}
	}))
	defer server.Close()

	ctx := exec.WithHTTPBearer(context.Background(), exec.HTTPBearerArtifact, artifactCredential)
	ctx = exec.WithHTTPBearer(ctx, exec.HTTPBearerSignature, signatureCredential)
	adapter := NewHTTPAdapter()
	runner := &run.FakeRunner{LookPaths: map[string]bool{"curl": false, "wget": false, "gpg": false}}
	config := map[string]any{"signature_url": server.URL + "/signature"}
	_, err := adapter.fetchChecksumFromURL(ctx, runner, server.URL+"/checksum", "artifact", &checksumConfig{algorithm: "sha256", format: "raw"}, config)
	if err == nil || !strings.Contains(err.Error(), "gpg: not found") {
		t.Fatalf("fetch checksum with unavailable gpg error = %v, want gpg unavailable", err)
	}
	if checksumAuth != "" {
		t.Errorf("checksum Authorization = %q, want absent without checksum credential", checksumAuth)
	}
	if signatureAuth != "Bearer "+signatureCredential {
		t.Errorf("signature Authorization = %q, want its own bearer", signatureAuth)
	}
}

func TestSelectCandidateDownloaderForTypedAuthUsesGoWithoutExecutableLookup(t *testing.T) {
	for _, kind := range []string{"http", "appimage", "android", "msi"} {
		t.Run(kind, func(t *testing.T) {
			fr := &run.FakeRunner{LookPaths: map[string]bool{"curl": true, "wget": true}}
			mc := &config.MethodCandidate{Kind: kind, SecretRef: &config.SecretReference{Provider: "env", Name: "ARTIFACT_TOKEN"}}
			dl := selectCandidateDownloader(context.Background(), fr, "https://example.com/private.tar.gz", mc)
			if _, ok := dl.(*GoDownloader); !ok {
				t.Fatalf("expected GoDownloader for typed %s auth, got %T", kind, dl)
			}
			if len(fr.Calls) != 0 {
				t.Errorf("authenticated selection probed executables: %#v", fr.Calls)
			}
		})
	}
}

func TestSelectDownloaderForURLUsesGoForAuthenticatedGitHub(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret-token")
	t.Setenv("GH_TOKEN", "")
	fr := &run.FakeRunner{LookPaths: map[string]bool{"curl": true, "wget": true}}

	dl := SelectDownloaderForURL(context.Background(), fr, "https://github.com/owner/private/releases/download/v1/tool.tar.gz")
	if _, ok := dl.(*GoDownloader); !ok {
		t.Fatalf("expected GoDownloader for authenticated GitHub URL, got %T", dl)
	}
	for _, call := range fr.Calls {
		for _, arg := range call.Args {
			if strings.Contains(arg, "secret-token") {
				t.Fatal("GitHub token must never be passed in command argv")
			}
		}
	}
}

func TestSelectDownloaderForURLKeepsExternalBackendWithoutGitHubAuth(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	fr := &run.FakeRunner{ExitCode: 1, LookPaths: map[string]bool{"curl": true, "wget": true}}

	dl := SelectDownloaderForURL(context.Background(), fr, "https://example.com/tool.tar.gz")
	if _, ok := dl.(*CurlDownloader); !ok {
		t.Fatalf("expected CurlDownloader for ordinary URL, got %T", dl)
	}
}

func TestSelectDownloaderForURLDoesNotSendGitHubTokenToOtherHosts(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret-token")
	t.Setenv("GH_TOKEN", "")
	fr := &run.FakeRunner{LookPaths: map[string]bool{"curl": true, "wget": true}}

	dl := SelectDownloaderForURL(context.Background(), fr, "https://downloads.example.com/tool.tar.gz")
	if _, ok := dl.(*CurlDownloader); !ok {
		t.Fatalf("expected CurlDownloader for non-GitHub URL, got %T", dl)
	}
}
