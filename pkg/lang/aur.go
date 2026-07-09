package lang

import (
	"context"
	"fmt"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// AURAdapter handles packages from the Arch User Repository via a helper
// like paru or yay. The helper binary is configurable per schema.
type AURAdapter struct {
	helper string
}

// NewAURAdapter creates an AUR adapter that uses the given helper binary.
func NewAURAdapter(helper string) *AURAdapter {
	return &AURAdapter{helper: helper}
}

func (a *AURAdapter) Kind() string { return "aur" }

func (a *AURAdapter) Available(ctx context.Context, rn run.Runner) bool {
	res := rn.Run(ctx, "which", a.helper)
	return res.Err == nil && res.ExitCode == 0
}

func (a *AURAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	pkg := pkgName(tool, mc)
	cmd := []string{a.helper, "-Qi", pkg}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

func (a *AURAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	pkg := pkgName(tool, mc)
	cmd := []string{a.helper, "-S", "--noconfirm", pkg}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("aur: install failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("aur: install exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

// pkgName extracts the package name, using either mc.Config["pkg"] or the
// tool's own name as fallback.
func pkgName(tool *schema.Tool, mc *schema.MethodCandidate) string {
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		return p
	}
	return tool.Name
}

// Ensure AURAdapter implements exec.Adapter at compile time.
var _ exec.Adapter = (*AURAdapter)(nil)
