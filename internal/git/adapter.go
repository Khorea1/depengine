// Package git provides an adapter for installing tools via git clone + build.
//
// The GitAdapter clones a repository (shallow by default), optionally runs
// a build command, and optionally copies artifacts to extract_to.
package git

import (
	"context"
	"errors"
	"fmt"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// GitAdapter implements exec.AdapterV2 for git-based installations.
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
func (a *GitAdapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	if paths, err := managedPaths(mc); err == nil && len(paths) > 0 {
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				return false
			}
		}
		return true
	}
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		extractTo = config.ExpandHomeDir(extractTo)
		if info, err := os.Stat(filepath.Join(extractTo, ".git")); err == nil && info.IsDir() {
			if ref := configuredRevision(mc); ref != "" {
				res := rn.Run(ctx, "git", "-C", extractTo, "rev-parse", "HEAD", ref)
				if res.Err != nil || res.ExitCode != 0 {
					return false
				}
				lines := nonEmptyLines(string(res.Stdout))
				return len(lines) >= 2 && lines[0] == lines[1]
			}
			return true
		}
	}
	if binary, ok := mc.Config["binary"].(string); ok && binary != "" {
		if run.LookPath(ctx, rn, binary) {
			return true
		}
	}
	return false
}

// Observe reports whether the tool is already installed via git, using the
// same three strategies as Check: managed paths on disk, an extract_to
// checkout (with HEAD pinned against the configured revision when one is
// set), and a binary on PATH. A revision match additionally carries the
// pinned revision as known identity; the other strategies establish
// presence only.
func (a *GitAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("git: tool and method are required")
	}
	if paths, err := managedPaths(mc); err == nil && len(paths) > 0 {
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				return plan.Observation{Presence: plan.PresenceAbsent}, nil
			}
		}
		return plan.Observation{Presence: plan.PresencePresent}, nil
	}
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		extractTo = config.ExpandHomeDir(extractTo)
		if info, err := os.Stat(filepath.Join(extractTo, ".git")); err == nil && info.IsDir() {
			if ref := configuredRevision(mc); ref != "" {
				res := rn.Run(ctx, "git", "-C", extractTo, "rev-parse", "HEAD", ref)
				if res.Err != nil || res.ExitCode != 0 {
					return plan.Observation{Presence: plan.PresenceAbsent}, nil
				}
				lines := nonEmptyLines(string(res.Stdout))
				if len(lines) < 2 || lines[0] != lines[1] {
					return plan.Observation{Presence: plan.PresenceAbsent}, nil
				}
				return plan.Observation{
					Presence:    plan.PresencePresent,
					Identity:    plan.ObservedIdentity{Revision: ref},
					KnownFields: []plan.IdentityField{plan.FieldRevision},
				}, nil
			}
			return plan.Observation{Presence: plan.PresencePresent}, nil
		}
	}
	if binary, ok := mc.Config["binary"].(string); ok && binary != "" {
		if run.LookPath(ctx, rn, binary) {
			return plan.Observation{Presence: plan.PresencePresent}, nil
		}
	}
	return plan.Observation{Presence: plan.PresenceAbsent}, nil
}

// InstalledVersion reports the version the git method pinned the tool to:
// the resolved `{latest}` tag when the URL still contains the placeholder, or
// the configured branch. Returns "" when no version is knowable.
func (a *GitAdapter) InstalledVersion(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) (string, error) {
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		extractTo = config.ExpandHomeDir(extractTo)
		if info, err := os.Stat(filepath.Join(extractTo, ".git")); err == nil && info.IsDir() {
			res := rn.Run(ctx, "git", "-C", extractTo, "rev-parse", "HEAD")
			if res.Err == nil && res.ExitCode == 0 {
				if lines := nonEmptyLines(string(res.Stdout)); len(lines) > 0 {
					return lines[0], nil
				}
			}
		}
	}
	urlRaw, ok := mc.Config["url"].(string)
	if !ok || urlRaw == "" {
		return "", nil
	}
	if tag, err := ghrelease.VersionTag(ctx, urlRaw, rn); err == nil && tag != "" && tag != "latest" {
		return tag, nil
	}
	if ref := configuredRevision(mc); ref != "" {
		return ref, nil
	}
	return "", nil
}

func normalizedGitDepth(raw any) (string, error) {
	if raw == nil {
		return "1", nil
	}
	var value int64
	switch depth := raw.(type) {
	case int64:
		value = depth
	case string:
		if depth == "" {
			return "1", nil
		}
		parsed, err := strconv.ParseInt(depth, 10, 64)
		if err != nil {
			return "", fmt.Errorf("git: depth must be a non-negative integer, got %q", depth)
		}
		value = parsed
	default:
		return "", fmt.Errorf("git: depth must be an integer or numeric string, got %T", raw)
	}
	if value < 0 {
		return "", fmt.Errorf("git: depth must be non-negative")
	}
	return strconv.FormatInt(value, 10), nil
}

