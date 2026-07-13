// Package git provides an adapter for installing tools via git clone + build.
//
// The GitAdapter clones a repository (shallow by default), optionally runs
// a build command, and optionally copies artifacts to extract_to.
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/ghrelease"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// GitAdapter implements exec.Adapter for git-based installations.
type GitAdapter struct{}

// NewGitAdapter creates a GitAdapter.
func NewGitAdapter() *GitAdapter {
	return &GitAdapter{}
}

func (a *GitAdapter) Kind() string { return "git" }

// Available checks whether git is on PATH.
func (a *GitAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "git")
}

// Check verifies if the tool was already installed via git. Uses two
// strategies:
//  1. If extract_to is set and contains a .git dir, consider it installed.
//  2. If binary is set and exists on PATH, consider it installed.
func (a *GitAdapter) Check(ctx context.Context, rn run.Runner, _ *schema.Tool, mc *schema.MethodCandidate) bool {
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		res := rn.Run(ctx, "test", "-d", extractTo+"/.git")
		if res.Err == nil && res.ExitCode == 0 {
			return true
		}
	}
	if binary, ok := mc.Config["binary"].(string); ok && binary != "" {
		res := rn.Run(ctx, "which", binary)
		if res.Err == nil && res.ExitCode == 0 {
			return true
		}
	}
	return false
}

// Install clones the repository, optionally builds, and optionally copies
// artifacts to the configured extract_to directory.
func (a *GitAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	url, ok := mc.Config["url"].(string)
	if !ok || url == "" {
		return fmt.Errorf("git: no url configured for tool %q", tool.Name)
	}

	// Resolve {latest} to the latest GitHub release tag.
	resolvedURL, err := ghrelease.ResolveLatest(ctx, url)
	if err != nil {
		return fmt.Errorf("git: resolve latest: %w", err)
	}
	url = resolvedURL

	// Determine clone depth (default: shallow).
	depth := "1"
	if d, ok := mc.Config["depth"].(string); ok && d != "" {
		depth = d
	}

	// Determine clone directory — use MkdirTemp for auto-cleanup.
	var cloneDir string
	if d, ok := mc.Config["extract_to"].(string); ok && d != "" {
		cloneDir = d
		if err := os.MkdirAll(cloneDir, 0o755); err != nil {
			return fmt.Errorf("git: mkdir extract_to %s: %w", cloneDir, err)
		}
	} else {
		cloneDir, err = os.MkdirTemp("", "depengine-git-"+tool.Name+"-*")
		if err != nil {
			return fmt.Errorf("git: temp dir: %w", err)
		}
		defer os.RemoveAll(cloneDir)
	}

	// Build clone args.
	cloneArgs := []string{"clone", "--depth", depth}
	if branch, ok := mc.Config["branch"].(string); ok && branch != "" {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, url, cloneDir)

	// Run git clone.
	res := rn.Run(ctx, "git", cloneArgs...)
	if res.Err != nil {
		return fmt.Errorf("git: clone failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("git: clone exited %d: %s", res.ExitCode, stderr)
	}

	// Run build step if configured.
	if buildCmd, ok := mc.Config["build"].(string); ok && buildCmd != "" {
		// Security: buildCmd is passed raw to sh -c to support shell syntax.
		// hasDangerousMethod() already flags "build" config keys with a TOFU
		// security warning unless --allow-arbitrary-code is set.
		fullCmd := fmt.Sprintf("cd %q && %s", cloneDir, buildCmd)
		buildRes := rn.Run(ctx, "sh", "-c", fullCmd)
		if buildRes.Err != nil {
			return fmt.Errorf("git: build failed: %w", buildRes.Err)
		}
		if buildRes.ExitCode != 0 {
			stderr := strings.TrimSpace(string(buildRes.Stderr))
			return fmt.Errorf("git: build exited %d: %s", buildRes.ExitCode, stderr)
		}
	}

	// If extract_to is set, copy artifacts from clone dir to extract destination.
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		artifact, ok2 := mc.Config["artifact"].(string)
		if !ok2 || artifact == "" {
			artifact = "/"
		}
		src := filepath.Join(cloneDir, artifact)
		if err := os.MkdirAll(extractTo, 0o755); err != nil {
			return fmt.Errorf("git: mkdir %s: %w", extractTo, err)
		}
		cpCmd := fmt.Sprintf("cp -r %q/. %q", src, extractTo)
		cpRes := rn.Run(ctx, "sh", "-c", cpCmd)
		if cpRes.Err != nil || cpRes.ExitCode != 0 {
			return fmt.Errorf("git: copy artifacts: %w", cpRes.Err)
		}
	}

	return nil
}

// Ensure GitAdapter implements exec.Adapter at compile time.
var _ exec.Adapter = (*GitAdapter)(nil)
