// Package ghrelease resolves GitHub release tags for the {latest} placeholder.
//
// Both the git and http adapters need to replace "{latest}" in URLs with the
// actual latest release tag from GitHub. This package provides a shared
// ResolveLatest with an in-memory cache so the same owner/repo is resolved
// only once per process lifecycle.
package ghrelease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// GitHub URL patterns for release/tag resolution.
var (
	githubRepoRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)`)
	cache        = sync.Map{}
)

// release represents a GitHub release API response.
type release struct {
	TagName string `json:"tag_name"`
}

// ResolveLatest replaces `{latest}` in a URL with the resolved version from
// GitHub's releases API. Uses an in-memory cache so the same owner/repo is
// only resolved once per process lifecycle.
//
// For non-GitHub URLs, {latest} is replaced with the literal string "latest"
// as a best-effort fallback (some hosting services accept this as a version).
func ResolveLatest(ctx context.Context, urlStr string) (string, error) {
	if !strings.Contains(urlStr, "{latest}") {
		return urlStr, nil
	}

	matches := githubRepoRe.FindStringSubmatch(urlStr)
	if len(matches) < 3 {
		// Can't resolve {latest} for non-GitHub URLs — leave it for v0.2.
		return strings.ReplaceAll(urlStr, "{latest}", "latest"), nil
	}

	owner, repo := matches[1], matches[2]
	cacheKey := owner + "/" + repo

	// Check cache.
	if v, ok := cache.Load(cacheKey); ok {
		tag := v.(string)
		return strings.ReplaceAll(urlStr, "{latest}", tag), nil
	}

	// Fetch latest release from GitHub API.
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return urlStr, fmt.Errorf("resolve latest: request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "depengine/0.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return urlStr, fmt.Errorf("resolve latest: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return urlStr, fmt.Errorf("resolve latest: GitHub API returned %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return urlStr, fmt.Errorf("resolve latest: decode: %w", err)
	}
	if rel.TagName == "" {
		return urlStr, fmt.Errorf("resolve latest: empty tag_name from GitHub")
	}

	cache.Store(cacheKey, rel.TagName)
	return strings.ReplaceAll(urlStr, "{latest}", rel.TagName), nil
}

// IsGitHubURL checks whether a URL points to a GitHub repository.
func IsGitHubURL(rawURL string) bool {
	return githubRepoRe.MatchString(rawURL)
}
