package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// PacstallAdapter manages packages via Pacstall, an AUR-style package
// manager for Debian/Ubuntu. Install requires sudo since it modifies
// system packages.
//
// Removal is intentionally manual: `pacstall -R {pkg}` exists but requires
// the same elevation as install and interactive confirmation; it is left to
// the user to keep the matrix conservative.
type PacstallAdapter struct{}

func NewPacstallAdapter() *PacstallAdapter {
	return &PacstallAdapter{}
}

func (a *PacstallAdapter) Kind() string { return "pacstall" }

func (a *PacstallAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if !run.LookPath(ctx, rn, "pacstall") {
		return false
	}
	// Elevation is needed unless already root.
	// Use the shared elevation detector instead of hardcoding "sudo".
	if isElevated() {
		return true
	}
	// ElevationMethod probes sudo -n and pkexec, caching the result.
	return run.ElevationMethod() != ""
}

func (a *PacstallAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return false
	}
	res := rn.Run(ctx, "pacstall", "-Ci", pkg[0])
	return res.Err == nil && res.ExitCode == 0
}

// ResolvePlan records the package selected by the Pacstall method. Pacstall
// has no adapter-neutral removal support, so the resolved plan keeps removal
// disabled even though the package manager exposes a removal command.
func (a *PacstallAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("pacstall: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("pacstall: tool and method are required")
	}
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return nil, errors.New("pacstall: no package name")
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		return nil, errors.New("pacstall: no package name in plan intent")
	}
	return &resolved, nil
}

// Observe uses Pacstall's existing non-mutating inspection command so the V2
// path reports the same installed-state result as Check.
func (a *PacstallAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return plan.Observation{}, errors.New("pacstall: no package name")
	}
	res := rn.Run(ctx, "pacstall", "-Ci", pkg[0])
	if res.Err == nil && res.ExitCode == 0 {
		return plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: pkg[0]}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, nil
	}
	return plan.Observation{Presence: plan.PresenceAbsent, Identity: plan.ObservedIdentity{Package: pkg[0]}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, nil
}

// InstallResolved executes only the package selected during resolution.
// Explicit operations have no Pacstall-specific interpretation and are
// rejected rather than treated as arbitrary commands.
func (a *PacstallAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New("pacstall: runner is required")
	}
	if resolved == nil {
		return errors.New("pacstall: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("pacstall", resolved); err != nil {
		return err
	}
	if resolved.Identity.Package == "" {
		return errors.New("pacstall: no package name in resolved plan")
	}
	return a.installPackage(ctx, rn, resolved.Identity.Package)
}

func (a *PacstallAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("pacstall: no package name")
	}

	return a.installPackage(ctx, rn, pkg[0])
}

func (a *PacstallAdapter) installPackage(ctx context.Context, rn run.Runner, packageName string) error {
	// Use elevation (sudo/pkexec) if not running as root.
	// ElevationPrefix detects the best method at runtime.
	var cmd []string
	if isElevated() {
		cmd = []string{"pacstall", "-I", packageName}
	} else if prefix := run.ElevationPrefix(); prefix != nil {
		cmd = append(append([]string(nil), prefix...), "pacstall", "-I", packageName)
	} else {
		// No working elevation — try with bare sudo anyway for a clear error.
		cmd = []string{"sudo", "pacstall", "-I", packageName}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "pacstall: install")
}

// CheckAvailable assumes availability: pacstall resolves names at install
// time, so an unknown package surfaces there.
func (a *PacstallAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *PacstallAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

// Remove is unsupported: pacstall has no reliable uninstall primitive, so
// removal stays manual.
func (a *PacstallAdapter) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return errors.New("pacstall: remove is not supported — remove manually")
}

// CanRemove always reports false; see Remove.
func (a *PacstallAdapter) CanRemove() bool { return false }

var _ exec.Adapter = (*PacstallAdapter)(nil)
var _ exec.AdapterV2 = (*PacstallAdapter)(nil)

// isElevated reports whether the current process is running with
// root privileges (EUID 0). This is not testable via Runner, so it's
// kept as a minimal wrapper for easy replacement in tests.
var isElevated = func() bool {
	return os.Geteuid() == 0
}
