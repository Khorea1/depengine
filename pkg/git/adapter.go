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

func init() {
	exec.Register(NewGitAdapter())
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes
// with the standard POSIX '\” sequence. Inside single quotes every character
// is literal, so the result is safe for use in sh -c strings.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
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

	origURL := url
	resolvedURL, err := ghrelease.ResolveLatest(ctx, url, rn)
	if err != nil {
		return fmt.Errorf("git: resolve latest: %w", err)
	}

	// If {latest} was resolved, extract the tag and use it as --branch.
	// Embedding the tag in the clone URL (e.g. repo.gitv1.2.3) is invalid
	// for git clone — the tag must be passed as a separate argument.
	var resolvedTag string
	if strings.Contains(origURL, "{latest}") && resolvedURL != origURL {
		pos := strings.Index(origURL, "{latest}")
		prefix := origURL[:pos]
		suffix := origURL[pos+len("{latest}"):]
		url = prefix + suffix
		resolvedTag = strings.TrimPrefix(resolvedURL, prefix)
		resolvedTag = strings.TrimSuffix(resolvedTag, suffix)
	} else {
		url = resolvedURL
	}

	// Determine clone depth (default: shallow).
	// The schema accepts both integer (0 for full history) and string ("1") values.
	depth := "1"
	switch d := mc.Config["depth"].(type) {
	case string:
		if d != "" {
			depth = d
		}
	case int64:
		depth = fmt.Sprintf("%d", d)
	}

	// Determine clone directory — use MkdirTemp for auto-cleanup.
	cloneDir, err := os.MkdirTemp("", "depengine-git-"+tool.Name+"-*")
	if err != nil {
		return fmt.Errorf("git: temp dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	// Build clone args.
	cloneArgs := []string{"clone", "--depth", depth}
	// If {latest} was resolved, use the resolved tag as --branch.
	// If the user also specified an explicit branch, resolvedTag wins
	// (it's the concrete latest release, which is more specific).
	if resolvedTag != "" {
		cloneArgs = append(cloneArgs, "--branch", resolvedTag)
	} else if branch, ok := mc.Config["branch"].(string); ok && branch != "" {
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
		fullCmd := fmt.Sprintf("cd %s && %s", shellQuote(cloneDir), buildCmd)
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
		cpCmd := fmt.Sprintf("cp -r %s/. %s", shellQuote(src), shellQuote(extractTo))
		cpRes := rn.Run(ctx, "sh", "-c", cpCmd)
		if cpRes.Err != nil {
			return fmt.Errorf("git: copy artifacts: %w", cpRes.Err)
		}
		if cpRes.ExitCode != 0 {
			stderr := strings.TrimSpace(string(cpRes.Stderr))
			return fmt.Errorf("git: copy exited %d: %s", cpRes.ExitCode, stderr)
		}
	}

	return nil
}

// isSharedDir checks if a directory path is a common shared system directory.
// We avoid deleting these directories completely during uninstallation.
func isSharedDir(path string) bool {
	p := filepath.Clean(path)
	if p == "/" || p == "." {
		return true
	}
	shared := []string{
		"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin", "/usr/local/sbin",
		"/opt", "/usr", "/usr/local", "/lib", "/usr/lib", "/usr/local/lib",
		"C:\\Windows", "C:\\Program Files", "C:\\Program Files (x86)",
	}
	for _, s := range shared {
		if p == s {
			return true
		}
	}
	// Check for shared directory suffixes once (not re-evaluated per loop entry).
	if strings.HasSuffix(p, "/bin") || strings.HasSuffix(p, "/sbin") || strings.HasSuffix(p, "\\bin") {
		return true
	}
	return false
}

// Remove uninstalls the tool by removing either the extracted binary (if extract_to
// is a shared directory) or the entire extract_to directory (if it is tool-specific).
func (a *GitAdapter) Remove(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	extractTo, ok := mc.Config["extract_to"].(string)
	if !ok || extractTo == "" {
		return fmt.Errorf("git: remove not supported without extract_to — installed via custom buildCmd/make install")
	}

	binary, hasBinary := mc.Config["binary"].(string)

	if isSharedDir(extractTo) {
		if hasBinary && binary != "" {
			target := filepath.Join(extractTo, binary)
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("git: remove binary %s: %w", target, err)
			}
			return nil
		}
		return fmt.Errorf("git: cannot remove: %s is a shared directory and binary is not configured", extractTo)
	}

	// Not a shared directory — safe to delete the whole directory
	if err := os.RemoveAll(extractTo); err != nil {
		return fmt.Errorf("git: remove directory %s: %w", extractTo, err)
	}

	return nil
}

// CanRemove returns true — the adapter can remove installations that were done
// with an extract_to path. Per-method validation (e.g. extract_to is required)
// happens inside Remove() and returns an error if the method config doesn't
// support automated removal.
func (a *GitAdapter) CanRemove() bool { return true }

// Ensure GitAdapter implements exec.Adapter at compile time.
var _ exec.Adapter = (*GitAdapter)(nil)
var _ exec.Remover = (*GitAdapter)(nil)
