package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// CondaAdapter implements the Adapter interface for conda packages.
type CondaAdapter struct{}

// NewCondaAdapter creates a new CondaAdapter.
func NewCondaAdapter() *CondaAdapter {
	return &CondaAdapter{}
}

func (a *CondaAdapter) Kind() string { return "conda" }

func (a *CondaAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "conda")
}

func (a *CondaAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return false
	}
	res := rn.Run(ctx, "conda", "list", pkg[0])
	return res.Err == nil && res.ExitCode == 0 && hasWord(string(res.Stdout), pkg[0])
}

func (a *CondaAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("conda: no package name")
	}
	res := rn.Run(ctx, "conda", "install", "-y", pkg[0])
	if res.Err != nil {
		return fmt.Errorf("conda: install failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("conda: install exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

// CanRemove reports whether this adapter supports removal.
func (a *CondaAdapter) CanRemove() bool { return true }

// Remove uninstalls a package from the active conda environment.
//
// Env-scoping caveat: `conda remove` (like the Install's `conda install`)
// operates on the currently active environment (base unless a named env is
// activated). Packages installed into a named env via `conda install -n <env>`
// would need `conda remove -n <env>` here; depengine does not track which env
// a tool was installed into, so removal targets the active one.
func (a *CondaAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("conda: no package name")
	}
	res := rn.Run(ctx, "conda", "remove", "-y", pkg[0])
	if res.Err != nil {
		return fmt.Errorf("conda: remove failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("conda: remove exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

var _ exec.Adapter = (*CondaAdapter)(nil)
var _ exec.Remover = (*CondaAdapter)(nil)
