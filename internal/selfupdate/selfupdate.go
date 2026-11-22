// Package selfupdate rebuilds the depengine executable from a Git checkout and
// replaces the running binary without destroying the previous installation
// until the new build has been verified.
package selfupdate

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/git"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// DefaultRepo is the upstream source used when the recipe omits one.
const DefaultRepo = "https://github.com/Khorea1/depengine.git"

const (
	buildOutputName = "depengine"
	selfToolName    = "depengine"
	selfRecipeName  = "self_recipe.toml"
)

//go:embed self_recipe.toml
var embeddedSelfRecipe []byte

// Options controls one self-update invocation.
type Options struct {
	Repo       string
	Ref        string
	BinDir     string
	Recipe     string // project-schema fragment; empty uses the embedded recipe
	DryRun     bool
	Check      bool
	Executable string // test override; empty uses os.Executable
}

// Result is the machine-readable outcome of a self-update.
type Result struct {
	Repo            string `json:"repo"`
	Recipe          string `json:"recipe,omitempty"`
	Ref             string `json:"ref,omitempty"`
	Revision        string `json:"revision,omitempty"`
	Target          string `json:"target"`
	CurrentRevision string `json:"current_revision,omitempty"`
	CurrentModified bool   `json:"current_modified,omitempty"`
	BuiltVersion    string `json:"built_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Replaced        bool   `json:"replaced"`
}

type refKind int

const (
	refBranch refKind = iota
	refTag
	refRevision
)

type remoteRef struct {
	Kind     refKind
	Value    string
	Revision string
}

var (
	scpLikeRepoURL = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:`)
	// buildRevision reports the VCS revision stamped into the running binary.
	buildRevision = runningBuildRevision
)

// Run resolves the requested source, rebuilds the binary, and atomically
// replaces the target. Check and DryRun stop after read-only resolution.
func Run(ctx context.Context, rn run.Runner, opts Options) (Result, error) {
	opts, err := normalizeOptions(opts)
	if err != nil {
		return Result{}, err
	}
	selfRecipe, err := readSelfRecipe(opts.Recipe)
	if err != nil {
		return Result{}, err
	}
	if opts.Repo == "" {
		source, sourceErr := selfRecipe.Source()
		if sourceErr != nil {
			return Result{}, sourceErr
		}
		opts.Repo = source
	}
	if err := validateRepoURL(opts.Repo); err != nil {
		return Result{}, err
	}
	if !run.LookPath(ctx, rn, "git") {
		return Result{}, errors.New("self-update: git is required on PATH")
	}
	target, targetDir, targetName, err := resolveTarget(opts, !opts.Check && !opts.DryRun)
	if err != nil {
		return Result{}, err
	}
	remote, err := resolveRemoteRef(ctx, rn, opts.Repo, opts.Ref)
	if err != nil {
		return Result{}, err
	}
	currentRevision, currentModified := buildRevision()
	result := Result{
		Repo:            opts.Repo,
		Recipe:          selfRecipe.Path,
		Ref:             remote.Value,
		Revision:        remote.Revision,
		Target:          target,
		CurrentRevision: currentRevision,
		CurrentModified: currentModified,
		UpdateAvailable: currentRevision == "" || currentModified || !strings.EqualFold(currentRevision, remote.Revision),
	}
	if opts.Check || opts.DryRun {
		return result, nil
	}
	if runtime.GOOS == "windows" {
		return result, errors.New("self-update cannot replace a running executable on Windows; use the release artifact or the source bootstrap script")
	}
	if !run.LookPath(ctx, rn, "go") {
		return result, errors.New("self-update: a Go toolchain is required on PATH")
	}

	version := buildVersion(remote)
	method, err := selfRecipe.Method(map[string]string{"version": version})
	if err != nil {
		return result, err
	}
	method = buildMethod(opts, remote, targetDir, targetName, method)
	tool := &config.Tool{Name: "depengine", Methods: []*config.MethodCandidate{method}}
	adapter := git.NewGitAdapter()
	intent := plan.New("depengine", "git", true)
	resolved, err := adapter.ResolvePlan(ctx, rn, tool, method, &intent)
	if err != nil {
		return result, fmt.Errorf("self-update: resolve source: %w", err)
	}
	if resolved.Identity.Revision != "" {
		result.Revision = resolved.Identity.Revision
	}
	backup, err := backupTarget(target)
	if err != nil {
		return result, err
	}
	installed := false
	defer func() {
		if !installed {
			_ = backup.restore()
		}
	}()
	if err := adapter.InstallResolved(ctx, rn, tool, method, resolved); err != nil {
		return result, fmt.Errorf("self-update: build or install failed; the previous executable was preserved: %w", err)
	}
	builtVersion, err := verifyTarget(ctx, rn, target)
	if err != nil {
		return result, fmt.Errorf("self-update: the new executable did not verify; the previous executable was preserved: %w", err)
	}
	installed = true
	// The verified replacement is in place. A leftover backup is harmless and
	// the next run clears it; do not report a failed update for cleanup alone.
	_ = backup.discard()
	result.BuiltVersion = builtVersion
	result.Replaced = true
	result.UpdateAvailable = false
	return result, nil
}

