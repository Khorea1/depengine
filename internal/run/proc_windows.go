//go:build windows

package run

import (
	"os/exec"
)

// setupChild is a no-op on Windows: process groups and SIGTERM do not
// exist there, so termination falls back to killing the direct child
// (see terminateTree). Orphaned grandchildren remain a known limitation
// on this platform.
func setupChild(cmd *exec.Cmd) {}

// terminateTree kills the direct child. Go has no portable
// whole-tree primitive on Windows short of job objects.
func terminateTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
