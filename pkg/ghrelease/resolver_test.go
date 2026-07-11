package ghrelease

import (
	"context"
	"testing"
)

func TestResolveLatestNoPlaceholder(t *testing.T) {
	url := "https://github.com/user/repo/releases/download/v1.0/file.tar.gz"
	got, err := ResolveLatest(context.Background(), url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != url {
		t.Fatalf("ResolveLatest(%q) = %q, want %q", url, got, url)
	}
}

func TestResolveLatestNonGitHub(t *testing.T) {
	url := "https://gitlab.com/user/repo/-/releases/{latest}/file.tar.gz"
	got, err := ResolveLatest(context.Background(), url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "https://gitlab.com/user/repo/-/releases/latest/file.tar.gz"
	if got != expected {
		t.Fatalf("ResolveLatest = %q, want %q (fallback 'latest')", got, expected)
	}
}

func TestResolveLatestEmptyURL(t *testing.T) {
	got, err := ResolveLatest(context.Background(), "")
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
