package main

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type checkV2Adapter struct {
	presence   plan.PresenceState
	checkCalls int
	resolve    int
	observe    int
}

type legacyCheckAdapter struct{ calls int }

func (a *legacyCheckAdapter) Kind() string                               { return "native" }
func (a *legacyCheckAdapter) Available(context.Context, run.Runner) bool { return true }
func (a *legacyCheckAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.calls++
	return true
}
func (a *legacyCheckAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func (a *checkV2Adapter) Kind() string                               { return "native" }
func (a *checkV2Adapter) Available(context.Context, run.Runner) bool { return true }
func (a *checkV2Adapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.checkCalls++
	return false
}
func (a *checkV2Adapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}
func (a *checkV2Adapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.resolve++
	resolved := intent.Clone()
	return &resolved, nil
}
func (a *checkV2Adapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	a.observe++
	return plan.Observation{Presence: a.presence}, nil
}
func (a *checkV2Adapter) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	return nil
}

func checkCandidate() (*config.Tool, *config.MethodCandidate) {
	return &config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo"}}
}

func TestCheckLiveAdapterV2UsesResolvedObservation(t *testing.T) {
	tool, method := checkCandidate()
	adapter := &checkV2Adapter{presence: plan.PresencePresent}
	if !checkLiveAdapterV2(context.Background(), adapter, tool, method) {
		t.Fatal("present observation should report installed")
	}
	if adapter.resolve != 1 || adapter.observe != 1 || adapter.checkCalls != 0 {
		t.Fatalf("calls resolve/observe/check = %d/%d/%d, want 1/1/0", adapter.resolve, adapter.observe, adapter.checkCalls)
	}
}

func TestCheckLiveAdapterV2AbsentAndUnknownAreNotInstalled(t *testing.T) {
	for _, presence := range []plan.PresenceState{plan.PresenceAbsent, plan.PresenceUnknown} {
		t.Run(string(presence), func(t *testing.T) {
			tool, method := checkCandidate()
			if checkLiveAdapterV2(context.Background(), &checkV2Adapter{presence: presence}, tool, method) {
				t.Fatalf("%s observation should not report installed", presence)
			}
		})
	}
}

func TestCheckLegacyAdapterFallsBackToCheck(t *testing.T) {
	tool, method := checkCandidate()
	legacy := &legacyCheckAdapter{}
	var adapter exec.Adapter = legacy
	if !checkAdapterInstalled(context.Background(), adapter, tool, method) {
		t.Fatal("legacy Check result was not preserved")
	}
	if legacy.calls != 1 {
		t.Fatalf("legacy Check calls = %d, want 1", legacy.calls)
	}
}
