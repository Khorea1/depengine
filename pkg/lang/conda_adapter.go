package lang

import (
	"context"
	"fmt"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
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

func (a *CondaAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return false
	}
	res := rn.Run(ctx, "conda", "list", pkg[0])
	return res.Err == nil && res.ExitCode == 0 && strings.Contains(string(res.Stdout), pkg[0])
}

func (a *CondaAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
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

var _ exec.Adapter = (*CondaAdapter)(nil)
