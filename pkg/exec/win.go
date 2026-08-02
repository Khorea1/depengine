package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

func init() {
	Register(&winAdapter{
		kind:       "scoop",
		binary:     "scoop",
		installCmd: []string{"scoop", "install", "{pkg}"},
		checkCmd:   []string{"scoop", "list", "{pkg}"},
		removeCmd:  []string{"scoop", "uninstall", "{pkg}"},
	})
	Register(&winAdapter{
		kind:       "choco",
		binary:     "choco",
		installCmd: []string{"choco", "install", "{pkg}", "-y"},
		checkCmd:   []string{"cmd", "/c", `choco list --local-only --exact --limit-output {pkg} | findstr /c:"{pkg}"`},
		removeCmd:  []string{"choco", "uninstall", "{pkg}", "-y"},
	})
}

// winAdapter implements Adapter for a Windows package manager (scoop, choco).
// Commands use "{pkg}" as a placeholder for the package name from Tool or config.
type winAdapter struct {
	kind, binary                    string
	installCmd, checkCmd, removeCmd []string
}

func (w *winAdapter) Kind() string { return w.kind }

func (w *winAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, w.binary)
}

func (w *winAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if rn == nil {
		return false
	}
	cmd := SubstitutePkg(w.checkCmd, tool, mc)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	return res.Err == nil && res.ExitCode == 0
}

func (w *winAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", w.kind)
	}
	cmd := SubstitutePkg(w.installCmd, tool, mc)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("%s: install failed: %w", w.kind, res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("%s: install exited %d: %s", w.kind, res.ExitCode, stderr)
	}
	return nil
}

func (w *winAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if rn == nil {
		return fmt.Errorf("%s: no runner", w.kind)
	}
	if len(w.removeCmd) == 0 {
		return fmt.Errorf("%s: no remove command configured", w.kind)
	}
	cmd := SubstitutePkg(w.removeCmd, tool, mc)
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("%s: remove failed: %w", w.kind, res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("%s: remove exited %d: %s", w.kind, res.ExitCode, stderr)
	}
	return nil
}
func (w *winAdapter) CanRemove() bool { return len(w.removeCmd) > 0 }

// Compile-time interface checks.
var _ Adapter = (*winAdapter)(nil)
var _ Remover = (*winAdapter)(nil)