// targetBackup keeps the verified previous executable available until the
// replacement answers version. The backup is a copy, not a rename, so the old
// binary stays runnable while the new source is cloned and built.
type targetBackup struct {
	target string
	backup string
	active bool
}

func backupTarget(target string) (*targetBackup, error) {
	backup := target + replacementBackupSuffix
	b := &targetBackup{target: target, backup: backup}
	if _, err := os.Lstat(backup); err == nil {
		// A previous run left a backup. If the target is gone, recover it;
		// otherwise the live target won and the stale copy is discarded.
		if _, targetErr := os.Lstat(target); errors.Is(targetErr, os.ErrNotExist) {
			if err := os.Rename(backup, target); err != nil {
				return nil, fmt.Errorf("self-update: recover previous executable %s: %w", target, err)
			}
		} else if err := os.RemoveAll(backup); err != nil {
			return nil, fmt.Errorf("self-update: clear stale backup %s: %w", backup, err)
		}
	}
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return b, nil
		}
		return nil, fmt.Errorf("self-update: inspect target %s: %w", target, err)
	}
	if err := copyExecutable(target, backup); err != nil {
		return nil, fmt.Errorf("self-update: back up %s: %w", target, err)
	}
	b.active = true
	return b, nil
}

func (b *targetBackup) restore() error {
	if b == nil || !b.active {
		return nil
	}
	b.active = false
	if err := os.Rename(b.backup, b.target); err != nil {
		return fmt.Errorf("self-update: restore previous executable %s: %w", b.target, err)
	}
	return nil
}

