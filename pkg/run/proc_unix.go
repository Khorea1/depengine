//go:build !windows

package run

import (
	"errors"
	"os/exec"
	"syscall"
)

// setupChild puts the child in its own process group so terminateTree can
// signal the whole tree (e.g. sudo -> apt -> dpkg) instead of only the
// direct child. Without this, cancelling the context kills sudo and
// orphans dpkg mid-transaction, leaving half-installed packages behind.
func setupChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateTree sends SIGTERM to the child's process group, giving every
// process in the tree a chance to clean up. The caller (os/exec, via
// cmd.Cancel) escalates after cmd.WaitDelay if anything ignores it.
func terminateTree(cmd *exec.Cmd) error {
	proc := cmd.Process
	if proc == nil {
		return nil
	}
	// A negative pid targets the group created by Setpgid above. ESRCH
	// only means the child already exited — not an error here, because
	// cmd.Wait still reports the real exit state.
	if err := syscall.Kill(-proc.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
