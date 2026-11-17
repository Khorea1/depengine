package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

// Reproduce interruptions around a real versioned replacement transaction.
// Before the old version is removed, the desired identity is Drifted rather
// than Absent. Recovery must revalidate the old identity before retrying.
func TestVersionedReplacementRecoveryAfterInterruption(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		phase                   plan.ReplacementPhase
		hostVersion, oldVersion string
		wantRemove, wantInstall int
		blocked                 bool
	}{
		{"planned, old version present", plan.ReplacementPlanned, "1.0.0", "1.0.0", 1, 1, false},
		{"removing, old version present", plan.ReplacementRemoving, "1.0.0", "1.0.0", 1, 1, false},
		{"removing, removal already completed", plan.ReplacementRemoving, "", "1.0.0", 0, 1, false},
		{"removed, target absent", plan.ReplacementRemoved, "", "1.0.0", 0, 1, false},
		{"installing, target absent", plan.ReplacementInstalling, "", "1.0.0", 0, 1, false},
		{"installing, target applied before crash", plan.ReplacementInstalling, "2.0.0", "1.0.0", 0, 0, false},
		{"installed, commit pending", plan.ReplacementInstalled, "2.0.0", "1.0.0", 0, 0, false},
		{"legacy missing concrete old version", plan.ReplacementRemoving, "1.0.0", "", 0, 0, true},
		{"old version unexpectedly changed", plan.ReplacementRemoving, "3.0.0", "1.0.0", 0, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "demo", "version": "2.0.0"}}
			tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}
			schema := &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}
			desired, err := candidatePlanIntentErr(tool, method)
			if err != nil || desired == nil {
				t.Fatalf("candidatePlanIntentErr() = %+v, %v", desired, err)
			}
			projection, err := plan.ProjectLock(*desired)
			if err != nil {
				t.Fatal(err)
			}
			previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: tt.oldVersion,
				Config: map[string]any{"pkg": "demo", "version": "1.0.0"}, RootRequested: true}
			locked, err := state.LoadLocked()
			if err != nil {
				t.Fatal(err)
			}
			locked.State().Tools["demo"] = previous
			candidate := state.ReplacementCandidate{Identity: projection.Candidate}
			if err := locked.BeginReplacement("demo", "npm", candidate, candidate, previous, projection, "", nil); err != nil {
				t.Fatal(err)
			}
			switch tt.phase {
			case plan.ReplacementPlanned:
			case plan.ReplacementRemoving:
				err = locked.PlanReplacementRemoval("demo")
			case plan.ReplacementRemoved, plan.ReplacementInstalling, plan.ReplacementInstalled:
				if err = locked.PlanReplacementRemoval("demo"); err == nil {
					err = locked.RecordReplacementRemoved("demo")
				}
				if err == nil && tt.phase != plan.ReplacementRemoved {
					err = locked.PlanReplacementInstall("demo")
				}
				if err == nil && tt.phase == plan.ReplacementInstalled {
					next := previous
					next.Version = "2.0.0"
					err = locked.CommitReplacementInstallWithPreparation("demo", next, nil, "", plan.PreparationPlan{}, "demo")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := locked.Close(); err != nil {
				t.Fatal(err)
			}
			installed := make(map[string]string)
			if tt.hostVersion != "" {
				installed["demo"] = tt.hostVersion
			}
			adapter := &replacementRecoveryAdapter{
				executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
				installed:               installed,
			}
			ex := New()
			WithRunner(&run.FakeRunner{})(ex)
			WithAdapters(adapter)(ex)
			WithSchemaInfo("schema.yaml", time.Time{})(ex)
			rc := ex.newRunContext(context.Background(), schema, "")
			err = ex.recoverAndRecord(context.Background(), rc)
			if tt.blocked {
				if err == nil || !strings.Contains(err.Error(), "ambiguous") {
					t.Fatalf("error = %v; want fail-closed recovery", err)
				}
				if len(adapter.removed) != 0 || len(adapter.order) != 0 {
					t.Fatalf("unsafe mutation: removed=%v installed=%v", adapter.removed, adapter.order)
				}
				persisted, err := state.Load()
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := persisted.ReplacementTransactions["demo"]; !ok {
					t.Fatal("lost unresolved WAL")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(adapter.removed) != tt.wantRemove || len(adapter.order) != tt.wantInstall || adapter.installed["demo"] != "2.0.0" {
				t.Fatalf("recover calls removed=%v installed=%v version=%q; want %d, %d, 2.0.0", adapter.removed, adapter.order, adapter.installed["demo"], tt.wantRemove, tt.wantInstall)
			}
			persisted, err := state.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(persisted.ReplacementTransactions) != 0 || persisted.Tools["demo"].Version != "2.0.0" || persisted.Tools["demo"].MethodKind != "npm" {
				t.Fatalf("recovered state = %+v", persisted)
			}
			if len(rc.recoveredCommits) != 1 {
				t.Fatalf("recovered commits = %d", len(rc.recoveredCommits))
			}
		})
	}
}

// Snap channel drift is actionable in verification, but a tracking channel
// is mutable. The existing destructive replacement WAL intentionally refuses
// to start without an immutable concrete target. This must not silently become
// a remove/install just because the channel comparison now detects drift.
func TestSnapChannelReplacementRequiresImmutableTarget(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	method := &config.MethodCandidate{Kind: "snap", Config: map[string]any{"pkg": "demo", "channel": "beta"}}
	tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}
	desired, err := candidatePlanIntentErr(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := plan.ProjectLock(*desired)
	if err != nil {
		t.Fatal(err)
	}
	if err := projection.RequireImmutable(); err == nil {
		t.Fatal("mutable Snap channel gained an unearned immutable lock")
	}
	previous := state.ToolState{Method: "snap", MethodKind: "snap", Version: "1.0", Config: map[string]any{"pkg": "demo", "channel": "stable"}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools["demo"] = previous
	candidate := state.ReplacementCandidate{Identity: projection.Candidate}
	if err := locked.BeginReplacement("demo", "snap", candidate, candidate, previous, projection, "", nil); err == nil || !strings.Contains(err.Error(), "not immutable") {
		t.Fatalf("mutable target replacement = %v, want fail-closed error", err)
	}
	if len(locked.State().ReplacementTransactions) != 0 {
		t.Fatal("invalid replacement WAL was persisted")
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
}
