package httpdownload

import (
	"context"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

// GitHubAdapter implements exec.Adapter for the "github" method kind: a
// higher-level alternative to "http" for tools distributed as GitHub
// release assets, for when the asset filename convention differs per
// target (arch/os) in a way that isn't expressible as one URL template.
//
// Instead of the schema author hardcoding one "http" method block per
// architecture spelling (see docs/schema-reference.md's node_exporter
// example — three near-identical blocks just to cover x86_64/aarch64/armv7
// asset names), "github" takes a repo and an asset filename *pattern*
// containing "{arch_any}"/"{os_any}"/"{version}", resolves the actual
// release from GitHub's API, and matches that pattern against the real
// list of asset names — so it works regardless of which spelling
// convention (amd64 vs x86_64 vs x64, ...) the upstream project chose.
//
// Config fields:
//
//	repo    (required) "owner/repo", or a full https://github.com/owner/repo URL
//	asset   (required) filename pattern; see ghrelease.ResolveAssetURL
//	release (optional) named release tag to resolve instead of the latest
//	        release, e.g. "nightly" for a project's rolling pre-release.
//	        Defaults to "latest" (the original latest-release behavior).
//	        Mutually exclusive with branch.
//	branch  (optional) literal branch name, for projects that publish a
//	        release tagged identically to a branch (e.g. an "unstable"
//	        rolling build). This does NOT query git branches/commits — it
//	        resolves the same way as `release`, just documenting intent
//	        differently. Mutually exclusive with release.
//
// Every other field (checksum, checksum_url, extract_to, binary,
// sudo_required, signing_key, signature_url, ...) has the exact same
// meaning as on "http", because Check/Install/Remove all delegate to the
// same HTTPAdapter logic once "url" has been resolved.
type GitHubAdapter struct {
	http *HTTPAdapter
}

// NewGitHubAdapter creates a "github" adapter, delegating the download,
// checksum, extraction and removal logic to an HTTPAdapter once the asset
// URL has been resolved.
func NewGitHubAdapter() *GitHubAdapter {
	return &GitHubAdapter{http: NewHTTPAdapter()}
}

func (a *GitHubAdapter) Kind() string { return "github" }

func (a *GitHubAdapter) RequiresElevation(tool *config.Tool, mc *config.MethodCandidate) bool {
	return a.http.RequiresElevation(tool, mc)
}

// Available mirrors HTTPAdapter: Go's net/http is always available.
func (a *GitHubAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return a.http.Available(ctx, rn)
}

// Check delegates directly to HTTPAdapter.Check, which never inspects the
// "url" field (only extract_to/binary), so no network resolution is needed
// just to answer "is this already installed".
func (a *GitHubAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	return a.http.Check(ctx, rn, tool, mc)
}

// Install resolves {repo, asset} against the GitHub API's real asset list
// for the latest release, then delegates the actual download/checksum/
// extract to HTTPAdapter.Install with "url" filled in.
func (a *GitHubAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	asset, _ := mc.Config["asset"].(string)
	if binary, _ := mc.Config["binary"].(string); binary != "" || isArchive(fileExtension(asset)) {
		return a.http.Install(ctx, rn, tool, mc)
	}
	clone := *mc
	clone.Config = make(map[string]any, len(mc.Config)+1)
	for key, value := range mc.Config {
		clone.Config[key] = value
	}
	clone.Config["binary"] = tool.Name
	return a.http.Install(ctx, rn, tool, &clone)
}

// Remove delegates to HTTPAdapter.Remove, which operates on extract_to/
// binary from the already-recorded install, not on "url" — no resolution
// needed to uninstall something already on disk.
func (a *GitHubAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	return a.http.Remove(ctx, rn, tool, mc)
}

func (a *GitHubAdapter) CanRemove() bool { return a.http.CanRemove() }
