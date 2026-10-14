package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	executor "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type pipxInventoryRunner struct {
	installed         bool
	version           string
	installVersion    string
	installVersionSet bool
	postInstallError  string
	calls             []run.FakeCall
}

func (r *pipxInventoryRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: slices.Clone(args)})
	if name != "pipx" {
		return run.Result{}
	}
	if len(args) == 2 && args[0] == "list" && args[1] == "--json" {
		if r.installed && r.postInstallError != "" {
			return run.Result{ExitCode: 1, Stderr: []byte(r.postInstallError)}
		}
		if !r.installed {
			return run.Result{Stdout: []byte(`{"pipx_spec_version":"0.1","venvs":{}}`)}
		}
		return run.Result{Stdout: []byte(fmt.Sprintf(`{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":%q}}}}}`, r.version))}
	}
	if len(args) == 2 && args[0] == "install" {
		pkg, version, ok := strings.Cut(args[1], "==")
		if !ok || pkg != "demo" || version == "" {
			return run.Result{ExitCode: 1, Stderr: []byte("unexpected pipx install spec")}
		}
		r.installed = true
		r.version = version
		if r.installVersionSet {
			r.version = r.installVersion
		}
		return run.Result{}
	}
	return run.Result{ExitCode: 1, Stderr: []byte("unexpected pipx command")}
}

func (*pipxInventoryRunner) LookPath(context.Context, string) bool { return true }
func (r *pipxInventoryRunner) RunWithEnv(ctx context.Context, _ map[string]string, _ []string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}
func (r *pipxInventoryRunner) RunInDir(ctx context.Context, _ string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}
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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
			if tt.secret != "" {
				resultError := report.Tools[0].Error
				if strings.Contains(resultError, tt.secret) || strings.Contains(string(encoded), tt.secret) {
					t.Fatalf("external result leaked Bearer secret: result=%q report=%s", resultError, encoded)
				}
				if !strings.Contains(resultError, "Authorization:") || !strings.Contains(resultError, "***") {
					t.Fatalf("external result did not retain a redacted authorization diagnostic: %q", resultError)
				}
			}
			for _, call := range runner.Calls {
				if call.Name == "pipx" && len(call.Args) > 0 && (call.Args[0] == "install" || call.Args[0] == "uninstall") {
					t.Fatalf("non-install transition executed package mutation: %v", call)
				}
			}
		})
	}
}
func TestPipxExecutorInstallIsVerifiedTrackedAndIdempotent(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	runner := &pipxInventoryRunner{}
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{"pipx"},
		Methods:    []*config.MethodCandidate{{Kind: "pipx", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}},
	}
	schema := &config.Schema{
		Defaults: config.Defaults{Manager: "pipx", MethodOrder: []string{"pipx"}},
		Tools:    map[string]*config.Tool{"demo": tool},
	}
	adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["pipx"])
	ex := executor.New()
	executor.WithRunner(runner)(ex)
	executor.WithAdapters(adapter)(ex)
	executor.WithSchemaInfo(filepath.Join(t.TempDir(), "schema.toml"), time.Now())(ex)

	first, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if len(first.Tools) != 1 || first.Tools[0].Status != executor.StatusInstalled {
		t.Fatalf("first Execute() tool result = %+v, want installed", first.Tools)
	}
	if !runner.installed || runner.version != "1.2.3" {
		t.Fatalf("provider inventory after install = installed:%v version:%q", runner.installed, runner.version)
	}
	if installs := pipxInstallCalls(runner.calls); len(installs) != 1 || !slices.Equal(installs[0].Args, []string{"install", "demo==1.2.3"}) {
		t.Fatalf("pipx install calls = %v, want exactly one pinned install", installs)
	}
	tracked, err := state.Load()
	if err != nil {
		t.Fatalf("load committed state: %v", err)
	}
	toolState, ok := tracked.Tools["demo"]
	if !ok || toolState.MethodKind != "pipx" || toolState.Version != "1.2.3" {
		t.Fatalf("tracked tool state = %+v (present=%v), want pipx/1.2.3", toolState, ok)
	}

	second, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("repeat Execute() error = %v", err)
	}
	if len(second.Tools) != 1 || second.Tools[0].Status != executor.StatusAlready {
		t.Fatalf("repeat Execute() tool result = %+v, want already", second.Tools)
	}
	if installs := pipxInstallCalls(runner.calls); len(installs) != 1 {
		t.Fatalf("repeat execution added pipx install calls: %v", installs)
	}
}

