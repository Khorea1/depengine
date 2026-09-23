package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// SDKManAdapter manages SDKs via SDKMAN (https://sdkman.io). Install is
// `sdk install {candidate}`; check probes whether the candidate directory
// exists under ~/.sdkman/candidates/.
//
// Removal is intentionally manual: `sdk uninstall` requires a candidate plus
// an exact installed version (SDKMAN keeps multiple versions per candidate),
// and depengine does not track which version was installed.
type SDKManAdapter struct{}

func NewSDKManAdapter() *SDKManAdapter {
	return &SDKManAdapter{}
}

func (a *SDKManAdapter) Kind() string { return "sdkman" }

func (a *SDKManAdapter) Available(ctx context.Context, rn run.Runner) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	sdk := filepath.Join(home, ".sdkman", "bin", "sdk")
	if _, err := os.Stat(sdk); err == nil {
		return true
	}
	return run.LookPath(ctx, rn, "sdk")
}

func (a *SDKManAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	candidate := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(candidate) == 0 {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	base := filepath.Join(home, ".sdkman", "candidates", candidate[0])
	if version, ok := mc.Config["version"].(string); ok && version != "" {
		// Exact-version intent is satisfied only when that version is actually
		// installed. Checking only the `current` symlink would silently accept
		// a different SDK version.
		_, err = os.Stat(filepath.Join(base, version))
		return err == nil
	}
	_, err = os.Stat(filepath.Join(base, "current"))
	return err == nil
}

// InstalledVersion reports the SDKMAN candidate version represented by the
// current method intent. Exact-version installs can be reported without
// invoking SDKMAN because their owned candidate directory is deterministic.
// For unpinned intent, resolve the `current` symlink when possible; failure is
// deliberately non-fatal because state persistence treats version reporting as
// best-effort.
func (a *SDKManAdapter) InstalledVersion(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	candidate := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(candidate) == 0 || candidate[0] == "" {
		return "", fmt.Errorf("sdkman: no package name")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := filepath.Join(home, ".sdkman", "candidates", candidate[0])
	if version, ok := mc.Config["version"].(string); ok && version != "" {
		if _, err := os.Stat(filepath.Join(base, version)); err != nil {
			return "", err
		}
		return version, nil
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(base, "current"))
	if err != nil {
		return "", err
	}
	version := filepath.Base(resolved)
	if version == "." || version == string(filepath.Separator) || version == "" {
		return "", fmt.Errorf("sdkman: could not determine current version for %s", candidate[0])
	}
	return version, nil
}

func (a *SDKManAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("sdkman: no package name")
	}
	return a.install(ctx, rn, pkg[0], sdkmanVersion(mc))
}

func sdkmanVersion(mc *config.MethodCandidate) string {
	if mc != nil {
		if version, ok := mc.Config["version"].(string); ok {
			return version
		}
	}
	return ""
}

func (a *SDKManAdapter) install(ctx context.Context, rn run.Runner, candidate, version string) error {
	if rn == nil {
		return errors.New("sdkman: runner is required")
	}
	if candidate == "" {
		return fmt.Errorf("sdkman: no package name")
	}
	cmd := []string{"sdk", "install", candidate}
	if version != "" {
		cmd = append(cmd, version)
	}
	return run.CheckResult(rn.Run(ctx, cmd[0], cmd[1:]...), "sdkman: install")
}

func (a *SDKManAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("sdkman: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("sdkman: tool and method are required")
	}
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return nil, errors.New("sdkman: no package name")
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		return nil, errors.New("sdkman: no package name in plan intent")
	}
	return &resolved, nil
}

func (a *SDKManAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("sdkman: tool and method are required")
	}
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	if !a.Check(ctx, rn, tool, mc) {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	observation := plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: pkg[0]}, KnownFields: []plan.IdentityField{plan.FieldPackage}}
	if version := sdkmanVersion(mc); version != "" {
		observation.Identity.Version = version
		observation.KnownFields = append(observation.KnownFields, plan.FieldVersion)
	}
	return observation, nil
}

func (a *SDKManAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil {
		return errors.New("sdkman: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("sdkman", resolved); err != nil {
		return err
	}
	return a.install(ctx, rn, resolved.Identity.Package, resolved.Identity.Version)
}

// CheckAvailable assumes availability: sdkman resolves names at install
// time, so an unknown candidate surfaces there.
func (a *SDKManAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *SDKManAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

// Remove is unsupported: sdkman removals are user-managed, so removal
// stays manual.
func (a *SDKManAdapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return errors.New("sdkman: remove is not supported — remove manually")
}

// CanRemove always reports false; see Remove.
func (a *SDKManAdapter) CanRemove() bool { return false }

var _ exec.AdapterV2 = (*SDKManAdapter)(nil)
var _ exec.Versioner = (*SDKManAdapter)(nil)
