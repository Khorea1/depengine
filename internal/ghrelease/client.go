package ghrelease

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/Khorea1/depengine/internal/run"
)

// Resolver is an injectable GitHub release-resolution client. It owns the
// tag/release caches, the HTTP client, and the `gh auth token` cache that
// used to live in package globals (reset by hand between tests). Most
// callers keep using the package-level functions, which delegate to
// Default; tests and concurrent workflows construct their own Resolver
// for isolation instead of mutating shared process state.
//
// A Resolver must not be copied after first use.
type Resolver struct {
	tags     sync.Map
	releases sync.Map

	httpMu sync.RWMutex
	http   *http.Client

	// tokCached replaces the old ghTokenOnce: an explicit cached flag
	// under mutex, so the cache can be reset without reconstructing a
	// sync.Once by hand.
	tokMu     sync.Mutex
	tokCached bool
	tokValue  string
}

// NewResolver returns an isolated Resolver with the production HTTP client.
func NewResolver() *Resolver {
	return &Resolver{http: &http.Client{Timeout: 30 * time.Second}}
}

// SetHTTPClient swaps the HTTP client. It is the test seam that replaces
// direct poking of the old package-level httpClient global.
func (r *Resolver) SetHTTPClient(c *http.Client) {
	r.httpMu.Lock()
	defer r.httpMu.Unlock()
	r.http = c
}

// ResetTokenCache drops the cached `gh auth token` result so the next
// GithubToken call re-probes. Prefer a fresh Resolver in new tests.
func (r *Resolver) ResetTokenCache() {
	r.tokMu.Lock()
	defer r.tokMu.Unlock()
	r.tokCached = false
	r.tokValue = ""
}

// Default is the process-wide Resolver backing the package-level
// functions below. It preserves the previous "once per process" caching
// behavior for existing callers; new code that needs isolation should
// construct its own Resolver.
var Default = NewResolver()

// ResolveLatest replaces `{latest}` in a URL with the resolved version from
// GitHub's releases API. See (*Resolver).ResolveLatest.
func ResolveLatest(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	return Default.ResolveLatest(ctx, urlStr, rn)
}

// VersionTag returns the concrete release tag that `{latest}` resolves to.
// See (*Resolver).VersionTag.
func VersionTag(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	return Default.VersionTag(ctx, urlStr, rn)
}

// ResolveLatestTag resolves the bare version tag that `{latest}` would
// expand to in urlStr. See (*Resolver).ResolveLatestTag.
func ResolveLatestTag(ctx context.Context, urlStr string, rn run.Runner) (string, error) {
	return Default.ResolveLatestTag(ctx, urlStr, rn)
}

// ResolveLatestReleaseTag returns the latest release tag for an owner/repo
// reference. See (*Resolver).ResolveLatestReleaseTag.
func ResolveLatestReleaseTag(ctx context.Context, repo string, rn run.Runner) (string, error) {
	return Default.ResolveLatestReleaseTag(ctx, repo, rn)
}

// ResolveAssetURL resolves a "github" method's {repo, asset} declaration
// into a concrete download URL. See (*Resolver).ResolveAssetURL.
func ResolveAssetURL(ctx context.Context, repo, assetPattern, targetArch, targetOS, ref string, rn run.Runner) (url, tag string, err error) {
	return Default.ResolveAssetURL(ctx, repo, assetPattern, targetArch, targetOS, ref, rn)
}

// GithubToken returns a GitHub personal access token from environment,
// falling back to `gh auth token`. See (*Resolver).GithubToken.
func GithubToken(ctx context.Context, rn run.Runner) string {
	return Default.GithubToken(ctx, rn)
}

// ResetGhTokenCache resets Default's cached gh CLI token result. Prefer
// (*Resolver).ResetTokenCache on an owned instance in new code.
func ResetGhTokenCache() {
	Default.ResetTokenCache()
}
