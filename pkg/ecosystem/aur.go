package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
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

// Ensure AURAdapter implements exec.Adapter and exec.Remover at compile time.
var _ exec.Adapter = (*AURAdapter)(nil)
var _ exec.Remover = (*AURAdapter)(nil)