func (b *targetBackup) discard() error {
	if b == nil || !b.active {
		return nil
	}
	b.active = false
	if err := os.Remove(b.backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is the resolved running executable.
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm()) // #nosec G304 -- dst is a sibling of the running executable.
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

const replacementBackupSuffix = ".depengine-old"

// selfRecipeFile is one loaded project-schema fragment. The bytes are kept so
// the same recipe can be parsed with and without the resolved revision in its
// build command.
type selfRecipeFile struct {
	Path string
	raw  []byte
}

func readSelfRecipe(path string) (*selfRecipeFile, error) {
	if path == "" {
		return &selfRecipeFile{Path: "builtin:" + selfRecipeName, raw: embeddedSelfRecipe}, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("self-update: resolve --recipe: %w", err)
	}
	raw, err := os.ReadFile(abs) // #nosec G304 -- the operator selects the recipe path.
	if err != nil {
		return nil, fmt.Errorf("self-update: read recipe %s: %w", abs, err)
	}
	return &selfRecipeFile{Path: abs, raw: raw}, nil
}

// Source returns the recipe's default repository URL without expanding
// host-specific placeholders.
func (r *selfRecipeFile) Source() (string, error) {
	method, err := r.parse(nil)
	if err != nil {
		return "", err
	}
	source, _ := method.Config["url"].(string)
	return source, nil
}

// Method parses the recipe into its single git candidate. facts may supply
// {version} so the recipe controls the build command while the revision is
// still resolved at runtime.
func (r *selfRecipeFile) Method(facts map[string]string) (*config.MethodCandidate, error) {
	return r.parse(facts)
}

func (r *selfRecipeFile) parse(facts map[string]string) (*config.MethodCandidate, error) {
	schema, err := config.ParseProjectSchemaBytes(r.Path, r.raw, facts)
	if err != nil {
		return nil, fmt.Errorf("self-update: invalid recipe %s: %w", r.Path, err)
	}
	if len(schema.Tools) != 1 {
		return nil, fmt.Errorf("self-update: recipe %s must declare exactly one tool, got %d", r.Path, len(schema.Tools))
	}
	tool := schema.Tools[selfToolName]
	if tool == nil {
		return nil, fmt.Errorf("self-update: recipe %s must declare [tools.%s]", r.Path, selfToolName)
	}
	if len(tool.Requires) > 0 || len(tool.RequiresWhen) > 0 || len(tool.PreInstall) > 0 || len(tool.PostInstall) > 0 {
		return nil, fmt.Errorf("self-update: recipe %s must not declare dependencies or tool hooks", r.Path)
	}
	if len(tool.Methods) != 1 || tool.Methods[0] == nil || tool.Methods[0].Kind != "git" {
		return nil, fmt.Errorf("self-update: recipe %s must declare exactly one git method", r.Path)
	}
	method := tool.Methods[0]
	if method.PreInstall != nil || method.PostInstall != nil {
		return nil, fmt.Errorf("self-update: recipe %s must not declare lifecycle hooks", r.Path)
	}
	if method.SecretRef != nil || method.ChecksumSecretRef != nil || method.SignatureSecretRef != nil || len(method.Sources) > 0 {
		return nil, fmt.Errorf("self-update: recipe %s must not declare credentials or custom sources", r.Path)
	}
	if method.When != nil && !method.When.IsZero() {
		return nil, fmt.Errorf("self-update: recipe %s must not restrict the host; self-update is a cross-platform operation", r.Path)
	}
	if _, ok := method.Config["build"]; !ok {
		return nil, fmt.Errorf("self-update: recipe %s must declare git.build", r.Path)
	}
	source, _ := method.Config["url"].(string)
	if source == "" {
		return nil, fmt.Errorf("self-update: recipe %s must declare git.url or pass --repo", r.Path)
	}
	if err := validateArtifactFields(method.Config); err != nil {
		return nil, fmt.Errorf("self-update: recipe %s: %w", r.Path, err)
	}
	return method, nil
}

func validateArtifactFields(cfg map[string]any) error {
	artifact, _ := cfg["artifact"].(string)
	if artifact == "" {
		return errors.New("git.artifact must name the built executable")
	}
	if artifact != filepath.Base(artifact) || artifact == "." || artifact == ".." {
		return fmt.Errorf("git.artifact %q must be a file name in the checkout root", artifact)
	}
	binary, _ := cfg["binary"].(string)
	if binary == "" {
		return errors.New("git.binary must name the installed executable")
	}
	if binary != filepath.Base(binary) || binary == "." || binary == ".." {
		return fmt.Errorf("git.binary %q must be a file name", binary)
	}
	return nil
}

func normalizeOptions(opts Options) (Options, error) {
	if strings.TrimSpace(opts.Repo) != opts.Repo || strings.ContainsAny(opts.Repo, "\x00\r\n") || strings.HasPrefix(opts.Repo, "-") {
		return Options{}, errors.New("self-update: --repo is padded, contains a control character, or starts with '-'")
	}
	if strings.TrimSpace(opts.Ref) != opts.Ref || strings.ContainsAny(opts.Ref, "\x00\r\n") || strings.HasPrefix(opts.Ref, "-") {
		return Options{}, errors.New("self-update: --ref is padded, contains a control character, or starts with '-'")
	}
	if strings.TrimSpace(opts.Recipe) != opts.Recipe || strings.ContainsAny(opts.Recipe, "\x00\r\n") || strings.HasPrefix(opts.Recipe, "-") {
		return Options{}, errors.New("self-update: --recipe must be a path without surrounding whitespace or control characters")
	}
	if strings.TrimSpace(opts.BinDir) != opts.BinDir || strings.ContainsAny(opts.BinDir, "\x00\r\n") {
		return Options{}, errors.New("self-update: --bin-dir contains surrounding whitespace or a control character")
	}
	return opts, nil
}

// validateRepoURL keeps the build source on a transport that authenticates the
// host. Local absolute paths are allowed for development and tests.
func validateRepoURL(repo string) error {
	if filepath.IsAbs(repo) {
		return nil
	}
	if scpLikeRepoURL.MatchString(repo) {
		return nil
	}
	parsed, err := url.Parse(repo)
	if err != nil || parsed.Scheme == "" {
		return errors.New("self-update: --repo must be an absolute path, an SSH URL, or an HTTPS URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("self-update: HTTPS --repo must name a host and contain no credentials, query, or fragment")
		}
	case "ssh":
		if parsed.Host == "" {
			return errors.New("self-update: SSH --repo must name a host")
		}
	case "file":
	default:
		return fmt.Errorf("self-update: unsupported --repo scheme %q; use HTTPS, SSH, file, or an absolute path", parsed.Scheme)
	}
	return nil
}

// resolveTarget chooses the executable to replace. create selects whether a
// missing destination directory may be created: read-only --check and
// --dry-run calls must not mutate the filesystem.
func resolveTarget(opts Options, create bool) (target, targetDir, targetName string, err error) {
	running := opts.Executable
	if running == "" {
		running, err = os.Executable()
		if err != nil {
			return "", "", "", fmt.Errorf("self-update: locate the running executable: %w", err)
		}
	}
	running, err = filepath.EvalSymlinks(running)
	if err != nil {
		return "", "", "", fmt.Errorf("self-update: resolve the running executable: %w", err)
	}
	if opts.BinDir == "" {
		target = running
	} else {
		dir, absErr := filepath.Abs(opts.BinDir)
		if absErr != nil {
			return "", "", "", fmt.Errorf("self-update: resolve --bin-dir: %w", absErr)
		}
		target = filepath.Join(dir, executableName())
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, linkErr := filepath.EvalSymlinks(target)
			if linkErr != nil {
				return "", "", "", fmt.Errorf("self-update: resolve target symlink %s: %w", target, linkErr)
			}
			target = resolved
			if info, statErr = os.Lstat(target); statErr != nil {
				return "", "", "", fmt.Errorf("self-update: inspect resolved target %s: %w", target, statErr)
			}
		}
		if info.IsDir() {
			return "", "", "", fmt.Errorf("self-update: target %s is a directory", target)
		}
		if !info.Mode().IsRegular() {
			return "", "", "", fmt.Errorf("self-update: target %s is not a regular file", target)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", "", "", fmt.Errorf("self-update: inspect target %s: %w", target, statErr)
	}
	targetDir = filepath.Dir(target)
	if create && opts.BinDir != "" {
		if err := os.MkdirAll(targetDir, 0o755); err != nil { // #nosec G301 -- the operator selected this bin directory.
			return "", "", "", fmt.Errorf("self-update: create %s: %w", targetDir, err)
		}
	}
	return target, targetDir, filepath.Base(target), nil
}

// buildMethod applies the command-line source, revision, and destination to the
// recipe's git method. The recipe owns the build and artifact contract; flags
// only replace fields whose values cannot be known before remote resolution.
func buildMethod(opts Options, remote remoteRef, targetDir, targetName string, method *config.MethodCandidate) *config.MethodCandidate {
	cfg := make(map[string]any, len(method.Config)+3)
	for key, value := range method.Config {
		cfg[key] = value
	}
	cfg["url"] = opts.Repo
	delete(cfg, "branch")
	delete(cfg, "tag")
	delete(cfg, "rev")
	switch remote.Kind {
	case refBranch:
		cfg["branch"] = remote.Value
	case refTag:
		cfg["tag"] = remote.Value
	case refRevision:
		cfg["rev"] = remote.Value
	}
	cfg["extract_to"] = targetDir
	cfg["binary"] = targetName
	return &config.MethodCandidate{Kind: "git", Config: cfg}
}

func buildVersion(remote remoteRef) string {
	if remote.Kind == refTag {
		return remote.Value
	}
	if len(remote.Revision) >= 12 {
		return remote.Revision[:12]
	}
	return remote.Revision
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return buildOutputName + ".exe"
	}
	return buildOutputName
}

