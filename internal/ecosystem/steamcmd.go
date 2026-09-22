package ecosystem

import (
	"context"
	"errors"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// SteamCMDAdapter manages game server installations via Valve's SteamCMD.
// The `steamcmd` binary must already be on PATH. Install runs
// `steamcmd +login anonymous +app_update {app_id} +quit`.
//
// Removal is intentionally manual: steamcmd has no uninstall concept —
// game servers are updated in place and "removal" means deleting the
// server's install directory.
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

// ResolvePlan projects the app ID into the plan identity. SteamCMD has no
// stable, adapter-neutral installed identity to resolve beyond that ID.
func (a *SteamCMDAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("steamcmd: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("steamcmd: tool and method are required")
	}
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return nil, fmt.Errorf("steamcmd: no app id")
	}
	resolved := intent.Clone()
	resolved.Identity.Package = pkg[0]
	resolved.Removal.Supported = false
	return &resolved, nil
}

// Observe cannot establish whether a game server matching the app ID is
// installed: steamcmd reports updates, not a stable installed identity.
func (a *SteamCMDAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	return plan.Observation{
		Presence: plan.PresenceUnknown,
		Detail:   "steamcmd cannot verify installed server identity",
	}, nil
}

// InstallResolved executes only the app ID resolved into the plan. Explicit
// operations have no SteamCMD-specific interpretation and are rejected rather
// than treated as arbitrary commands.
func (a *SteamCMDAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New("steamcmd: runner is required")
	}
	if resolved == nil {
		return errors.New("steamcmd: nil resolved plan")
	}
	if len(resolved.Operations) > 0 {
		return errors.New("steamcmd: resolved operations are unsupported")
	}
	if resolved.Identity.Package == "" {
		return fmt.Errorf("steamcmd: no app id in resolved plan")
	}
	cmd := []string{"steamcmd", "+login", "anonymous", "+app_update", resolved.Identity.Package, "+quit"}
	return run.CheckResult(rn.Run(ctx, cmd[0], cmd[1:]...), "steamcmd: install")
}

func (a *SteamCMDAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("steamcmd: no app id")
	}
	cmd := []string{"steamcmd", "+login", "anonymous", "+app_update", pkg[0], "+quit"}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "steamcmd: install")
}

var _ exec.Adapter = (*SteamCMDAdapter)(nil)
var _ exec.AdapterV2 = (*SteamCMDAdapter)(nil)
