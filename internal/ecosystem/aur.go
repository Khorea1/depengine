package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// AURAdapter handles packages from the Arch User Repository via a helper
// like paru or yay. The helper binary is configurable per schema and must be
// trusted, as it is executed directly.
type AURAdapter struct{ helper string }

// NewAURAdapter creates an AURAdapter that uses the given helper binary.
// The helper string is trimmed; empty helper disables the adapter.
func NewAURAdapter(helper string) *AURAdapter {
	return &AURAdapter{helper: strings.TrimSpace(helper)}
}

func (a *AURAdapter) Kind() string { return "aur" }

func (a *AURAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if a.helper == "" {
		return false
	}
	return run.LookPath(ctx, rn, a.helper)
}

// pkgName resolves the package name from "{pkg}" substitution.
// Returns empty string and false if no package name is available.
func (a *AURAdapter) pkgName(tool *config.Tool, mc *config.MethodCandidate) (string, bool) {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return "", false
	}
	return pkg[0], true
}

func (a *AURAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	name, ok := a.pkgName(tool, mc)
	if !ok {
		return false
	}
	res := rn.Run(ctx, a.helper, "-Qi", name)
	return res.Err == nil && res.ExitCode == 0
}

func (a *AURAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("aur: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("aur: tool and method are required")
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		name, ok := a.pkgName(tool, mc)
		if !ok {
			return nil, errors.New("aur: no package name")
		}
		resolved.Identity.Package = name
	}
	return &resolved, nil
}

func (a *AURAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	name, ok := a.pkgName(tool, mc)
	if !ok {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "aur: no package name"}, errors.New("aur: no package name")
	}
	res := rn.Run(ctx, a.helper, "-Qi", name)
	identity := plan.ObservedIdentity{Package: name}
	known := []plan.IdentityField{plan.FieldPackage}
	if res.Err != nil {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: "aur: package query failed", Identity: identity, KnownFields: known}, res.Err
	}
	if res.ExitCode == 0 {
		return plan.Observation{Presence: plan.PresencePresent, Identity: identity, KnownFields: known}, nil
	}
	return plan.Observation{Presence: plan.PresenceAbsent, Identity: identity, KnownFields: known}, nil
}

func (a *AURAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil {
		return errors.New("aur: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("aur", resolved); err != nil {
		return err
	}
	if resolved.Identity.Package == "" {
		return errors.New("aur: no package name in resolved plan")
	}
	res := rn.Run(ctx, a.helper, "-S", "--noconfirm", resolved.Identity.Package)
	return run.CheckResult(res, "aur: install")
}

func (a *AURAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	name, ok := a.pkgName(tool, mc)
	if !ok {
		return fmt.Errorf("aur: no package name")
	}
	res := rn.Run(ctx, a.helper, "-S", "--noconfirm", name)
	return run.CheckResult(res, "aur: install")
}

// CanRemove reports whether this adapter supports removal. AUR helpers
// (paru/yay) pass pacman operations through, so -Rns works.
func (a *AURAdapter) CanRemove() bool { return true }

// Remove uninstalls a package via the AUR helper's pacman passthrough.
// --noconfirm avoids an interactive confirmation prompt, matching Install.
// -Rns removes the package, unneeded dependencies, and .pacsave files — matching pacman semantics.
func (a *AURAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	name, ok := a.pkgName(tool, mc)
	if !ok {
		return fmt.Errorf("aur: no package name")
	}
	res := rn.Run(ctx, a.helper, "-Rns", "--noconfirm", name)
	return run.CheckResult(res, "aur: remove")
}

// Ensure AURAdapter implements exec.AdapterV2 and exec.Remover at compile time.
// CheckAvailable assumes availability: AUR helpers resolve names against
// the AUR at install time, so an unknown package surfaces there.
func (a *AURAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints beyond adapter
// availability.
func (a *AURAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*AURAdapter)(nil)
