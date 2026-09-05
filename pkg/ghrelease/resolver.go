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
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Khorea1/depengine/pkg/run"
)

// UserAgent is the identifying string sent on every request this package
// makes to the GitHub API. Exported so other packages that also talk to
// github.com directly (e.g. httpdownload's GoDownloader, downloading the
// release asset itself rather than resolving {latest}) can present as the
// same client instead of drifting out of sync with a copy-pasted literal.
const UserAgent = "github.com/Khorea1/depengine/0.1"

// GitHub URL patterns for release/tag resolution.
var (
	githubRepoRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)`)
	cache        = sync.Map{}

	// httpClient is an HTTP client with a 30s timeout used for GitHub API calls.
	httpClient   = &http.Client{Timeout: 30 * time.Second}
	httpClientMu sync.RWMutex
)

// release represents a GitHub release API response.
type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

// asset is one file attached to a GitHub release.
type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// releaseCache caches full release payloads (tag + asset list), separately
// from the tag-only cache above, since most callers of fetchLatestTag never
// need the asset list and we don't want to force a heavier decode on them.
var releaseCache = sync.Map{}

// ResolveLatest replaces `{latest}` in a URL with the resolved version from
// GitHub's releases API. Uses an in-memory cache so the same owner/repo is
// only resolved once per process lifecycle.
//
// For non-GitHub URLs, {latest} is replaced with the literal string "latest"
// as a best-effort fallback (some hosting services accept this as a version).
//
// Authentication: If GITHUB_TOKEN or GH_TOKEN environment variable is set, it
// is used as a Bearer token in the Authorization header. This raises the
func ResolveLatest(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	if !strings.Contains(urlStr, "{latest}") {
		return urlStr, nil
	}

	matches := githubRepoRe.FindStringSubmatch(urlStr)
	if len(matches) < 3 {
		// Can't resolve {latest} for non-GitHub URLs — leave it for v0.2.
		return strings.ReplaceAll(urlStr, "{latest}", "latest"), nil
	}

	tag, err := fetchLatestTag(ctx, matches[1], matches[2], rn)
	if err != nil {
		return urlStr, err
	}
	return strings.ReplaceAll(urlStr, "{latest}", tag), nil
}

// VersionTag returns the concrete release tag that `{latest}` resolves to
// for urlStr — the version a tool is installed at. For URLs that still
// contain the `{latest}` placeholder it resolves via the GitHub API (cached);
// for URLs without the placeholder (e.g. pins already baked in by
// depengine.lock) it returns "" since the tag cannot be recovered
// generically from an arbitrary URL.
func VersionTag(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	if !strings.Contains(urlStr, "{latest}") {
		return "", nil
	}
	return ResolveLatestTag(ctx, urlStr, rn)
}

// ResolveLatestTag resolves the bare version tag that `{latest}` would
// expand to in urlStr, WITHOUT baking it into a URL. For non-GitHub URLs it
// returns the same literal fallback ("latest") that ResolveLatest substitutes,
// so callers can treat the return value uniformly as "the placeholder value".
//
// This exists for pkg/lock: depengine.lock pins a resolved *version*, not a
// fully-resolved URL. Storing just the tag means that if the URL template in
// schema.toml later changes (a corrected asset filename, a new architecture
// suffix, etc.) between `depengine update` runs, `depengine install` still
// applies the pinned version to the CURRENT template on next use, instead of
// silently re-extracting a stale, fully-baked URL that no longer matches
// what schema.toml declares. It mirrors how Cargo.lock/package-lock.json pin
// versions rather than resolved download URLs.
func ResolveLatestTag(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	matches := githubRepoRe.FindStringSubmatch(urlStr)
	if len(matches) < 3 {
		return "latest", nil
	}
	return fetchLatestTag(ctx, matches[1], matches[2], rn)
}

// fetchLatestTag calls GitHub's releases API for owner/repo and returns the
// latest release's tag name, using the shared in-memory cache so the same
// repo is only fetched once per process lifecycle.
func fetchLatestTag(ctx context.Context, owner, repo string, rn run.Runner) (string, error) {
	cacheKey := owner + "/" + repo

	// Check cache.
	if v, ok := cache.Load(cacheKey); ok {
		return v.(string), nil
	}

	// Fetch latest release from GitHub API.
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("resolve latest: request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", UserAgent)

	// Add GitHub token if available to raise rate limit from 60 to 5000 req/h.
	if token := githubToken(ctx, rn); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := func() (*http.Response, error) {
		httpClientMu.RLock()
		client := httpClient
		httpClientMu.RUnlock()
		return client.Do(req)
	}()
	if err != nil {
		return "", fmt.Errorf("resolve latest: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve latest: GitHub API returned %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("resolve latest: decode: %w", err)
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("resolve latest: empty tag_name from GitHub")
	}

	cache.Store(cacheKey, rel.TagName)
	return rel.TagName, nil
}

// fetchLatestRelease calls GitHub's releases API for owner/repo and returns
// the full latest release payload (tag name + asset list), using its own
// in-memory cache (separate from fetchLatestTag's) so the same repo is only
// fetched once per process lifecycle even when both tag-only and
// asset-matching callers are in play during the same run.
func fetchLatestRelease(ctx context.Context, owner, repo string, rn run.Runner) (*release, error) {
	cacheKey := owner + "/" + repo
	if v, ok := releaseCache.Load(cacheKey); ok {
		return v.(*release), nil
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("resolve release: request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", UserAgent)
	if token := githubToken(ctx, rn); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := func() (*http.Response, error) {
		httpClientMu.RLock()
		client := httpClient
		httpClientMu.RUnlock()
		return client.Do(req)
	}()
	if err != nil {
		return nil, fmt.Errorf("resolve release: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve release: GitHub API returned %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("resolve release: decode: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("resolve release: empty tag_name from GitHub")
	}

	releaseCache.Store(cacheKey, &rel)
	return &rel, nil
}

// ResolveAssetURL resolves a "github" method's {repo, asset} declaration into
// a concrete download URL, by matching assetPattern against the *actual*
// list of asset filenames on the latest GitHub release — instead of the
// schema author having to guess (and hardcode, once per architecture) which
// filename spelling convention the upstream project happens to use.
//
// assetPattern is a filename template that may contain:
//   - "{version}"  → the resolved release tag (matched with or without a
//     leading "v", since some projects tag "v1.2.3" but name assets
//     "myapp-1.2.3...").
//   - "{arch_any}" → matches any known spelling of targetArch (e.g.
//     targetArch "x86_64" also matches assets using "amd64" or "x64").
//   - "{os_any}"   → matches any known spelling of targetOS (e.g. targetOS
//     "darwin" also matches assets using "macos" or "osx").
//
// Every other character in assetPattern is matched literally (regex
// metacharacters are escaped), so normal filenames need no special casing.
// Returns the matched asset's browser_download_url and the resolved tag.
//
// This deliberately does NOT try to guess a spelling when no known synonym
// matches an actual asset — it returns an error naming every asset that WAS
// found, so the schema author can see exactly what's available and adjust
// assetPattern, rather than silently downloading the wrong file.
func ResolveAssetURL(ctx context.Context, repo, assetPattern, targetArch, targetOS string, rn run.Runner) (url, tag string, err error) {
	owner, name, ok := splitRepo(repo)
	if !ok {
		return "", "", fmt.Errorf("resolve asset: %q is not an owner/repo GitHub reference", repo)
	}

	rel, err := fetchLatestRelease(ctx, owner, name, rn)
	if err != nil {
		return "", "", err
	}

	re, err := assetPatternRegexp(assetPattern, rel.TagName, targetArch, targetOS)
	if err != nil {
		return "", "", fmt.Errorf("resolve asset: %w", err)
	}

	for _, a := range rel.Assets {
		if re.MatchString(a.Name) {
			return a.BrowserDownloadURL, rel.TagName, nil
		}
	}

	names := make([]string, len(rel.Assets))
	for i, a := range rel.Assets {
		names[i] = a.Name
	}
	return "", "", fmt.Errorf(
		"resolve asset: no asset in release %s of %s matched pattern %q (arch=%s, os=%s); available assets: %s",
		rel.TagName, repo, assetPattern, targetArch, targetOS, strings.Join(names, ", "))
}

// splitRepo parses an "owner/repo" reference, also accepting a full
// "https://github.com/owner/repo" URL for convenience/copy-paste.
func splitRepo(repo string) (owner, name string, ok bool) {
	repo = strings.TrimSuffix(repo, "/")
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimPrefix(repo, "http://github.com/")
	repo = strings.TrimPrefix(repo, "github.com/")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// GithubToken returns a GitHub personal access token from environment.
// Checks GITHUB_TOKEN first, then GH_TOKEN (common aliases used by gh CLI and CI),
// falling back to `gh auth token` if the GitHub CLI is authenticated. Returns ""
// if no token is available.
//
// Exported so other packages that talk to github.com directly (e.g.
// httpdownload's GoDownloader, which needs the token to fetch private-repo
// release assets, not just resolve {latest}) can reuse the same resolution
// order and the same `gh auth token` cache instead of re-implementing it.
func GithubToken(ctx context.Context, rn run.Runner) string {
	return githubToken(ctx, rn)
}

// githubToken returns a GitHub personal access token from environment.
// Checks GITHUB_TOKEN first, then GH_TOKEN (common aliases used by gh CLI and CI).
// Falls back to `gh auth token` if the GitHub CLI is authenticated.
func githubToken(ctx context.Context, rn run.Runner) string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	return ghCLIToken(ctx, rn)
}

var (
	ghTokenOnce  sync.Once
	ghTokenValue string
)

// ghCLIToken runs `gh auth token` to retrieve the GitHub CLI's authenticated
// token. The result is cached so the subprocess runs at most once per process
// lifecycle.
func ghCLIToken(ctx context.Context, rn run.Runner) string {
	ghTokenOnce.Do(func() {
		res := rn.Run(ctx, "gh", "auth", "token")
		if res.Err != nil || res.ExitCode != 0 {
			ghTokenValue = ""
			return
		}
		ghTokenValue = strings.TrimSpace(string(res.Stdout))
	})
	return ghTokenValue
}

// ResetGhTokenCache resets the cached gh CLI token result. Intended for
// use in tests that manipulate the gh authentication state.
func ResetGhTokenCache() {
	ghTokenOnce = sync.Once{}
	ghTokenValue = ""
}

// IsGitHubURL checks whether a URL points to a GitHub repository.
func IsGitHubURL(rawURL string) bool {
	return githubRepoRe.MatchString(rawURL)
}
