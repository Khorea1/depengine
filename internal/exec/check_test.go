package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type checkAvailabilityTracker struct {
	*executorAdapterV2Double
	calls int
}

func (a *checkAvailabilityTracker) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.calls++
	return true
}

type observationProbe struct {
	v2TestStub
	calls int
}

func (*observationProbe) Kind() string                               { return "observed" }
func (*observationProbe) Available(context.Context, run.Runner) bool { return true }
func (a *observationProbe) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	a.calls++
	return plan.Observation{Presence: plan.PresencePresent}, nil
}

func TestCheckInstalledResolvesAndObservesWithoutInstallProbes(t *testing.T) {
	availableCalls := 0
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{
			kindValue: "cargo",
			availableFunc: func() bool {
				availableCalls++
				return false
			},
		},
		presence: plan.PresencePresent,
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "cargo", Config: map[string]any{"pkg": "demo"},
	}}}

	if _, installed := ex.CheckInstalled(context.Background(), tool, "", false); installed {
		t.Fatal("CheckInstalled(live=false) should skip an unavailable adapter")
	}
	if availableCalls != 1 || adapter.resolveCall != 0 || adapter.observeCall != 0 {
		t.Fatalf("after non-live check: available/resolve/observe = %d/%d/%d, want 1/0/0", availableCalls, adapter.resolveCall, adapter.observeCall)
	}

	kind, installed := ex.CheckInstalled(context.Background(), tool, "", true)
	if !installed || kind != "cargo" {
		t.Fatalf("CheckInstalled(live=true) = %q, %v; want cargo, true", kind, installed)
	}
	if adapter.resolveCall != 1 || adapter.observeCall != 1 || adapter.checkCalls != 0 {
		t.Fatalf("resolve/observe/check = %d/%d/%d, want 1/1/0", adapter.resolveCall, adapter.observeCall, adapter.checkCalls)
	}
}

func TestCheckInstalledUsesSchemaMethodOrder(t *testing.T) {
	nativeAdapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "native"},
		presence:        plan.PresencePresent,
	}
	cargoAdapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresencePresent,
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithDefaultMethodOrder([]string{"cargo", "apt"})(ex)
	WithAdapters(nativeAdapter, cargoAdapter)(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{
		{Kind: "native", Config: map[string]any{"pkg": "demo"}},
		{Kind: "cargo", Config: map[string]any{"pkg": "demo"}},
	}}

	kind, installed := ex.CheckInstalled(context.Background(), tool, "debian", true)
	if !installed || kind != "cargo" {
		t.Fatalf("CheckInstalled() = %q, %v; want configured first method cargo, true", kind, installed)
	}
	if cargoAdapter.observeCall != 1 || nativeAdapter.observeCall != 0 {
		t.Fatalf("cargo/native observations = %d/%d, want 1/0", cargoAdapter.observeCall, nativeAdapter.observeCall)
	}
}

func TestCheckInstalledDoesNotProbeInstallAvailability(t *testing.T) {
	adapter := &checkAvailabilityTracker{executorAdapterV2Double: &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresenceAbsent,
	}}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{
		Kind: "cargo", Config: map[string]any{"pkg": "demo"},
	}}}

	if _, installed := ex.CheckInstalled(context.Background(), tool, "", true); installed {
		t.Fatal("absent observation should not report installed")
	}
	if adapter.calls != 0 {
		t.Fatalf("CheckAvailable() calls = %d, want 0 for a presence-only check", adapter.calls)
	}
}

func TestCheckInstalledUsesAdapterObservation(t *testing.T) {
	adapter := &observationProbe{}
	ex := New()
	WithAdapters(adapter)(ex)
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{{Kind: "observed"}}}

	kind, installed := ex.CheckInstalled(context.Background(), tool, "", true)
	if !installed || kind != "observed" || adapter.calls != 1 {
		t.Fatalf("CheckInstalled() = %q, %v with %d Observe calls; want observed, true, 1", kind, installed, adapter.calls)
	}
}