func resolveRemoteRef(ctx context.Context, rn run.Runner, repo, ref string) (remoteRef, error) {
	if ref == "" {
		result := rn.Run(ctx, "git", "ls-remote", "--symref", "--", repo, "HEAD")
		if err := run.CheckResultCompleteOutput(result, "self-update: resolve default branch"); err != nil {
			return remoteRef{}, err
		}
		branch, revision := parseDefaultHead(string(result.Stdout))
		if revision == "" {
			return remoteRef{}, errors.New("self-update: remote HEAD did not resolve to a commit")
		}
		return remoteRef{Kind: refBranch, Value: branch, Revision: revision}, nil
	}
	if isFullCommitID(ref) {
		return remoteRef{Kind: refRevision, Value: ref, Revision: ref}, nil
	}
	result := rn.Run(ctx, "git", "ls-remote", "--", repo, "refs/heads/"+ref, "refs/tags/"+ref, "refs/tags/"+ref+"^{}")
	if err := run.CheckResultCompleteOutput(result, "self-update: resolve ref"); err != nil {
		return remoteRef{}, err
	}
	branch, tag, peeled := parseRemoteRefs(string(result.Stdout))
	switch {
	case branch != "":
		return remoteRef{Kind: refBranch, Value: ref, Revision: branch}, nil
	case peeled != "":
		return remoteRef{Kind: refTag, Value: ref, Revision: peeled}, nil
	case tag != "":
		return remoteRef{Kind: refTag, Value: ref, Revision: tag}, nil
	default:
		return remoteRef{}, fmt.Errorf("self-update: ref %q was not found in %s", ref, repo)
	}
}

