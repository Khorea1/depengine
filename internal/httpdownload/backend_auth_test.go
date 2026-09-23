package httpdownload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
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

func TestSelectCandidateDownloaderForTypedAuthUsesGoWithoutExecutableLookup(t *testing.T) {
	fr := &run.FakeRunner{LookPaths: map[string]bool{"curl": true, "wget": true}}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"secret_ref": map[string]any{"provider": "env", "name": "ARTIFACT_TOKEN"},
	}}

	dl := selectCandidateDownloader(context.Background(), fr, "https://example.com/private.tar.gz", mc)
	if _, ok := dl.(*GoDownloader); !ok {
		t.Fatalf("expected GoDownloader for typed HTTP auth, got %T", dl)
	}
	if len(fr.Calls) != 0 {
		t.Errorf("authenticated selection probed executables: %#v", fr.Calls)
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