func configuredRevision(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	for _, field := range []string{"rev", "tag", "branch"} {
		if value, ok := mc.Config[field].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func nonEmptyLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

type resolvedCloneSource struct {
	URL         string
	Branch      string
	Tag         string
	Revision    string
	ResolvedTag string
}

// resolveCloneSource performs the read-only part of Install's git source
// resolution. Keeping it shared with ResolvePlan prevents dry-run from
// presenting a URL/ref different from the one Install will consume.
func resolveCloneSource(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (resolvedCloneSource, error) {
	url, ok := mc.Config["url"].(string)
	if !ok || url == "" {
		name := ""
		if tool != nil {
			name = tool.Name
		}
		return resolvedCloneSource{}, fmt.Errorf("git: no url configured for tool %q", name)
	}
	if parsed, err := urlpkg.Parse(url); err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) && parsed.User != nil {
		return resolvedCloneSource{}, fmt.Errorf("git: embedded URL credentials are not allowed; use an external credential helper")
	}

	source := resolvedCloneSource{URL: url}
	source.Branch, _ = mc.Config["branch"].(string)
	source.Tag, _ = mc.Config["tag"].(string)
	source.Revision, _ = mc.Config["rev"].(string)
	configuredRefs := 0
	for _, ref := range []string{source.Branch, source.Tag, source.Revision} {
		if ref != "" {
			configuredRefs++
		}
	}
	if configuredRefs > 1 {
		return resolvedCloneSource{}, fmt.Errorf("git: branch, tag, and rev are mutually exclusive")
	}
	origURL := source.URL
	if strings.Contains(origURL, "{latest}") && configuredRefs > 0 {
		return resolvedCloneSource{}, fmt.Errorf("git: {latest} URL resolution cannot be combined with branch, tag, or rev")
	}

	resolvedURL, err := ghrelease.ResolveLatest(ctx, source.URL, rn)
	if err != nil {
		return resolvedCloneSource{}, fmt.Errorf("git: resolve latest: %w", err)
	}
	if strings.Contains(origURL, "{latest}") && resolvedURL != origURL {
		prefix, suffix, _ := strings.Cut(origURL, "{latest}")
		source.URL = prefix + suffix
		source.ResolvedTag = strings.TrimPrefix(resolvedURL, prefix)
		source.ResolvedTag = strings.TrimSuffix(source.ResolvedTag, suffix)
	} else {
		source.URL = resolvedURL
	}
	return source, nil
}

// ResolvePlan exposes the concrete clone URL and resolved/requested ref without
// cloning or otherwise mutating host state.
func (a *GitAdapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, nil
	}
	source, err := resolveCloneSource(ctx, rn, tool, mc)
	if err != nil {
		return intent, err
	}
	resolved := intent.Clone()
	resolved.Identity.Source = source.URL
	switch {
	case source.ResolvedTag != "":
		resolved.Identity.Version = source.ResolvedTag
	case source.Revision != "":
		resolved.Identity.Revision = source.Revision
	}
	return &resolved, nil
}

// Install clones the repository, optionally builds, and optionally copies
// artifacts to the configured extract_to directory.
func (a *GitAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if _, err := managedPaths(mc); err != nil {
		return err
	}
	source, err := resolveCloneSource(ctx, rn, tool, mc)
	if err != nil {
		return err
	}
	return a.installResolvedSource(ctx, rn, tool, mc, source)
}

// InstallResolved executes an already-resolved clone plan. The clone identity
// (URL and ref) comes exclusively from resolved; mc.Config supplies only
// non-identity execution parameters (depth, submodules, build, extract_to,
// artifact, managed_paths). It never calls resolveCloneSource, ResolveLatest,
// or any network resolution.
func (a *GitAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if _, err := managedPaths(mc); err != nil {
		return err
	}
	source, err := resolvedCloneSourceFromPlan(resolved)
	if err != nil {
		return err
	}
	return a.installResolvedSource(ctx, rn, tool, mc, source)
}

// resolvedCloneSourceFromPlan rebuilds the clone identity solely from the
// resolved plan. RequestedVersion preserves the configured branch/tag/rev
// selector; Identity carries the concrete URL, resolved {latest} tag
// (Version), and pinned revision (Revision).
func resolvedCloneSourceFromPlan(resolved *plan.ResolvedInstallPlan) (resolvedCloneSource, error) {
	if resolved == nil {
		return resolvedCloneSource{}, fmt.Errorf("git: resolved plan is required")
	}
	source := resolvedCloneSource{URL: resolved.Identity.Source}
	if source.URL == "" {
		return resolvedCloneSource{}, fmt.Errorf("git: resolved plan has no concrete clone URL")
	}
	if resolved.Identity.Revision != "" {
		source.Revision = resolved.Identity.Revision
		return source, nil
	}
	if requested := resolved.Identity.RequestedVersion; requested != nil {
		switch requested.Mode {
		case plan.VersionGitBranch:
			source.Branch = requested.Value
			return source, nil
		case plan.VersionGitTag:
			source.Tag = requested.Value
			return source, nil
		case plan.VersionGitRevision:
			source.Revision = requested.Value
			return source, nil
		}
	}
	// No pinned selector: either a default-branch clone or a resolved
	// {latest} tag carried in Identity.Version.
	if resolved.Identity.Version != "" {
		source.ResolvedTag = resolved.Identity.Version
	}
	return source, nil
}

func (a *GitAdapter) installResolvedSource(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, source resolvedCloneSource) error {
	url := source.URL
	branch := source.Branch
	tag := source.Tag
	rev := source.Revision
	resolvedTag := source.ResolvedTag

	// Determine clone depth (default: shallow). 0 means full history.
	depth, err := normalizedGitDepth(mc.Config["depth"])
	if err != nil {
		return err
	}

	// Determine clone directory — use MkdirTemp for auto-cleanup.
	cloneDir, err := os.MkdirTemp("", "depengine-git-"+tool.Name+"-*")
	if err != nil {
		return fmt.Errorf("git: temp dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	// Build clone args. depth=0 means full history and therefore omits
	// --depth entirely; git rejects --depth 0.
	cloneArgs := []string{"clone"}
	if depth != "0" {
		cloneArgs = append(cloneArgs, "--depth", depth)
	}
	if rev != "" {
		cloneArgs = append(cloneArgs, "--no-checkout")
	}
	// Branches and tags can both use clone --branch. Exact revs are fetched
	// and detached below because arbitrary commits need not be branch tips.
	switch {
	case resolvedTag != "":
		cloneArgs = append(cloneArgs, "--branch", resolvedTag)
	case branch != "":
		cloneArgs = append(cloneArgs, "--branch", branch)
	case tag != "":
		cloneArgs = append(cloneArgs, "--branch", tag)
	}

	cloneArgs = append(cloneArgs, url, cloneDir)

	// Run git clone.
	res := rn.Run(ctx, "git", cloneArgs...)
	if err := run.CheckResult(res, "git: clone"); err != nil {
		return err
	}
	if rev != "" {
		fetchArgs := []string{"-C", cloneDir, "fetch"}
		if depth != "0" {
			fetchArgs = append(fetchArgs, "--depth", depth)
		}
		fetchArgs = append(fetchArgs, "origin", rev)
		if err := run.CheckResult(rn.Run(ctx, "git", fetchArgs...), "git: fetch revision"); err != nil {
			return err
		}
		if err := run.CheckResult(rn.Run(ctx, "git", "-C", cloneDir, "checkout", "--detach", "FETCH_HEAD"), "git: checkout revision"); err != nil {
			return err
		}
	}
	if submodules, _ := mc.Config["submodules"].(bool); submodules {
		if err := run.CheckResult(rn.Run(ctx, "git", "-C", cloneDir, "submodule", "update", "--init", "--recursive"), "git: submodules"); err != nil {
			return err
		}
	}

	// Run build step if configured.
	if rawBuild, ok := mc.Config["build"]; ok {
		buildCommands, err := parseBuildCommands(rawBuild)
		if err != nil {
			return fmt.Errorf("git: build: %w", err)
		}
		for _, command := range buildCommands {
			buildRes := run.RunInDir(ctx, rn, cloneDir, command[0], command[1:]...)
			if err := run.CheckResult(buildRes, "git: build"); err != nil {
				return err
			}
		}
	}

	// If extract_to is set, copy artifacts from clone dir to extract destination.
	if extractTo, ok := mc.Config["extract_to"].(string); ok && extractTo != "" {
		extractTo = config.ExpandHomeDir(extractTo)
		artifact, ok2 := mc.Config["artifact"].(string)
		if !ok2 || artifact == "" {
			artifact = "/"
		}
		src, err := artifactPath(cloneDir, artifact)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(extractTo, 0o755); err != nil {
			return fmt.Errorf("git: mkdir %s: %w", extractTo, err)
		}
		if err := copyArtifact(ctx, src, extractTo); err != nil {
			return fmt.Errorf("git: copy: %w", err)
		}
	}

	return nil
}

func parseBuildCommands(raw any) ([][]string, error) {
	if command, ok := raw.(string); ok {
		if command == "" {
			return nil, fmt.Errorf("command must not be empty")
		}
		return [][]string{{"sh", "-c", command}}, nil
	}
	if tables, ok := raw.([]any); ok {
		commands := make([][]string, 0, len(tables))
		for i, table := range tables {
			command, err := parseBuildCommand(table)
			if err != nil {
				return nil, fmt.Errorf("command %d: %w", i, err)
			}
			commands = append(commands, command)
		}
		if len(commands) == 0 {
			return nil, fmt.Errorf("command list must not be empty")
		}
		return commands, nil
	}
	command, err := parseBuildCommand(raw)
	if err != nil {
		return nil, err
	}
	return [][]string{command}, nil
}

func parseBuildCommand(raw any) ([]string, error) {
	table, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected command table, got %T", raw)
	}
	if len(table) != 1 {
		return nil, fmt.Errorf("command table must contain only run")
	}
	rawArgs, ok := table["run"].([]any)
	if !ok || len(rawArgs) == 0 {
		return nil, fmt.Errorf("run must be a non-empty string array")
	}
	args := make([]string, len(rawArgs))
	for i, rawArg := range rawArgs {
		arg, ok := rawArg.(string)
		if !ok {
			return nil, fmt.Errorf("run[%d] must be a string", i)
		}
		args[i] = arg
	}
	if args[0] == "" {
		return nil, fmt.Errorf("executable must not be empty")
	}
	return args, nil
}

func artifactPath(cloneDir, artifact string) (string, error) {
	src := filepath.Join(cloneDir, artifact)
	rel, err := filepath.Rel(cloneDir, src)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("git: artifact %q escapes clone directory", artifact)
	}
	return src, nil
}

// isSharedDir checks if a directory path is a common shared system directory.
// We avoid deleting these directories completely during uninstallation.
// Separators are folded to "/" after Clean so Unix-style manifests and
// Windows-style paths evaluate identically on every platform (ToSlash
// alone is a no-op for literal backslashes on Unix).
func isSharedDir(path string) bool {
	p := strings.ReplaceAll(filepath.Clean(path), "\\", "/")
	if p == "/" || p == "." {
		return true
	}
	shared := []string{
		"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin", "/usr/local/sbin",
		"/opt", "/usr", "/usr/local", "/lib", "/usr/lib", "/usr/local/lib",
		"C:/Windows", "C:/Program Files", "C:/Program Files (x86)",
	}
	for _, s := range shared {
		if p == s {
			return true
		}
	}
	if strings.HasSuffix(p, "/bin") || strings.HasSuffix(p, "/sbin") {
		return true
	}
	return false
}

// Remove uninstalls the tool by removing either the extracted binary (if extract_to
// is a shared directory) or the entire extract_to directory (if it is tool-specific).
func (a *GitAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if paths, err := managedPaths(mc); err != nil {
		return err
	} else if len(paths) > 0 {
		sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
		for _, path := range paths {
			if err := os.RemoveAll(path); err != nil {
				if runErr := run.CheckResult(run.RunElevated(ctx, rn, "rm", "-rf", "--", path), "git: remove managed path"); runErr != nil {
					return fmt.Errorf("git: remove managed path %s: %w", path, runErr)
				}
			}
		}
		return nil
	}
	extractTo, ok := mc.Config["extract_to"].(string)
	extractTo = config.ExpandHomeDir(extractTo)
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

func managedPaths(mc *config.MethodCandidate) ([]string, error) {
	raw, ok := mc.Config["managed_paths"].([]any)
	if !ok {
		return nil, nil
	}
	home, _ := os.UserHomeDir()
	paths := make([]string, 0, len(raw))
	for _, item := range raw {
		path, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("git: managed_paths entries must be strings")
		}
		path = filepath.Clean(config.ExpandHomeDir(path))
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("git: managed path %q must be absolute after expansion", path)
		}
		if path == filepath.Dir(path) || path == filepath.Clean(home) || isSharedDir(path) {
			return nil, fmt.Errorf("git: refusing unsafe managed path %q", path)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// CanRemove returns true — the adapter can remove installations that were done
// with an extract_to path. Per-method validation (e.g. extract_to is required)
// happens inside Remove() and returns an error if the method config doesn't
// support automated removal.
func (a *GitAdapter) CanRemove() bool { return true }

// Ensure GitAdapter implements exec.AdapterV2 at compile time.
// CheckAvailable assumes availability: git remotes have no cheap local
// index to probe, so an unreachable repository surfaces at clone time.
func (a *GitAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints: the clone target is
// host-independent and build/extract parameters are validated at execution.
func (a *GitAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*GitAdapter)(nil)
