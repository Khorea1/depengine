package lang

import (
	"context"
	"fmt"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// PacstallAdapter manages packages via Pacstall, an AUR-style package
// manager for Debian/Ubuntu. Install requires sudo since it modifies
// system packages.
type PacstallAdapter struct{}

func NewPacstallAdapter() *PacstallAdapter {
	return &PacstallAdapter{}
}

func (a *PacstallAdapter) Kind() string { return "pacstall" }

func (a *PacstallAdapter) Available(ctx context.Context, rn run.Runner) bool {
	res := rn.Run(ctx, "which", "pacstall")
	return res.Err == nil && res.ExitCode == 0
}

func (a *PacstallAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return false
	}
	res := rn.Run(ctx, "pacstall", "-Ci", pkg[0])
	return res.Err == nil && res.ExitCode == 0
}

func (a *PacstallAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("pacstall: no package name")
	}
	cmd := []string{"sudo", "pacstall", "-I", pkg[0]}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("pacstall: install failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("pacstall: install exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

var _ exec.Adapter = (*PacstallAdapter)(nil)
