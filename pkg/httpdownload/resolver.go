package httpdownload

import (
	"context"

	"depengine/pkg/ghrelease"
)

// ResolveLatest replaces `{latest}` in a URL with the resolved version from
// GitHub's releases API. Delegates to depengine/pkg/ghrelease.
func ResolveLatest(ctx context.Context, urlStr string) (string, error) {
	return ghrelease.ResolveLatest(ctx, urlStr)
}

// IsGitHubURL checks whether a URL points to a GitHub repository.
func IsGitHubURL(rawURL string) bool {
	return ghrelease.IsGitHubURL(rawURL)
}
