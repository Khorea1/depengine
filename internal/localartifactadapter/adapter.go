package localartifactadapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/localartifact"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// Adapter installs project-vendored files without network access.
type Adapter struct{}

func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Kind() string { return "local" }

func (a *Adapter) Available(context.Context, run.Runner) bool { return true }

func (a *Adapter) Check(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	resolved, destination, err := resolveCandidate(tool, mc)
	if err != nil {
		return false
	}
	switch resolved.Artifact.Kind {
	case plan.ArtifactRaw:
		err := localartifact.VerifyRegularFileState(destination, resolved.Artifact.Checksum, resolved.Mode)
		return err == nil
	case plan.ArtifactArchive:
		err := localartifact.VerifyArchiveChecksum(destination, resolved.Artifact.Checksum)
		return err == nil
	default:
		return false
	}
}

func (a *Adapter) Install(_ context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("local: runner is required")
	}
	resolved, destination, err := resolveCandidate(tool, mc)
	if err != nil {
		return fmt.Errorf("local: %w", err)
	}
	if err := localartifact.Install(resolved, destination); err != nil {
		return fmt.Errorf("local: install: %w", err)
	}
	return nil
}

func (a *Adapter) Remove(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	_, destination, err := candidateDestination(tool, mc)
	if err != nil {
		return fmt.Errorf("local: %w", err)
	}
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("local: remove %s: %w", destination, err)
	}
	return nil
}

func (a *Adapter) CanRemove() bool { return true }

func resolveCandidate(tool *config.Tool, mc *config.MethodCandidate) (localartifact.Resolved, string, error) {
	if mc == nil {
		return localartifact.Resolved{}, "", fmt.Errorf("missing method configuration")
	}
	localPath, _ := mc.Config["local_path"].(string)
	if localPath == "" {
		return localartifact.Resolved{}, "", fmt.Errorf("local_path is required")
	}
	if mc.ProjectRoot == "" || !filepath.IsAbs(mc.ProjectRoot) {
		return localartifact.Resolved{}, "", fmt.Errorf("project root is unavailable for local_path %q", localPath)
	}
	checksum, _ := mc.Config["checksum"].(string)
	resolved, err := localartifact.Resolve(mc.ProjectRoot, localPath, checksum)
	if err != nil {
		return localartifact.Resolved{}, "", err
	}
	if mc.Config == nil {
		mc.Config = make(map[string]any)
	}
	// Freeze the bytes resolved during this candidate attempt. This closes the
	// check→install TOCTOU window when checksum was omitted and also gives state
	// persistence a portable content identity without a machine-specific path.
	if checksum == "" {
		mc.Config["checksum"] = resolved.Artifact.Checksum
	}
	destination, err := destinationFor(tool, mc, resolved.Artifact.Kind)
	if err != nil {
		return localartifact.Resolved{}, "", err
	}
	return resolved, destination, nil
}

func candidateDestination(tool *config.Tool, mc *config.MethodCandidate) (plan.ArtifactKind, string, error) {
	if mc == nil {
		return "", "", fmt.Errorf("missing method configuration")
	}
	localPath, _ := mc.Config["local_path"].(string)
	if localPath == "" {
		return "", "", fmt.Errorf("local_path is required")
	}
	portable, err := plan.NormalizeProjectPath(localPath)
	if err != nil {
		return "", "", err
	}
	artifactKind, err := localartifact.ClassifyProjectPath(portable)
	if err != nil {
		return "", "", err
	}
	destination, err := destinationFor(tool, mc, artifactKind)
	return artifactKind, destination, err
}

func destinationFor(tool *config.Tool, mc *config.MethodCandidate, kind plan.ArtifactKind) (string, error) {
	if tool == nil || tool.Name == "" || filepath.Base(tool.Name) != tool.Name {
		return "", fmt.Errorf("tool name must be a single path component")
	}
	configured, _ := mc.Config["install_dir"].(string)
	configured = config.ExpandHomeDir(configured)
	base := configured
	if base == "" {
		switch kind {
		case plan.ArtifactRaw:
			base = config.ExpandHomeDir("~/.local/bin")
		case plan.ArtifactArchive:
			base = config.ExpandHomeDir("~/.local/opt")
		default:
			return "", fmt.Errorf("unsupported local artifact kind %q", kind)
		}
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("install_dir must resolve to an absolute path")
	}

	// The adapter owns exactly one child of install_dir. In particular, archive
	// removal never recursively deletes the configured parent directory.
	destination := filepath.Join(filepath.Clean(base), tool.Name)
	if filepath.Dir(destination) != filepath.Clean(base) {
		return "", fmt.Errorf("tool destination escapes install_dir")
	}
	return destination, nil
}

var _ exec.Adapter = (*Adapter)(nil)
var _ exec.Remover = (*Adapter)(nil)
