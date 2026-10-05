package exec

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// hasDangerousMethod reports whether any selected method contains a field whose
// schema contract is executable code. The decision is based on method semantics
// (methodkind.Command), not on the raw TOML representation of the value. This
// keeps string commands, structured argv commands, aliases, and future command
// fields behind the same --allow-arbitrary-code gate.
func (ex *Executor) hasDangerousMethod(rc *runContext, tool *config.Tool) bool {
	for _, method := range rc.selectedMethods(tool) {
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
// CandidateRunsArbitraryCode reports whether executing one selected method
// crosses the arbitrary-code capability boundary. Composition-root commands
// that execute adapters directly must use the same gate as Executor.Execute.
func CandidateRunsArbitraryCode(tool *config.Tool, method *config.MethodCandidate) bool {
	return methodRunsArbitraryCode(tool, method)
}

func methodRunsArbitraryCode(tool *config.Tool, method *config.MethodCandidate) bool {
	if intent, _ := candidatePlanIntent(tool, method); intent != nil {
		if capabilities, err := methodkind.PlanCapabilities(*intent); err == nil {
			return capabilities&methodkind.CapabilityArbitraryCode != 0
		}
	}
	return len(method.PreInstall) > 0 || len(method.PostInstall) > 0 || configuredCommand(method)
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

func (ex *Executor) hasArbitraryCode(rc *runContext, tool *config.Tool) bool {
	return len(tool.PreInstall) > 0 || len(tool.PostInstall) > 0 || ex.hasDangerousMethod(rc, tool)
}

// runLifecycleHooks executes only hooks carried by the selected resolved plan
// and matching the concrete transition selected from verification. It returns
// true when at least one hook actually ran successfully; dry-run rendering and
// condition-skipped hooks do not become historical execution evidence.
func (ex *Executor) runLifecycleHooks(ctx context.Context, toolName string, resolved *plan.ResolvedInstallPlan, transition plan.TransitionKind, timing plan.HookTiming) (bool, error) {
	if resolved == nil {
		return false, nil
	}
	hooks, err := resolved.HookSchedule(transition, timing)
	if err != nil {
		return false, err
	}
	phase := lifecycleHookPhase(transition, timing)
	ran := false
	for _, hook := range hooks {
		if condition := configHookCondition(hook.When); condition != nil && !condition.Match(ex.facts) {
			ex.outputf("    %s: %s: skipped (when condition not met)\n", toolName, phase)
			ex.logDebug(ctx, phase, "tool", toolName, "hook", hook.ID, "status", "skip_when")
			continue
		}
		if len(hook.Operation.Command) == 0 || strings.TrimSpace(hook.Operation.Command[0]) == "" {
			return ran, fmt.Errorf("%s: empty command", phase)
		}
		command := strings.Join(hook.Operation.Command, " ")
		if ex.dryRun {
			ex.outputf("    %s: %s: would run %s\n", toolName, phase, command)
			ex.logDebug(ctx, phase, "tool", toolName, "hook", hook.ID, "cmd", command, "status", "would_run")
			continue
		}
		ex.outputf("    %s: %s: %s\n", toolName, phase, command)
		ex.logDebug(ctx, phase, "tool", toolName, "hook", hook.ID, "cmd", command)
		result := ex.mutationRunner(toolName, phase).Run(ctx, hook.Operation.Command[0], hook.Operation.Command[1:]...)
		var hookErr error
		if result.Err != nil {
			hookErr = result.Err
			ex.outputf("    ⚠  %s: %s: %s (failed)\n", toolName, phase, result.Err)
			ex.logWarn(ctx, phase, "tool", toolName, "hook", hook.ID, "error", result.Err.Error())
		} else if result.ExitCode != 0 {
			hookErr = fmt.Errorf("%s exited %d", phase, result.ExitCode)
			ex.outputf("    ⚠  %s: %s: exit %d (failed)\n", toolName, phase, result.ExitCode)
			ex.logWarn(ctx, phase, "tool", toolName, "hook", hook.ID, "exit_code", result.ExitCode)
		}
		if hookErr != nil {
			if hook.FailurePolicy == plan.HookFailContinueReport {
				// There is no durable "hook success" state. Continue-report hooks are
				// diagnostics around a transition; their failure is logged but does
				// not authorize aborting or rolling back the primary mutation.
				continue
			}
			return ran, hookErr
		}
		ran = true
	}
	return ran, nil
}

func lifecycleHookPhase(transition plan.TransitionKind, timing plan.HookTiming) string {
	prefix := "pre"
	if timing == plan.HookAfter {
		prefix = "post"
	}
	return prefix + "-" + string(transition)
}

func configHookCondition(condition *plan.HookCondition) *config.Condition {
	if condition == nil {
		return nil
	}
	out := &config.Condition{
		DistroFamily:     append([]string(nil), condition.DistroFamily...),
		TargetFamily:     append([]string(nil), condition.TargetFamily...),
		DistroID:         append([]string(nil), condition.DistroID...),
		DistroVersion:    append([]string(nil), condition.DistroVersion...),
		DistroVersionMin: condition.DistroVersionMin,
		DistroVersionMax: condition.DistroVersionMax,
		Arch:             append([]string(nil), condition.Arch...),
		OS:               append([]string(nil), condition.OS...),
		Kernel:           append([]string(nil), condition.Kernel...),
		Libc:             append([]string(nil), condition.Libc...),
		InitSystem:       append([]string(nil), condition.InitSystem...),
	}
	if condition.IsWSL != nil {
		value := *condition.IsWSL
		out.IsWSL = &value
	}
	if condition.IsContainer != nil {
		value := *condition.IsContainer
		out.IsContainer = &value
	}
	if condition.IsAndroid != nil {
		value := *condition.IsAndroid
		out.IsAndroid = &value
	}
	return out
}
