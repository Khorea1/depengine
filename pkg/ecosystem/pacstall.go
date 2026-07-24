package ecosystem

import (
	"context"
	"fmt"
	"os"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// PacstallAdapter manages packages via Pacstall, an AUR-style package
// manager for Debian/Ubuntu. Install requires sudo since it modifies
// system packages.
type PacstallAdapter struct{}

func NewPacstallAdapter() *PacstallAdapter {
	return &PacstallAdapter{}
}

func (a *PacstallAdapter) Kind() string { return "pacstall" }

func (a *PacstallAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if !run.LookPath(ctx, rn, "pacstall") {
		return false
	}
	// Install uses sudo when not running as root; verify it's available too.
	// (This doesn't check passwordless sudo — just that the binary exists.)
	if !isElevated() {
		return run.LookPath(ctx, rn, "sudo")
	}
	return true
}

func (a *PacstallAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return false
	}
	res := rn.Run(ctx, "pacstall", "-Ci", pkg[0])
	return res.Err == nil && res.ExitCode == 0
}

func (a *PacstallAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return fmt.Errorf("pacstall: no package name")
	}

	// Use sudo if not running as root. The caller should ensure the
	// user has passwordless sudo for pacstall or a cached credential.
	var cmd []string
	if isElevated() {
		cmd = []string{"pacstall", "-I", pkg[0]}
	} else {
		cmd = []string{"sudo", "pacstall", "-I", pkg[0]}
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("pacstall: install failed: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("pacstall: install exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

var _ exec.Adapter = (*PacstallAdapter)(nil)

// isElevated reports whether the current process is running with
// root privileges (EUID 0). This is not testable via Runner, so it's
// kept as a minimal wrapper for easy replacement in tests.
var isElevated = func() bool {
	return os.Geteuid() == 0
}
