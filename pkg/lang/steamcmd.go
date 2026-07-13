package lang

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// SteamCMDAdapter manages game server installations via Valve's SteamCMD.
// The `steamcmd` binary must already be on PATH. Install runs
// `steamcmd +login anonymous +app_update {app_id} +quit`.
type SteamCMDAdapter struct{}

func NewSteamCMDAdapter() *SteamCMDAdapter {
	return &SteamCMDAdapter{}
}

func (a *SteamCMDAdapter) Kind() string { return "steamcmd" }

func (a *SteamCMDAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "steamcmd")
}

func (a *SteamCMDAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	return false
}

func (a *SteamCMDAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("steamcmd: no app id")
	}
	cmd := []string{"steamcmd", "+login", "anonymous", "+app_update", pkg[0], "+quit"}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("steamcmd: install failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("steamcmd: install exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

func installDirFromConfig(mc *schema.MethodCandidate, pkg string) string {
	if d, ok := mc.Config["dir"].(string); ok && d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".steam", "steamapps", "common", pkg)
}

var _ exec.Adapter = (*SteamCMDAdapter)(nil)
