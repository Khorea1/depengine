package ecosystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// SDKManAdapter manages SDKs via SDKMAN (https://sdkman.io). Install is
// `sdk install {candidate}`; check probes whether the candidate directory
// exists under ~/.sdkman/candidates/.
//
// Removal is intentionally manual: `sdk uninstall` requires a candidate plus
// an exact installed version (SDKMAN keeps multiple versions per candidate),
// and depengine does not track which version was installed.
type SDKManAdapter struct{}

func NewSDKManAdapter() *SDKManAdapter {
	return &SDKManAdapter{}
}

func (a *SDKManAdapter) Kind() string { return "sdkman" }

func (a *SDKManAdapter) Available(ctx context.Context, rn run.Runner) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	sdk := filepath.Join(home, ".sdkman", "bin", "sdk")
	if _, err := os.Stat(sdk); err == nil {
		return true
	}
	return run.LookPath(ctx, rn, "sdk")
}

func (a *SDKManAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	candidate := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(candidate) == 0 {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	current := filepath.Join(home, ".sdkman", "candidates", candidate[0], "current")
	_, err = os.Stat(current)
	return err == nil
}

func (a *SDKManAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("sdkman: no package name")
	}
	cmd := []string{"sdk", "install", pkg[0]}
	if v, ok := mc.Config["version"].(string); ok && v != "" {
		cmd = append(cmd, v)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "sdkman: install")
}

var _ exec.Adapter = (*SDKManAdapter)(nil)
