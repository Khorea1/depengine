package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

// CargoAdapter extends BaseAdapter with git-repo support: when
// mc.Config["git"] is set, it uses `cargo install --git {url}`
// instead of `cargo install {pkg}`.
type CargoAdapter struct {
	*BaseAdapter
}

// NewCargoAdapter creates a cargo adapter with git-repo support.
func NewCargoAdapter() *CargoAdapter {
	return &CargoAdapter{
		BaseAdapter: NewBaseAdapter(Configs["cargo"]),
	}
}

// Install checks for a git URL in config and switches to --git mode.
func (a *CargoAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if gitURL, ok := mc.Config["git"].(string); ok && gitURL != "" {
		cmd := []string{"cargo", "install", "--git", gitURL}
		if pkg, ok := mc.Config["pkg"].(string); ok && pkg != "" {
			cmd = append(cmd, pkg)
		}
		res := rn.Run(ctx, cmd[0], cmd[1:]...)
		if res.Err != nil {
			return fmt.Errorf("cargo --git: install failed: %w", res.Err)
		}
		if res.ExitCode != 0 {
			stderr := strings.TrimSpace(string(res.Stderr))
			return fmt.Errorf("cargo --git: install exited %d: %s", res.ExitCode, stderr)
		}
		return nil
	}
	return a.BaseAdapter.Install(ctx, rn, tool, mc)
}

// Ensure CargoAdapter implements exec.Adapter.
var _ exec.Adapter = (*CargoAdapter)(nil)
