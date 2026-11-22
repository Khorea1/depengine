package exec

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

// A non-pinnable drift must never run a before-upgrade hook or install a lazy
// prerequisite: both are host mutations that precede the replacement WAL.
func TestUnpinnableReplacementStopsBeforeHooksAndLazyPrerequisites(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	adapter := &verificationAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "cargo"}},
		observation: plan.Observation{Presence: plan.PresencePresent,
			Identity: plan.ObservedIdentity{Package: "other"}, KnownFields: []plan.IdentityField{plan.FieldPackage}},
	}
	method := &config.MethodCandidate{
		Kind: "cargo", Config: map[string]any{"pkg": "demo"},
		Requires: []string{"helper"}, PreInstall: []config.Hook{{Run: []string{"before-upgrade-hook"}}},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"demo": {Name: "demo", Methods: []*config.MethodCandidate{method}},
		"helper": {Name: "helper", DependencyOnly: true, Methods: []*config.MethodCandidate{
			{Kind: "cargo", Config: map[string]any{"pkg": "helper"}},
		}},
	}}
	runner := &run.FakeRunner{}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithAllowArbitraryCode()(ex)
	WithOutput(io.Discard)(ex)
	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || len(report.Tools) != 1 || report.Tools[0].Tool != "demo" ||
		!strings.Contains(report.Tools[0].Error, "cannot be journaled safely") {
		t.Fatalf("report = %+v; want fail-closed drift before hooks and prerequisites", report)
	}
	if len(runner.Calls) != 0 || adapter.installCall != 0 {
		t.Fatalf("mutation before rejected upgrade: subprocesses=%v installs=%d", runner.Calls, adapter.installCall)
	}
	stored, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.ReplacementTransactions) != 0 || len(stored.Tools) != 0 {
		t.Fatalf("unsafe state change = %+v", stored)
	}
}
