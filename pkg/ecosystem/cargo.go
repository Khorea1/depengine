package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
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

// InstalledVersion reports the installed crate version by querying
// `cargo install --list`. Best-effort: returns "" when the crate is not
// listed or the query fails.
func (a *CargoAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	pkg := tool.Name
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		pkg = p
	}
	res := rn.Run(ctx, "cargo", "install", "--list")
	if res.Err != nil || res.ExitCode != 0 {
		return "", nil
	}
	// `cargo install --list` prints one line per crate: "  bat v0.24.0:".
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != pkg {
			continue
		}
		ver := strings.TrimSuffix(fields[1], ":")
		ver = strings.TrimPrefix(ver, "v")
		if ver != "" {
			return ver, nil
		}
	}
	return "", nil
}

// Ensure CargoAdapter implements exec.Adapter.
var _ exec.Adapter = (*CargoAdapter)(nil)
