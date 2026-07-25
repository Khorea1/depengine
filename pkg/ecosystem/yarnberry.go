package ecosystem

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

// YarnBerryAdapter manages packages via Yarn Berry (v2+). Unlike classic
// yarn, Berry does not support `yarn global add`. Install uses
// `yarn add {pkg}` (project-local); check reads the lock file or runs
// `yarn info {pkg} --json` inside a project directory.
//
// NOTE: Berry installs are project-local, not global. Tools installed via
// this adapter live in the current project's node_modules/.bin and won't
// be available system-wide. This is a Berry design constraint, not a bug.
type YarnBerryAdapter struct{}

func NewYarnBerryAdapter() *YarnBerryAdapter {
	return &YarnBerryAdapter{}
}

func (a *YarnBerryAdapter) Kind() string { return "yarn-berry" }

func (a *YarnBerryAdapter) Available(ctx context.Context, rn run.Runner) bool {
	res := rn.Run(ctx, "yarn", "--version")
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	version := strings.TrimSpace(string(res.Stdout))
	if version == "" {
		return false
	}
	major, ok := parseMajorVersion(version)
	if !ok {
		return false
	}
	return major >= 2
}

// parseMajorVersion extracts the major version number from a version
// string like "3.0.2", "v2.1.0", or "1.22.19". Returns false if the
// major cannot be parsed (e.g. "berry-2.0.0" or non-numeric prefix).
func parseMajorVersion(version string) (int, bool) {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 {
		return 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return major, true
}

func (a *YarnBerryAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return false
	}
	// Use yarn node to check local resolution: exits 0 if package is installed,
	// 1 if not. This avoids hitting the npm registry (unlike `yarn info`).
	res := rn.Run(ctx, "yarn", "node", "-e",
		"try{process.exit(require.resolve(process.argv[1])?0:1)}catch(e){process.exit(1)}", pkg[0])
	return res.Err == nil && res.ExitCode == 0
}

func (a *YarnBerryAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("yarn-berry: no package name")
	}
	cmd := []string{"yarn", "add", pkg[0]}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("yarn-berry: install failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("yarn-berry: install exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

var _ exec.Adapter = (*YarnBerryAdapter)(nil)
