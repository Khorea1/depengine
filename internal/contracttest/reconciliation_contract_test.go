package contracttest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	executor "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestPipxObservationReconciliationSelectsSafeTransitions(t *testing.T) {
	const exact = `{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":"1.2.3"}}}}}`
	const drift = `{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":"2.0.0"}}}}}`
	const empty = `{"pipx_spec_version":"0.1","venvs":{}}`

	tests := []struct {
		name              string
		stdout            string
		stderr            string
		exitCode          int
		wantPresence      plan.PresenceState
		wantState         plan.VerificationState
		wantTransition    plan.TransitionKind
		wantRequired      bool
		wantTransitionErr bool
	}{
		{name: "absent selects install", stdout: empty, wantPresence: plan.PresenceAbsent, wantState: plan.StateAbsent, wantTransition: plan.TransitionInstall, wantRequired: true},
		{name: "desired state is satisfied without mutation", stdout: exact, wantPresence: plan.PresencePresent, wantState: plan.StateSatisfied},
		{name: "drift selects upgrade", stdout: drift, wantPresence: plan.PresencePresent, wantState: plan.StateDrifted, wantTransition: plan.TransitionUpgrade, wantRequired: true},
		{name: "unknown state rejects mutation", stdout: `{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":""}}}}}`, wantPresence: plan.PresenceUnknown, wantState: plan.StateUnknown, wantTransitionErr: true},
		{name: "broken probe rejects mutation", stderr: "pipx failed", exitCode: 1, wantPresence: plan.PresenceBroken, wantState: plan.StateBroken, wantTransitionErr: true},
	}

	adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["pipx"])
	method := &config.MethodCandidate{Kind: "pipx", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{LookPaths: map[string]bool{"pipx": true}, Stdout: tt.stdout, Stderr: tt.stderr, ExitCode: tt.exitCode}
			observation, observeErr := adapter.Observe(context.Background(), runner, &config.Tool{Name: "demo"}, method)
			if observation.Presence != tt.wantPresence {
				t.Fatalf("Observe() presence = %q, want %q (observation=%+v error=%v)", observation.Presence, tt.wantPresence, observation, observeErr)
			}
			verification := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1.2.3"}, observation)
			if verification.State != tt.wantState {
				t.Fatalf("Reconcile() state = %q, want %q (observation=%+v)", verification.State, tt.wantState, observation)
			}
			decision, transitionErr := plan.TransitionForVerification(verification)
			if (transitionErr != nil) != tt.wantTransitionErr {
				t.Fatalf("TransitionForVerification() error = %v, wantErr %v", transitionErr, tt.wantTransitionErr)
			}
			if transitionErr == nil && (decision.Required != tt.wantRequired || decision.Transition != tt.wantTransition) {
				t.Fatalf("transition decision = %+v, want required=%v transition=%q", decision, tt.wantRequired, tt.wantTransition)
			}
		})
	}
}

func TestPipxExecutorFailsClosedAndSatisfiedWithoutMutation(t *testing.T) {
	const exact = `{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":"1.2.3"}}}}}`
	const unknown = `{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":""}}}}}`
	tests := []struct {
		name       string
		stdout     string
		stderr     string
		exitCode   int
		wantStatus executor.StatusEnum
		secret     string
	}{
		{name: "satisfied", stdout: exact, wantStatus: executor.StatusAlready},
		{name: "unknown fails closed", stdout: unknown, wantStatus: executor.StatusFailed},
		{name: "broken fails closed and redacts external error", stderr: "Authorization: Bearer secret-value\n", exitCode: 1, wantStatus: executor.StatusFailed, secret: "secret-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{LookPaths: map[string]bool{"pipx": true}, Stdout: tt.stdout, Stderr: tt.stderr, ExitCode: tt.exitCode}
			tool := &config.Tool{
				Name:       "demo",
				MethodOnly: []string{"pipx"},
				Methods:    []*config.MethodCandidate{{Kind: "pipx", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}},
			}
			schema := &config.Schema{
				Defaults: config.Defaults{Manager: "pipx", MethodOrder: []string{"pipx"}},
				Tools:    map[string]*config.Tool{"demo": tool},
			}
			ex := executor.New()
			executor.WithRunner(runner)(ex)
			executor.WithAdapters(ecosystem.NewBaseAdapter(ecosystem.Configs["pipx"]))(ex)
			report, err := ex.Execute(context.Background(), schema, "linux")
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if report == nil || len(report.Tools) != 1 || report.Tools[0].Status != tt.wantStatus {
				t.Fatalf("Execute() report = %+v, want status %v", report, tt.wantStatus)
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("marshal external report: %v", err)
			}
			if tt.secret != "" && (strings.Contains(report.Tools[0].Error, tt.secret) || strings.Contains(string(encoded), tt.secret)) {
				t.Fatalf("external result leaked Bearer secret: result=%q report=%s", report.Tools[0].Error, encoded)
			}
			for _, call := range runner.Calls {
				if call.Name == "pipx" && len(call.Args) > 0 && (call.Args[0] == "install" || call.Args[0] == "uninstall") {
					t.Fatalf("non-install transition executed package mutation: %v", call)
				}
			}
		})
	}
}
