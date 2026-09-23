package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type executorAdapterV2Double struct {
	testMockAdapter
	presence      plan.PresenceState
	observeErr    error
	observeDetail string
	observeCall   int
	checkCalls    int
	resolveCall   int
	installCall   int
	installed     *plan.ResolvedInstallPlan
}

func (a *executorAdapterV2Double) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.checkCalls++
	return false
}

func (a *executorAdapterV2Double) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	a.observeCall++
	if a.observeErr != nil {
		return plan.Observation{}, a.observeErr
	}
	detail := a.observeDetail
	if detail == "" {
		detail = "probe detail"
	}
	return plan.Observation{Presence: a.presence, Identity: plan.ObservedIdentity{Package: "demo"}, KnownFields: []plan.IdentityField{plan.FieldPackage}, Detail: detail}, nil
}

func (a *executorAdapterV2Double) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.resolveCall++
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *executorAdapterV2Double) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	a.installCall++
	return nil
}

func (a *executorAdapterV2Double) InstallResolved(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.installCall++
	a.installed = resolved
	return nil
}

func (a *executorAdapterV2Double) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func (a *executorAdapterV2Double) CanRemove() bool { return true }

func (a *executorAdapterV2Double) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

func (a *executorAdapterV2Double) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ AdapterV2 = (*executorAdapterV2Double)(nil)

func v2ExecutorAttempt(t *testing.T, adapter AdapterV2) ToolResult {
	t.Helper()
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{adapter.Kind()},
		Methods: []*config.MethodCandidate{{
			Kind:   adapter.Kind(),
			Config: map[string]any{"pkg": "demo"},
		}},
	}
	result := ToolResult{Tool: tool.Name}
	ex.tryMethods(context.Background(), tool, &result, time.Now())
	return result
}

func TestExecutorAdapterV2PresenceStates(t *testing.T) {
	tests := []struct {
		name        string
		presence    plan.PresenceState
		wantStatus  StatusEnum
		wantInstall int
	}{
		{name: "present with matching package", presence: plan.PresencePresent, wantStatus: StatusAlready},
		{name: "absent", presence: plan.PresenceAbsent, wantStatus: StatusInstalled, wantInstall: 1},
		{name: "unknown", presence: plan.PresenceUnknown, wantStatus: StatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &executorAdapterV2Double{
				testMockAdapter: testMockAdapter{kindValue: "cargo"},
				presence:        tt.presence,
			}
			result := v2ExecutorAttempt(t, adapter)
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %v; result = %+v", result.Status, tt.wantStatus, result)
			}
			if adapter.observeCall != 1 {
				t.Fatalf("Observe() calls = %d, want 1", adapter.observeCall)
			}
			if adapter.resolveCall != 1 {
				t.Fatalf("ResolvePlan() calls = %d, want 1", adapter.resolveCall)
			}
			if adapter.checkCalls != 0 {
				t.Fatalf("Check() calls = %d, want 0 for V2", adapter.checkCalls)
			}
			if adapter.installCall != tt.wantInstall {
				t.Fatalf("Install() calls = %d, want %d", adapter.installCall, tt.wantInstall)
			}
			if tt.wantInstall > 0 && adapter.installed == nil {
				t.Fatal("InstallResolved() did not receive the resolved plan")
			}
		})
	}
}

func TestExecutorAdapterV2BrokenObservationFailsCandidate(t *testing.T) {
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresenceBroken,
	}
	result := v2ExecutorAttempt(t, adapter)
	if result.Status != StatusFailed {
		t.Fatalf("status = %v, want failed; result = %+v", result.Status, result)
	}
	if adapter.installCall != 0 {
		t.Fatalf("Install() calls = %d, want 0", adapter.installCall)
	}
	if len(result.Methods) != 1 || result.Methods[0].Status != "failed" || !strings.Contains(result.Methods[0].Error, "probe detail") {
		t.Fatalf("methods = %+v, want explainable broken-observation failure", result.Methods)
	}
}

func TestExecutorAdapterV2ObservationErrorFailsCandidate(t *testing.T) {
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		observeErr:      errors.New("probe unavailable at https://user:pass@example.test/?token=secret"),
	}
	result := v2ExecutorAttempt(t, adapter)
	if result.Status != StatusFailed {
		t.Fatalf("status = %v, want failed; result = %+v", result.Status, result)
	}
	if adapter.installCall != 0 {
		t.Fatalf("Install() calls = %d, want 0", adapter.installCall)
	}
	if len(result.Methods) != 1 || !strings.Contains(result.Methods[0].Error, "probe unavailable") {
		t.Fatalf("methods = %+v, want explainable observation error", result.Methods)
	}
	if strings.Contains(result.Methods[0].Error, "pass") || strings.Contains(result.Methods[0].Error, "secret") {
		t.Fatalf("observation error leaked probe credentials: %q", result.Methods[0].Error)
	}
}

func explainAdapterAttempt(t *testing.T, adapter AdapterV2) MethodAttempt {
	t.Helper()
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{adapter.Kind()},
		Methods: []*config.MethodCandidate{{
			Kind:   adapter.Kind(),
			Config: map[string]any{"pkg": "demo"},
		}},
	}
	attempts := ex.ExplainTool(context.Background(), tool, "")
	if len(attempts) != 1 {
		t.Fatalf("attempts = %+v, want 1", attempts)
	}
	return attempts[0]
}

func TestExplainToolAdapterV2PresenceStates(t *testing.T) {
	tests := []struct {
		name       string
		presence   plan.PresenceState
		wantStatus string
	}{
		{name: "present", presence: plan.PresencePresent, wantStatus: "already_installed"},
		{name: "absent", presence: plan.PresenceAbsent, wantStatus: "would_install"},
		{name: "unknown", presence: plan.PresenceUnknown, wantStatus: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &executorAdapterV2Double{
				testMockAdapter: testMockAdapter{kindValue: "cargo"},
				presence:        tt.presence,
			}
			attempt := explainAdapterAttempt(t, adapter)
			if attempt.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q: %+v", attempt.Status, tt.wantStatus, attempt)
			}
			if adapter.observeCall != 1 || adapter.checkCalls != 0 {
				t.Fatalf("Observe() calls = %d, Check() calls = %d; want 1 and 0", adapter.observeCall, adapter.checkCalls)
			}
			if adapter.resolveCall != 1 {
				t.Fatalf("ResolvePlan() calls = %d, want 1", adapter.resolveCall)
			}
		})
	}
}

func TestExplainToolAdapterV2ProbeFailureIsSafe(t *testing.T) {
	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
		presence:        plan.PresenceBroken,
		observeDetail:   "probe failed at https://user:pass@example.test/?token=secret",
	}
	attempt := explainAdapterAttempt(t, adapter)
	if attempt.Status != "failed" {
		t.Fatalf("status = %q, want failed: %+v", attempt.Status, attempt)
	}
	if strings.Contains(attempt.Error, "pass") || strings.Contains(attempt.Error, "secret") {
		t.Fatalf("error leaked probe credentials: %q", attempt.Error)
	}
	if !strings.Contains(attempt.Error, "probe failed") {
		t.Fatalf("error = %q, want probe failure detail", attempt.Error)
	}
}
