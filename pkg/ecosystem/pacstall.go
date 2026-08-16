package ecosystem

import (
	"context"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// PacstallAdapter manages packages via Pacstall, an AUR-style package
// manager for Debian/Ubuntu. Install requires sudo since it modifies
// system packages.
//
// Removal is intentionally manual: `pacstall -R {pkg}` exists but requires
// the same elevation as install and interactive confirmation; it is left to
// the user to keep the matrix conservative.
type PacstallAdapter struct{}

func NewPacstallAdapter() *PacstallAdapter {
	return &PacstallAdapter{}
}

func (a *PacstallAdapter) Kind() string { return "pacstall" }

func (a *PacstallAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if !run.LookPath(ctx, rn, "pacstall") {
		return false
	}
	// Elevation is needed unless already root.
	// Use the shared elevation detector instead of hardcoding "sudo".
	if isElevated() {
		return true
	}
	// ElevationMethod probes sudo -n and pkexec, caching the result.
	return run.ElevationMethod() != ""
}

func (a *PacstallAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return false
	}
	res := rn.Run(ctx, "pacstall", "-Ci", pkg[0])
	return res.Err == nil && res.ExitCode == 0
}

func (a *PacstallAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("pacstall: no package name")
	}

	// Use elevation (sudo/pkexec) if not running as root.
	// ElevationPrefix detects the best method at runtime.
	var cmd []string
	if isElevated() {
		cmd = []string{"pacstall", "-I", pkg[0]}
	} else if prefix := run.ElevationPrefix(); prefix != nil {
		cmd = append(prefix, "pacstall", "-I", pkg[0])
	} else {
		// No working elevation — try with bare sudo anyway for a clear error.
		cmd = []string{"sudo", "pacstall", "-I", pkg[0]}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return run.CheckResult(res, "pacstall: install")
}

var _ exec.Adapter = (*PacstallAdapter)(nil)

// isElevated reports whether the current process is running with
// root privileges (EUID 0). This is not testable via Runner, so it's
// kept as a minimal wrapper for easy replacement in tests.
var isElevated = func() bool {
	return os.Geteuid() == 0
}
