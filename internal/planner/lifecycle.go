package planner

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

// ProjectLifecycleHooks projects generic tool hooks plus hooks owned by the
// selected method candidate into the candidate's plan. The legacy
// pre_install/post_install surface applies to both fresh installs and upgrades;
// the resolved verification result later selects exactly one transition.
func ProjectLifecycleHooks(p *plan.ResolvedInstallPlan, tool *config.Tool, method *config.MethodCandidate) {
	if p == nil || tool == nil || method == nil {
		return
	}
	p.Hooks = appendLifecycleHooks(p.Hooks, "tool", tool.PreInstall, plan.HookBefore)
	p.Hooks = appendLifecycleHooks(p.Hooks, "tool", tool.PostInstall, plan.HookAfter)
	p.Hooks = appendLifecycleHooks(p.Hooks, "candidate", method.PreInstall, plan.HookBefore)
	p.Hooks = appendLifecycleHooks(p.Hooks, "candidate", method.PostInstall, plan.HookAfter)
}

func appendLifecycleHooks(dst []plan.LifecycleHook, owner string, hooks []config.Hook, timing plan.HookTiming) []plan.LifecycleHook {
	for index, hook := range hooks {
		for _, transition := range []plan.TransitionKind{plan.TransitionInstall, plan.TransitionUpgrade} {
			dst = append(dst, plan.LifecycleHook{
				ID:         fmt.Sprintf("%s/%s/%d/%s", owner, timing, index, transition),
				Transition: transition,
				Timing:     timing,
				Operation: plan.Operation{
					Kind:          "hook",
					Description:   fmt.Sprintf("%s %s hook", owner, timing),
					Effect:        plan.EffectMutation,
					Command:       append([]string(nil), hook.Run...),
					ArbitraryCode: true,
				},
				When:          planHookCondition(hook.When),
				FailurePolicy: plan.HookFailAbort,
			})
		}
	}
	return dst
}

func planHookCondition(condition *config.Condition) *plan.HookCondition {
	if condition == nil {
		return nil
	}
	out := &plan.HookCondition{
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
