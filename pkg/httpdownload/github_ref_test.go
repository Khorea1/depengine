package httpdownload

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestResolveArtifactDirectURL(t *testing.T) {
	got, err := ResolveArtifact(context.Background(), &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.invalid/tool.tar.gz",
	}}, &run.FakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.invalid/tool.tar.gz" {
		t.Fatalf("url=%q", got)
	}
}

func TestResolveArtifactRejectsMissingSource(t *testing.T) {
	_, err := ResolveArtifact(context.Background(), &config.MethodCandidate{Config: map[string]any{}}, &run.FakeRunner{})
	if err == nil || !strings.Contains(err.Error(), "no url or complete repo+asset") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveArtifactRejectsIncompleteRepoAsset(t *testing.T) {
	for _, cfg := range []map[string]any{{"repo": "owner/repo"}, {"asset": "tool-*.tar.gz"}} {
		_, err := ResolveArtifact(context.Background(), &config.MethodCandidate{Config: cfg}, &run.FakeRunner{})
		if err == nil || !strings.Contains(err.Error(), "complete repo+asset") {
			t.Fatalf("cfg=%v err=%v", cfg, err)
		}
	}
}

func TestResolveArtifactRejectsURLWithRepoAsset(t *testing.T) {
	_, err := ResolveArtifact(context.Background(), &config.MethodCandidate{Config: map[string]any{
		"url": "https://example.invalid/tool.tar.gz", "repo": "owner/repo", "asset": "tool.tar.gz",
	}}, &run.FakeRunner{})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err=%v", err)
	}
}

func TestArtifactRefCarriesExplicitReleaseAndBranch(t *testing.T) {
	ref := artifactRef(&config.MethodCandidate{Config: map[string]any{
		"repo": "owner/repo", "asset": "tool-*.tar.gz", "release": "v1.2.3", "branch": "nightly",
	}})
	if ref.Release != "v1.2.3" || ref.Branch != "nightly" {
		t.Fatalf("ref=%+v", ref)
	}
}
