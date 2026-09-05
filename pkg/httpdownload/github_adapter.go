package httpdownload

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/ghrelease"
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
//	repo  (required) "owner/repo", or a full https://github.com/owner/repo URL
//	asset (required) filename pattern; see ghrelease.ResolveAssetURL
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

func init() {
	exec.Register(NewGitHubAdapter())
}

func (a *GitHubAdapter) Kind() string { return "github" }

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
	resolvedMC, err := a.resolve(ctx, rn, tool, mc)
	if err != nil {
		return err
	}
	return a.http.Install(ctx, rn, tool, resolvedMC)
}

// Remove delegates to HTTPAdapter.Remove, which operates on extract_to/
// binary from the already-recorded install, not on "url" — no resolution
// needed to uninstall something already on disk.
func (a *GitHubAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	return a.http.Remove(ctx, rn, tool, mc)
}

func (a *GitHubAdapter) CanRemove() bool { return a.http.CanRemove() }

// resolve builds a copy of mc with "url" populated from the matched GitHub
// release asset, leaving every other field (checksum, extract_to, binary,
// ...) untouched for HTTPAdapter to use exactly as it would for "http".
func (a *GitHubAdapter) resolve(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (*config.MethodCandidate, error) {
	repo, _ := mc.Config["repo"].(string)
	if repo == "" {
		return nil, fmt.Errorf("github: no repo configured for tool %q", tool.Name)
	}
	assetPattern, _ := mc.Config["asset"].(string)
	if assetPattern == "" {
		return nil, fmt.Errorf("github: no asset pattern configured for tool %q", tool.Name)
	}
	arch, _ := mc.Config["_current_arch"].(string)
	osName, _ := mc.Config["_current_os"].(string)

	url, _, err := ghrelease.ResolveAssetURL(ctx, repo, assetPattern, arch, osName, rn)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}

	resolved := make(map[string]any, len(mc.Config)+1)
	for k, v := range mc.Config {
		resolved[k] = v
	}
	resolved["url"] = url

	return &config.MethodCandidate{
		Kind:   "http",
		Label:  mc.Label,
		When:   mc.When,
		Config: resolved,
	}, nil
}
