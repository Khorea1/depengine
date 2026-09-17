package httpdownload

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/ghrelease"
	"github.com/Khorea1/depengine/pkg/run"
)

// ArtifactRef identifies exactly one downloadable artifact.
type ArtifactRef struct {
	URL, Repo, Asset, Release, Branch, Arch, OS string
}

func artifactRef(mc *config.MethodCandidate) ArtifactRef {
	return ArtifactRef{
		URL: stringConfig(mc, "url"), Repo: stringConfig(mc, "repo"), Asset: stringConfig(mc, "asset"),
		Release: stringConfig(mc, "release"), Branch: stringConfig(mc, "branch"),
		Arch: stringConfig(mc, "_current_arch"), OS: stringConfig(mc, "_current_os"),
	}
}

func stringConfig(mc *config.MethodCandidate, key string) string {
	value, _ := mc.Config[key].(string)
	return value
}

// ResolveArtifact resolves URL templates or an exact GitHub release asset.
func ResolveArtifact(ctx context.Context, mc *config.MethodCandidate, rn run.Runner) (string, error) {
	ref := artifactRef(mc)
	if ref.URL != "" {
		if ref.Repo != "" || ref.Asset != "" {
			return "", fmt.Errorf("artifact: url and repo+asset are mutually exclusive")
		}
		return ResolveLatest(ctx, ref.URL, rn)
	}
	if ref.Repo == "" || ref.Asset == "" {
		return "", fmt.Errorf("artifact: no url or complete repo+asset configured")
	}
	release := ref.Release
	if ref.Branch != "" {
		release = ref.Branch
	}
	if release == "latest" {
		release = ""
	}
	url, _, err := ghrelease.ResolveAssetURL(ctx, ref.Repo, ref.Asset, ref.Arch, ref.OS, release, rn)
	if err != nil {
		return "", fmt.Errorf("artifact: %w", err)
	}
	return url, nil
}

// ResolveLatest replaces `{latest}` in a URL with the resolved version from
// GitHub's releases API. Delegates to depengine/pkg/ghrelease.
func ResolveLatest(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	return ghrelease.ResolveLatest(ctx, urlStr, rn)
}

// IsGitHubURL checks whether a URL points to a GitHub repository.
func IsGitHubURL(rawURL string) bool {
	return ghrelease.IsGitHubURL(rawURL)
}
