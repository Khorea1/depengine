package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
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

// Always returns false because steamcmd is inherently stateful — it updates
// game servers to the latest version on every run. Skipping the check means
// we always ensure the server is current, which is the expected behavior.

func (a *SteamCMDAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	return false
}

func (a *SteamCMDAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
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


var _ exec.Adapter = (*SteamCMDAdapter)(nil)
