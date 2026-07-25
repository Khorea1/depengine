package httpdownload

import (
	"context"

	"github.com/Khorea1/depengine/pkg/ghrelease"
	"github.com/Khorea1/depengine/pkg/run"
)

// ResolveLatest replaces `{latest}` in a URL with the resolved version from
// GitHub's releases API. Delegates to depengine/pkg/ghrelease.
func ResolveLatest(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	return ghrelease.ResolveLatest(ctx, urlStr, rn)
}

// IsGitHubURL checks whether a URL points to a GitHub repository.
func IsGitHubURL(rawURL string) bool {
	return ghrelease.IsGitHubURL(rawURL)
}
