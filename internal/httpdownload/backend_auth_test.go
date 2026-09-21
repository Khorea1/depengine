package httpdownload

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

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
