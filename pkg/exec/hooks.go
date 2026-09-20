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
		if methodRunsArbitraryCode(tool, method) {
			return true
		}
	}
	return false
}

// methodRunsArbitraryCode derives the arbitrary-code gate from the shared
// plan. If static planning rejects the candidate (malformed programmatic
// input) it fails closed to the schema-level command predicate, so an
// invalid extra field cannot bypass the security gate.
func methodRunsArbitraryCode(tool *config.Tool, method *config.MethodCandidate) bool {
	if intent, _ := candidatePlanIntent(tool, method); intent != nil {
		if capabilities, err := methodkind.PlanCapabilities(*intent); err == nil {
			return capabilities&methodkind.CapabilityArbitraryCode != 0
		}
	}
	return configuredCommand(method)
}

func configuredCommand(method *config.MethodCandidate) bool {
	contract, ok := methodkind.Lookup(method.Kind)
	if !ok {
		return false
	}
	for name, field := range contract.Fields {
		if field.Type == methodkind.Command {
			if _, configured := method.Config[name]; configured {
				return true
			}
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
