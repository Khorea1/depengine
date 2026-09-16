package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
)

func (ex *Executor) hasDangerousMethod(tool *config.Tool) bool {
	for _, method := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
		for _, key := range []string{"build", "build_cmd", "build_command"} {
			if value, ok := method.Config[key].(string); ok && value != "" {
				return true
			}
		}
	}
	return false
}

func (ex *Executor) runPreinstall(ctx context.Context, tool *config.Tool) error {
	cmd := strings.TrimSpace(tool.PreInstall)
	if cmd == "" {
		return nil
	}
	ex.outputf("    pre-install: %s\n", cmd)
	ex.logDebug(ctx, "preinstall", "tool", tool.Name, "cmd", cmd)
	result := ex.rn.Run(ctx, "sh", "-c", cmd)
	if result.Err != nil {
		ex.outputf("    ⚠  pre-install: %s (aborting)\n", result.Err)
		ex.logWarn(ctx, "preinstall", "tool", tool.Name, "error", result.Err.Error())
		return result.Err
	}
	if result.ExitCode != 0 {
		ex.outputf("    ⚠  pre-install: exit %d (aborting)\n", result.ExitCode)
		ex.logWarn(ctx, "preinstall", "tool", tool.Name, "exit_code", result.ExitCode)
		return fmt.Errorf("pre-install exit %d", result.ExitCode)
	}
	return nil
}

func (ex *Executor) runPostinstall(ctx context.Context, tool *config.Tool) error {
	cmd := strings.TrimSpace(tool.PostInstall)
	if cmd == "" {
		return nil
	}
	if tool.PostInstallWhen != nil && !tool.PostInstallWhen.Match(ex.facts) {
		ex.outputf("    postinstall: skipped (when condition not met)\n")
		ex.logDebug(ctx, "postinstall", "tool", tool.Name, "status", "skip_when")
		return nil
	}
	ex.outputf("    postinstall: %s\n", cmd)
	ex.logDebug(ctx, "postinstall", "tool", tool.Name, "cmd", cmd)
	result := ex.rn.Run(ctx, "sh", "-c", cmd)
	if result.Err != nil {
		ex.outputf("    ⚠  postinstall: %s (failed)\n", result.Err)
		ex.logWarn(ctx, "postinstall", "tool", tool.Name, "error", result.Err.Error())
		return result.Err
	}
	if result.ExitCode != 0 {
		ex.outputf("    ⚠  postinstall: exit %d (failed)\n", result.ExitCode)
		ex.logWarn(ctx, "postinstall", "tool", tool.Name, "exit_code", result.ExitCode)
		return fmt.Errorf("postinstall exited %d", result.ExitCode)
	}
	ex.logDebug(ctx, "postinstall", "tool", tool.Name, "status", "done")
	return nil
}
