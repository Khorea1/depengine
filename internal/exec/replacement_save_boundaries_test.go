package exec

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type replacementSaveBoundaryAdapter struct {
	*replacementRecoveryAdapter
	removeErr    error
	afterRemove  func()
	afterInstall func()
}

func (a *replacementSaveBoundaryAdapter) Remove(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate) error {
	a.removed = append(a.removed, tool.Name)
	if a.removeErr != nil {
		return a.removeErr
	}
	delete(a.installed, tool.Name)
	if a.afterRemove != nil {
		a.afterRemove()
	}
	return nil
}

func (a *replacementSaveBoundaryAdapter) InstallResolved(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan) error {
	if a.installed == nil {
		a.installed = make(map[string]string)
	}
	a.installed[tool.Name] = desired.Identity.Version
	a.order = append(a.order, tool.Name)
	if a.afterInstall != nil {
		a.afterInstall()
	}
	return nil
}

type replacementSaveBoundaryRunner struct {
	calls    []string
	afterRun func()
}

func (r *replacementSaveBoundaryRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, name)
	if r.afterRun != nil {
		r.afterRun()
	}
	return run.Result{}
}

func TestReplacementExecutorSaveFailuresStopMutationsAndPreserveRecoveryState(t *testing.T) {
	cases := []struct {
		name             string
		fault            string
		wantError        bool
		wantRemoved      int
		wantInstalled    int
		wantHooks        int
		wantDurablePhase plan.ReplacementPhase
		saveBoundary     string
		wantVersion      string
		wantPostDone     bool
		wantNoWAL        bool
	}{
		{name: "begin save", fault: "begin", wantError: true, wantVersion: "1.0.0", wantPostDone: true, wantNoWAL: true},
		{name: "removal boundary save", fault: "removal-boundary", saveBoundary: "removal_boundary", wantError: true, wantDurablePhase: plan.ReplacementPlanned, wantVersion: "1.0.0", wantPostDone: true},
		{name: "remove failure", fault: "remove-failure", wantRemoved: 1, wantDurablePhase: plan.ReplacementRemoving, wantVersion: "1.0.0", wantPostDone: true},
		{name: "removed state save", fault: "removed-state", wantError: true, wantRemoved: 1, wantDurablePhase: plan.ReplacementRemoving, wantVersion: "1.0.0", wantPostDone: true},
		{name: "install boundary save", fault: "install-boundary", saveBoundary: "install_boundary", wantError: true, wantRemoved: 1, wantDurablePhase: plan.ReplacementRemoved, wantVersion: "1.0.0", wantPostDone: true},
		{name: "installed commit save", fault: "installed-commit", wantError: true, wantRemoved: 1, wantInstalled: 1, wantDurablePhase: plan.ReplacementInstalling, wantVersion: "1.0.0", wantPostDone: true},
		{name: "post-hook-running boundary save", fault: "post-hook-running-boundary", saveBoundary: "post_hook_running", wantError: true, wantRemoved: 1, wantInstalled: 1, wantDurablePhase: plan.ReplacementInstalled, wantVersion: "2.0.0", wantPostDone: false},
		{name: "final clear save", fault: "final-clear", wantError: true, wantRemoved: 1, wantInstalled: 1, wantHooks: 1, wantDurablePhase: plan.ReplacementPostHookRunning, wantVersion: "2.0.0", wantPostDone: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			const toolName = "demo"
			previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", PostinstallDone: true, RootRequested: true, Config: map[string]any{"pkg": "package-demo", "version": "1.0.0"}}
			locked, err := state.LoadLocked()
			if err != nil {
				t.Fatal(err)
			}
			locked.State().Tools[toolName] = previous
			if err := locked.Save(); err != nil {
				_ = locked.Close()
				t.Fatal(err)
			}
			if err := locked.Close(); err != nil {
				t.Fatal(err)
			}

			blocker := state.DefaultPath() + ".tmp"
			blockSave := func() {
				t.Helper()
				if err := os.Mkdir(blocker, 0700); err != nil {
					t.Fatalf("create state-save blocker: %v", err)
				}
			}
			unblockSave := func() {
				t.Helper()
				if _, err := os.Stat(blocker); err == nil {
					if err := os.Remove(blocker); err != nil {
						t.Fatalf("remove state-save blocker: %v", err)
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stat state-save blocker: %v", err)
				}
			}

			adapter := &replacementSaveBoundaryAdapter{
				replacementRecoveryAdapter: &replacementRecoveryAdapter{
					executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
					installed:               map[string]string{toolName: "1.0.0"},
				},
			}
			runner := &replacementSaveBoundaryRunner{}
			switch tc.fault {
			case "begin":
				blockSave()
			case "remove-failure":
				adapter.removeErr = errors.New("remove refused")
			case "removed-state":
				adapter.afterRemove = blockSave
			case "installed-commit":
				adapter.afterInstall = blockSave
			case "final-clear":
				runner.afterRun = blockSave
			}

			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "package-demo", "version": "2.0.0"}}
			hook := config.Hook{Run: []string{"lifecycle-hook", tc.fault}}
			tool := &config.Tool{Name: toolName, Methods: []*config.MethodCandidate{method}, PostInstall: []config.Hook{hook}}
			schema := &config.Schema{Tools: map[string]*config.Tool{toolName: tool}}
			resolved := plan.New(toolName, "npm", true)
			resolved.Identity.Package = "package-demo"
			resolved.Identity.Version = "2.0.0"
			resolved.Hooks = []plan.LifecycleHook{{
				ID: "post-install", Transition: plan.TransitionUpgrade, Timing: plan.HookAfter,
				Operation:     plan.Operation{Kind: "hook", Effect: plan.EffectMutation, Command: []string{"lifecycle-hook", tc.fault}, ArbitraryCode: true},
				FailurePolicy: plan.HookFailAbort,
			}}

			ex := New()
			if tc.saveBoundary != "" {
				ex.beforeReplacementSave = func(boundary string) {
					if boundary == tc.saveBoundary {
						blockSave()
					}
				}
			}
			WithRunner(runner)(ex)
			WithAdapters(adapter)(ex)
			WithSchemaInfo("schema.yaml", time.Time{})(ex)
			WithAllowArbitraryCode()(ex)
			result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, method, &resolved, previous)
			unblockSave()
			if (err != nil) != tc.wantError {
				t.Fatalf("execution error = %v, wantError=%t", err, tc.wantError)
			}
			if result.Status != StatusFailed {
				t.Fatalf("status = %v, want failed: %+v", result.Status, result)
			}
			if got := len(adapter.removed); got != tc.wantRemoved {
				t.Errorf("remove calls = %d, want %d", got, tc.wantRemoved)
			}
			if got := len(adapter.order); got != tc.wantInstalled {
				t.Errorf("install calls = %d, want %d", got, tc.wantInstalled)
			}
			if got := len(runner.calls); got != tc.wantHooks {
				t.Errorf("hook calls = %d, want %d", got, tc.wantHooks)
			}

			persisted, err := state.Load()
			if err != nil {
				t.Fatal(err)
			}
			tracked := persisted.Tools[toolName]
			if tracked.Version != tc.wantVersion || tracked.PostinstallDone != tc.wantPostDone {
				t.Errorf("durable tool state = version %q postinstall_done=%t, want %q/%t", tracked.Version, tracked.PostinstallDone, tc.wantVersion, tc.wantPostDone)
			}
			if tc.wantNoWAL {
				if len(persisted.ReplacementTransactions) != 0 {
					t.Errorf("failed begin left %d durable replacement transactions", len(persisted.ReplacementTransactions))
				}
			} else {
				tx, ok := persisted.ReplacementTransactions[toolName]
				if !ok || tx.Journal.Phase != tc.wantDurablePhase {
					t.Errorf("durable replacement WAL = %+v, present=%t; want phase %q", tx, ok, tc.wantDurablePhase)
				}
			}
			if got := persisted.OwnedResources; !reflect.DeepEqual(got, []plan.OwnedResourceState(nil)) {
				t.Errorf("durable resources after failed replacement = %+v, want none", got)
			}
		})
	}
}
