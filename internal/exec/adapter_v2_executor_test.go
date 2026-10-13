package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

type executorAdapterV2Double struct {
	testMockAdapter
	presence      plan.PresenceState
	observed      plan.ObservedIdentity
	knownFields   []plan.IdentityField
	observeErr    error
	observeDetail string
	observeCall   int
	checkCalls    int
	resolveCall   int
	installCall   int
	installed     *plan.ResolvedInstallPlan
	postInstall   *plan.Observation
}

func (a *executorAdapterV2Double) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.checkCalls++
	return false
}

func (a *executorAdapterV2Double) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	a.observeCall++
	if a.installCall > 0 && a.postInstall != nil {
		return *a.postInstall, nil
	}
	if a.observeErr != nil {
		return plan.Observation{}, a.observeErr
	}
	detail := a.observeDetail
	if detail == "" {
		detail = "probe detail"
	}
	identity := a.observed
	if identity.Package == "" {
		identity.Package = "demo"
	}
	knownFields := a.knownFields
	if len(knownFields) == 0 {
		knownFields = []plan.IdentityField{plan.FieldPackage}
	}
	return plan.Observation{Presence: a.presence, Identity: identity, KnownFields: knownFields, Detail: detail}, nil
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

func (a *executorAdapterV2Double) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
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
	ctx := context.Background()
	rc := ex.newRunContext(ctx, nil, "")
	ex.tryMethods(ctx, rc, tool, &result, time.Now())
	return result
}

type macPortsSequenceRunner struct {
	*run.FakeRunner
	initiallyInstalled bool
	initialInventory   string
	initialExitCode    int
	inventoryQueries   int
}

func (r *macPortsSequenceRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	result := r.FakeRunner.Run(ctx, name, args...)
	if name != "/opt/local/bin/port" || len(args) == 0 || args[0] != "installed" {
		return result
	}
	r.inventoryQueries++
	if r.inventoryQueries == 1 && (r.initialInventory != "" || r.initialExitCode != 0) {
		result.Stdout = []byte(r.initialInventory)
		result.ExitCode = r.initialExitCode
		return result
	}
	if r.initiallyInstalled || r.inventoryQueries > 1 {
		result.Stdout = []byte("The following ports are currently installed:\n  git-delta @0.20.1_0 (active)\n")
	} else {
		result.Stdout = []byte("None of the specified ports are installed.\n")
	}
	return result
}

