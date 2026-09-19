package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
)

// hasDangerousMethod reports whether any selected method contains a field whose
// schema contract is executable code. The decision is based on method semantics
// (methodkind.Command), not on the raw TOML representation of the value. This
// keeps string commands, structured argv commands, aliases, and future command
// fields behind the same --allow-arbitrary-code gate.
func (ex *Executor) hasDangerousMethod(tool *config.Tool) bool {
	for _, method := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
		contract, ok := methodkind.Lookup(method.Kind)
		if !ok {
			continue
		}
		if contract.RequestedCapabilities(method.Config)&methodkind.CapabilityArbitraryCode != 0 {
			return true
		}
	}
	return false
}

func (ex *Executor) hasArbitraryCode(tool *config.Tool) bool {
	return len(tool.PreInstall) > 0 || len(tool.PostInstall) > 0 || ex.hasDangerousMethod(tool)
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
