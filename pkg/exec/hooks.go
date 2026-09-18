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
	return ex.runHooks(ctx, tool.Name, "pre-install", tool.PreInstall)
}

func (ex *Executor) runPostinstall(ctx context.Context, tool *config.Tool) error {
	return ex.runHooks(ctx, tool.Name, "post-install", tool.PostInstall)
}

func (ex *Executor) runHooks(ctx context.Context, toolName, phase string, hooks []config.Hook) error {
	for _, hook := range hooks {
		if hook.When != nil && !hook.When.Match(ex.facts) {
			ex.outputf("    %s: skipped (when condition not met)\n", phase)
			ex.logDebug(ctx, phase, "tool", toolName, "status", "skip_when")
			continue
		}
		if len(hook.Run) == 0 || strings.TrimSpace(hook.Run[0]) == "" {
			return fmt.Errorf("%s: empty command", phase)
		}
		command := strings.Join(hook.Run, " ")
		if ex.dryRun {
			ex.outputf("    %s: would run %s\n", phase, command)
			ex.logDebug(ctx, phase, "tool", toolName, "cmd", command, "status", "would_run")
			continue
		}
		ex.outputf("    %s: %s\n", phase, command)
		ex.logDebug(ctx, phase, "tool", toolName, "cmd", command)
		result := ex.mutationRunner(toolName, phase).Run(ctx, hook.Run[0], hook.Run[1:]...)
		if result.Err != nil {
			ex.outputf("    ⚠  %s: %s (failed)\n", phase, result.Err)
			ex.logWarn(ctx, phase, "tool", toolName, "error", result.Err.Error())
			return result.Err
		}
		if result.ExitCode != 0 {
			ex.outputf("    ⚠  %s: exit %d (failed)\n", phase, result.ExitCode)
			ex.logWarn(ctx, phase, "tool", toolName, "exit_code", result.ExitCode)
			return fmt.Errorf("%s exited %d", phase, result.ExitCode)
		}
	}
	return nil
}