func TestExecutorMacPortsV2Lifecycle(t *testing.T) {
	for _, tc := range []struct {
		name               string
		initiallyInstalled bool
		initialInventory   string
		initialExitCode    int
		wantStatus         StatusEnum
		wantInventoryCalls int
	}{
		{name: "installed inventory", initiallyInstalled: true, wantStatus: StatusAlready, wantInventoryCalls: 1},
		// The executor verifies the installed target after the adapter's own
		// post-install verification, so this path queries inventory three times.
		{name: "absent inventory then install", wantStatus: StatusInstalled, wantInventoryCalls: 3},
		{name: "unexpected inventory", initialInventory: "unrecognized output\n", wantStatus: StatusFailed, wantInventoryCalls: 1},
		{name: "unrelated package", initialInventory: "The following ports are currently installed:\n  cmake @3.30.0_0 (active)\n", wantStatus: StatusFailed, wantInventoryCalls: 1},
		{name: "failed inventory query", initialInventory: "None of the specified ports are installed.\n", initialExitCode: 1, wantStatus: StatusFailed, wantInventoryCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &macPortsSequenceRunner{
				FakeRunner: &run.FakeRunner{
					ExitCode:  0,
					LookPaths: map[string]bool{"port": false, "/opt/local/bin/port": true},
				},
				initiallyInstalled: tc.initiallyInstalled,
				initialInventory:   tc.initialInventory,
				initialExitCode:    tc.initialExitCode,
			}
			ex := New()
			WithRunner(runner)(ex)
			WithAdapters(&NativeByManagerAdapter{managerName: "port"})(ex)
			schema := &config.Schema{
				Defaults: config.Defaults{MethodOrder: []string{"port"}},
				Tools: map[string]*config.Tool{
					"git-delta": {
						Name:       "git-delta",
						MethodOnly: []string{"port"},
						Methods: []*config.MethodCandidate{{
							Kind:   "port",
							Config: map[string]any{"pkg": "git-delta"},
						}},
					},
				},
			}
			report, err := ex.Execute(context.Background(), schema, "macos")
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if len(report.Tools) != 1 || report.Tools[0].Status != tc.wantStatus {
				t.Fatalf("Execute() tools = %+v, want status %v", report.Tools, tc.wantStatus)
			}
			result := report.Tools[0]
			if result.PlanIntent == nil || result.PlanIntent.Identity.Package != "git-delta" {
				t.Fatalf("PlanIntent = %+v, want effective package git-delta", result.PlanIntent)
			}
			if runner.inventoryQueries != tc.wantInventoryCalls {
				t.Fatalf("MacPorts inventory queries = %d, want %d", runner.inventoryQueries, tc.wantInventoryCalls)
			}

			var installCalls int
			for _, call := range runner.Calls {
				if call.Name != "sudo" {
					continue
				}
				installCalls++
				want := []string{"/opt/local/bin/port", "install", "git-delta"}
				if len(call.Args) != len(want) || strings.Join(call.Args, "\x00") != strings.Join(want, "\x00") {
					t.Fatalf("MacPorts install args = %q, want %q", call.Args, want)
				}
			}
			wantInstalls := 0
			if tc.wantStatus == StatusInstalled {
				wantInstalls = 1
			}
			if installCalls != wantInstalls {
				t.Fatalf("MacPorts candidate ran %d install commands, want %d", installCalls, wantInstalls)
			}
		})
	}
}

func TestExecutorMacPortsSchemaShorthand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\n[tools]\ngit-delta = { port = \"git-delta\", method_only = [\"port\"] }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := config.ParseProjectSchema(path, nil)
	if err != nil {
		t.Fatalf("ParseProjectSchema: %v", err)
	}
	if _, err := config.Validate(schema, []string{"native", "port"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	runner := &macPortsSequenceRunner{
		FakeRunner:         &run.FakeRunner{LookPaths: map[string]bool{"port": false, "/opt/local/bin/port": true}},
		initiallyInstalled: true,
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(&NativeByManagerAdapter{managerName: "port"})(ex)
	report, err := ex.Execute(context.Background(), schema, "macos")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusAlready || report.Tools[0].MethodKind != "port" {
		t.Fatalf("Execute: tools = %+v; want installed port candidate", report.Tools)
	}
}

func TestExecutorMacPortsDoesNotProbeOrInstallOnLinux(t *testing.T) {
	runner := &run.FakeRunner{LookPaths: map[string]bool{"port": true}}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(&NativeByManagerAdapter{managerName: "port"})(ex)
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"git-delta": {
			Name:       "git-delta",
			MethodOnly: []string{"port"},
			Methods:    []*config.MethodCandidate{{Kind: "port", Config: map[string]any{"pkg": "git-delta"}}},
		},
	}}
	if _, err := ex.Execute(context.Background(), schema, "debian"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, call := range runner.Calls {
		if call.Name == "port" || call.Name == "sudo" {
			t.Fatalf("MacPorts command ran outside macOS: %+v", runner.Calls)
		}
	}
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
			if tt.presence == plan.PresenceAbsent {
				adapter.postInstall = &plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "demo"}, KnownFields: []plan.IdentityField{plan.FieldPackage}}
			}
			result := v2ExecutorAttempt(t, adapter)
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %v, want %v; result = %+v", result.Status, tt.wantStatus, result)
			}
			wantObserveCalls := 1 + tt.wantInstall
			if adapter.observeCall != wantObserveCalls {
				t.Fatalf("Observe() calls = %d, want %d", adapter.observeCall, wantObserveCalls)
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