func parseDefaultHead(output string) (branch, revision string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "ref: refs/heads/") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				branch = strings.TrimPrefix(fields[1], "refs/heads/")
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "HEAD" && isFullCommitID(fields[0]) {
			revision = fields[0]
		}
	}
	return branch, revision
}

func parseRemoteRefs(output string) (branch, tag, peeled string) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || !isFullCommitID(fields[0]) {
			continue
		}
		switch {
		case strings.HasSuffix(fields[1], "^{}"):
			peeled = fields[0]
		case strings.HasPrefix(fields[1], "refs/heads/"):
			branch = fields[0]
		case strings.HasPrefix(fields[1], "refs/tags/"):
			tag = fields[0]
		}
	}
	return branch, tag, peeled
}

func isFullCommitID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F' {
			continue
		}
		return false
	}
	return true
}

func runningBuildRevision() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if !isFullCommitID(revision) {
		return "", modified == "true"
	}
	return revision, modified == "true"
}

func verifyTarget(ctx context.Context, rn run.Runner, target string) (string, error) {
	result := rn.Run(ctx, target, "version")
	if err := run.CheckResultCompleteOutput(result, "self-update: verify the new executable"); err != nil {
		return "", err
	}
	line := strings.TrimSpace(strings.SplitN(string(result.Stdout), "\n", 2)[0])
	if !strings.HasPrefix(line, "depengine ") {
		return "", fmt.Errorf("self-update: %s did not report a depengine version", target)
	}
	return line, nil
}
