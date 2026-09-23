package localartifactadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
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
	return verifyDestination(resolved, destination)
}

// verifyDestination reports whether the installed destination still matches
// the resolved source content. It is the shared verification core behind
// Check and Observe.
func verifyDestination(resolved localartifact.Resolved, destination string) bool {
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

// ResolvePlan performs the read-only portion of Install — vendored file
// resolution and content hashing — without materializing anything. The
// computed checksum enriches the intent's artifact so InstallResolved and
// state persistence share one content identity. Unlike resolveCandidate, it
// never mutates the method candidate: the plan carries the identity.
func (a *Adapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("local: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("local: tool and method are required")
	}
	localPath, _ := mc.Config["local_path"].(string)
	checksum, _ := mc.Config["checksum"].(string)
	resolved, err := localartifact.Resolve(mc.ProjectRoot, localPath, checksum)
	if err != nil {
		return nil, fmt.Errorf("local: %w", err)
	}
	out := intent.Clone()
	if len(out.Artifacts) == 0 {
		out.Artifacts = []plan.Artifact{resolved.Artifact}
	} else {
		out.Artifacts[0] = resolved.Artifact
	}
	return &out, nil
}

// Observe reports whether the installed destination still matches the
// vendored source content. A present observation carries the verified
// content digest so reconciliation can distinguish content drift (already
// absent here) from an inability to resolve the source (unknown).
func (a *Adapter) Observe(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("local: tool and method are required")
	}
	resolved, destination, err := resolveCandidate(tool, mc)
	if err != nil {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: fmt.Sprintf("local: %v", err)}, nil
	}
	if !verifyDestination(resolved, destination) {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	return plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Digest: resolved.Artifact.Checksum},
		KnownFields: []plan.IdentityField{plan.FieldDigest},
	}, nil
}

// InstallResolved materializes exactly the artifact described by the resolved
// plan. The project-relative path and checksum come exclusively from
// resolved; the method candidate supplies only execution-local parameters
// (project root, install directory). It never re-derives identity from
// mc.Config local_path/checksum.
func (a *Adapter) InstallResolved(_ context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return fmt.Errorf("local: runner is required")
	}
	if resolved == nil {
		return errors.New("local: nil resolved plan")
	}
	if err := validateResolvedOperations(resolved); err != nil {
		return err
	}
	if len(resolved.Artifacts) == 0 || resolved.Artifacts[0].LocalPath == "" {
		return errors.New("local: resolved plan has no concrete local artifact")
	}
	if mc == nil {
		return errors.New("local: method configuration is required")
	}
	artifact := resolved.Artifacts[0]
	settled, err := localartifact.Resolve(mc.ProjectRoot, artifact.LocalPath, artifact.Checksum)
	if err != nil {
		return fmt.Errorf("local: %w", err)
	}
	destination, err := destinationFor(tool, mc, settled.Artifact.Kind)
	if err != nil {
		return fmt.Errorf("local: %w", err)
	}
	if err := localartifact.Install(settled, destination); err != nil {
		return fmt.Errorf("local: install: %w", err)
	}
	return nil
}

// validateResolvedOperations accepts the planner's local operation set — the
// read-only resolve-local-artifact probe plus the single install mutation —
// and rejects anything else, in particular arbitrary commands smuggled in as
// plan operations.
func validateResolvedOperations(resolved *plan.ResolvedInstallPlan) error {
	reject := func() error {
		return errors.New("local: resolved operations are unsupported")
	}
	if len(resolved.Operations) == 0 {
		return reject()
	}
	installs := 0
	for _, op := range resolved.Operations {
		if op.Command != nil || op.ArbitraryCode {
			return reject()
		}
		switch {
		case op.Kind == "install" && op.Effect == plan.EffectMutation && op.Description == "":
			installs++
		case op.Kind == "resolve-local-artifact" && op.Effect == plan.EffectReadOnly:
		default:
			return reject()
		}
	}
	if installs != 1 {
		return reject()
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

// CheckAvailable assumes availability: vendored paths are validated at
// plan time, so a missing file surfaces during resolution.
func (a *Adapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints: vendored artifacts
// are host-independent by construction.
func (a *Adapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*Adapter)(nil)
