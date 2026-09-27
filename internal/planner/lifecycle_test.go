package planner

import (
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

func TestBuildCandidateIntentProjectsCandidateLocalLifecycleHooks(t *testing.T) {
	tool := &config.Tool{
		Name:       "demo",
		PreInstall: []config.Hook{{Run: []string{"echo", "generic-pre"}}},
	}
	method := &config.MethodCandidate{
		Kind:   "native",
		Config: map[string]any{"pkg": "demo"},
		PreInstall: []config.Hook{{
			Run:  []string{"echo", "candidate-pre"},
			When: &config.Condition{OS: []string{"linux"}},
		}},
		PostInstall: []config.Hook{{Run: []string{"echo", "candidate-post"}}},
	}

	got, err := BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}

	installBefore, err := got.HookSchedule(plan.TransitionInstall, plan.HookBefore)
	if err != nil {
		t.Fatal(err)
	}
	if len(installBefore) != 2 {
		t.Fatalf("install/before hooks = %d, want 2: %#v", len(installBefore), installBefore)
	}
	if commands := [][]string{installBefore[0].Operation.Command, installBefore[1].Operation.Command}; !reflect.DeepEqual(commands, [][]string{{"echo", "generic-pre"}, {"echo", "candidate-pre"}}) {
		t.Fatalf("install/before commands = %#v", commands)
	}
	if installBefore[1].When == nil || !reflect.DeepEqual(installBefore[1].When.OS, []string{"linux"}) {
		t.Fatalf("candidate hook condition not projected: %#v", installBefore[1].When)
	}
	upgradeBefore, err := got.HookSchedule(plan.TransitionUpgrade, plan.HookBefore)
	if err != nil || len(upgradeBefore) != 2 {
		t.Fatalf("upgrade/before = %#v, err=%v; want matching legacy hooks", upgradeBefore, err)
	}
	installAfter, err := got.HookSchedule(plan.TransitionInstall, plan.HookAfter)
	if err != nil || len(installAfter) != 1 || !reflect.DeepEqual(installAfter[0].Operation.Command, []string{"echo", "candidate-post"}) {
		t.Fatalf("install/after = %#v, err=%v", installAfter, err)
	}
}

func TestBuildCandidateIntentDoesNotLeakHooksAcrossCandidates(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	withHook := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "one"}, PreInstall: []config.Hook{{Run: []string{"echo", "only-one"}}}}
	withoutHook := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "two"}}

	first, err := BuildCandidateIntent(tool, withHook)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildCandidateIntent(tool, withoutHook)
	if err != nil {
		t.Fatal(err)
	}
	firstHooks, _ := first.HookSchedule(plan.TransitionInstall, plan.HookBefore)
	secondHooks, _ := second.HookSchedule(plan.TransitionInstall, plan.HookBefore)
	if len(firstHooks) != 1 {
		t.Fatalf("first hooks = %d, want 1", len(firstHooks))
	}
	if len(secondHooks) != 0 {
		t.Fatalf("second candidate inherited another candidate's hooks: %#v", secondHooks)
	}
}

func TestBuildValidatedCandidateIntentTreatsLifecycleHooksAsExecutorOwned(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{
		Kind:       "native",
		Config:     map[string]any{"pkg": "demo"},
		PreInstall: []config.Hook{{Run: []string{"echo", "pre"}}},
	}
	intent, err := BuildValidatedCandidateIntent(tool, method, methodkind.CandidateRequirements{})
	if err != nil {
		t.Fatalf("BuildValidatedCandidateIntent() rejected executor-owned hook: %v", err)
	}
	caps, err := methodkind.PlanCapabilities(*intent)
	if err != nil {
		t.Fatal(err)
	}
	if caps&methodkind.CapabilityArbitraryCode == 0 {
		t.Fatalf("plan capabilities = %v, want arbitrary-code for security gating", methodkind.CapabilityNames(caps))
	}
}