func TestPipxExecutorRejectsUnverifiedPostInstallInventory(t *testing.T) {
	tests := []struct {
		name                string
		installedVersion    string
		installedVersionSet bool
		probeError          string
	}{
		{name: "wrong version", installedVersion: "2.0.0", installedVersionSet: true},
		{name: "unknown version", installedVersionSet: true},
		{name: "broken inventory", probeError: "Authorization: Bearer verification-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			runner := &pipxInventoryRunner{installVersion: tt.installedVersion, installVersionSet: tt.installedVersionSet, postInstallError: tt.probeError}
			tool := &config.Tool{Name: "demo", MethodOnly: []string{"pipx", "cargo"}, PostInstall: []config.Hook{{Run: []string{"post-install-marker"}}}, Methods: []*config.MethodCandidate{{Kind: "pipx", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}, {Kind: "cargo", Config: map[string]any{"pkg": "fallback"}}}}
			schema := &config.Schema{Defaults: config.Defaults{Manager: "pipx", MethodOrder: []string{"pipx", "cargo"}}, Tools: map[string]*config.Tool{"demo": tool}}
			ex := executor.New()
			executor.WithRunner(runner)(ex)
			executor.WithAdapters(ecosystem.NewBaseAdapter(ecosystem.Configs["pipx"]), ecosystem.NewBaseAdapter(ecosystem.Configs["cargo"]))(ex)
			executor.WithAllowArbitraryCode()(ex)
			report, err := ex.Execute(context.Background(), schema, "linux")
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if len(report.Tools) != 1 || report.Tools[0].Status != executor.StatusFailed {
				t.Fatalf("Execute() report = %+v, want failed post-install verification", report)
			}
			if len(report.Tools[0].Methods) != 1 || report.Tools[0].Methods[0].Status != "failed" {
				t.Fatalf("method attempts = %+v, want terminal failed attempt", report.Tools[0].Methods)
			}
			if tt.probeError != "" && strings.Contains(report.Tools[0].Error, "verification-secret") {
				t.Fatalf("verification error leaked secret: %q", report.Tools[0].Error)
			}
			for _, call := range runner.calls {
				if call.Name == "post-install-marker" {
					t.Fatalf("post-install hook ran after verification failure: %#v", runner.calls)
				}
			}
			if installs := pipxInstallCalls(runner.calls); len(installs) != 1 {
				t.Fatalf("pipx install calls = %v, want one mutation and no retry", installs)
			}
			tracked, loadErr := state.Load()
			if loadErr != nil {
				t.Fatalf("load state: %v", loadErr)
			}
			if _, ok := tracked.Tools["demo"]; ok || len(tracked.OwnedResources) != 0 {
				t.Fatalf("unverified install adopted into state: tools=%v ownership=%v", tracked.Tools, tracked.OwnedResources)
			}
			if len(tracked.PreparationJournals) != 0 {
				t.Fatalf("non-transactional attempt fabricated preparation journal: %v", tracked.PreparationJournals)
			}
		})
	}
}

func pipxInstallCalls(calls []run.FakeCall) []run.FakeCall {
	var installs []run.FakeCall
	for _, call := range calls {
		if call.Name == "pipx" && len(call.Args) > 0 && call.Args[0] == "install" {
			installs = append(installs, call)
		}
	}
	return installs
}
