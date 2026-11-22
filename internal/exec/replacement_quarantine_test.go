package exec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type quarantiningAdapter struct {
	*replacementRecoveryAdapter
	quarantined     bool
	committed       bool
	rolledBack      bool
	installErr      error
	commitErr       error
	previousVersion string
}

func (a *quarantiningAdapter) QuarantineForReplacement(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (func() error, func() error, error) {
	a.quarantined = true
	delete(a.installed, "demo")
	return func() error {
			a.committed = true
			return a.commitErr
		}, func() error {
			a.rolledBack = true
			a.installed["demo"] = a.previousVersion
			return nil
		}, nil
}

func (a *quarantiningAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan) error {
	if a.installErr != nil {
		return a.installErr
	}
	return a.replacementRecoveryAdapter.InstallResolved(ctx, rn, tool, method, desired)
}

func TestExecutorQuarantinesReplacementUntilVerified(t *testing.T) {
	for _, tc := range []struct {
		name            string
		installErr      error
		commitErr       error
		wantStatus      StatusEnum
		wantCommitted   bool
		wantRolledBack  bool
		wantVersion     string
		wantInstalled   int
		wantQuarantined bool
	}{
		{name: "success", wantStatus: StatusInstalled, wantCommitted: true, wantVersion: "2.0.0", wantInstalled: 1, wantQuarantined: true},
		{name: "install failure", installErr: errors.New("install refused"), wantStatus: StatusFailed, wantRolledBack: true, wantVersion: "1.0.0", wantQuarantined: true},
		{name: "cleanup failure keeps install", commitErr: errors.New("backup busy"), wantStatus: StatusInstalled, wantCommitted: true, wantVersion: "2.0.0", wantInstalled: 1, wantQuarantined: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			const toolName = "demo"
			previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", RootRequested: true, Config: map[string]any{"pkg": "package-demo", "version": "1.0.0"}}
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

			adapter := &quarantiningAdapter{
				replacementRecoveryAdapter: &replacementRecoveryAdapter{
					executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
					installed:               map[string]string{toolName: "1.0.0"},
				},
				installErr:      tc.installErr,
				commitErr:       tc.commitErr,
				previousVersion: "1.0.0",
			}
			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "package-demo", "version": "2.0.0"}}
			tool := &config.Tool{Name: toolName, Methods: []*config.MethodCandidate{method}}
			schema := &config.Schema{Tools: map[string]*config.Tool{toolName: tool}}
			resolved := plan.New(toolName, "npm", true)
			resolved.Identity.Package = "package-demo"
			resolved.Identity.Version = "2.0.0"

			ex := New()
			WithRunner(&run.FakeRunner{})(ex)
			WithAdapters(adapter)(ex)
			WithSchemaInfo("schema.yaml", time.Time{})(ex)
			result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, method, &resolved, previous)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.wantStatus {
				t.Fatalf("status = %v, want %v: %+v", result.Status, tc.wantStatus, result)
			}
			if adapter.quarantined != tc.wantQuarantined {
				t.Fatalf("quarantined = %t, want %t", adapter.quarantined, tc.wantQuarantined)
			}
			if adapter.committed != tc.wantCommitted || adapter.rolledBack != tc.wantRolledBack {
				t.Fatalf("commit/rollback = %t/%t, want %t/%t", adapter.committed, adapter.rolledBack, tc.wantCommitted, tc.wantRolledBack)
			}
			if len(adapter.removed) != 0 {
				t.Fatalf("ordinary removal ran despite quarantine: %v", adapter.removed)
			}
			if got := len(adapter.order); got != tc.wantInstalled {
				t.Fatalf("install calls = %d, want %d", got, tc.wantInstalled)
			}
			persisted, err := state.Load()
			if err != nil {
				t.Fatal(err)
			}
			if got := persisted.Tools[toolName].Version; got != tc.wantVersion {
				t.Fatalf("persisted version = %q, want %q", got, tc.wantVersion)
			}
		})
	}
}
