package exec

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/source"
	"github.com/Khorea1/depengine/internal/state"
)

func sourceBackedSchema() *config.Schema {
	return &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"cargo"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name:       "demo",
				MethodOnly: []string{"cargo"},
				Methods: []*config.MethodCandidate{{
					Kind:    "cargo",
					Config:  map[string]any{"pkg": "demo"},
					Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}},
				}},
			},
		},
	}
}

type preparationPlanCapturingAdapter struct {
	*testMockAdapter
	installed *plan.ResolvedInstallPlan
}

func (a *preparationPlanCapturingAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	a.installed = resolved
	return a.Install(ctx, rn, tool, mc)
}

func TestExecutorDryRunProjectsPreparationWithoutPersistingJournal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{{}}}
	installCalled := false
	adapter := &testMockAdapter{
		kindValue: "cargo",
		checkFunc: func(string) bool { return false },
		installFunc: func(string) error {
			installCalled = true
			return nil
		},
	}
	var output bytes.Buffer
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)
	WithDryRun()(ex)
	WithOutput(&output)(ex)

	report, err := ex.Execute(context.Background(), sourceBackedSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if installCalled {
		t.Fatal("dry-run executed candidate install")
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusWouldInstall {
		t.Fatalf("report = %+v, want one planned install", report.Tools)
	}
	intent := report.Tools[0].PlanIntent
	if intent == nil || intent.Preparation == nil {
		t.Fatalf("dry-run plan intent = %#v, want runtime preparation projection", intent)
	}
	if len(intent.Preparation.Prepare) != 1 || intent.Preparation.Prepare[0].Apply.Kind != "add-source" {
		t.Fatalf("prepare phase = %#v, want one add-source mutation", intent.Preparation.Prepare)
	}
	if len(intent.Preparation.Commit) != 1 || intent.Preparation.Commit[0].Kind != "install" {
		t.Fatalf("commit phase = %#v, want install operation", intent.Preparation.Commit)
	}
	gotOutput := output.String()
	if !strings.Contains(gotOutput, "prepare: would add source brew-tap vendor/tools") {
		t.Fatalf("dry-run output missing prepare phase:\n%s", gotOutput)
	}
	if !strings.Contains(gotOutput, "commit: would install via cargo") {
		t.Fatalf("dry-run output missing commit phase:\n%s", gotOutput)
	}
	if _, err := os.Stat(state.DefaultPath()); !os.IsNotExist(err) {
		t.Fatalf("dry-run persisted state or WAL: stat err=%v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != "brew" {
		t.Fatalf("dry-run host calls = %#v, want read-only source probe only", runner.calls)
	}
}

func TestExecutorFinalizesDurableSourcePreparation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{{}, {}}}
	adapter := &testMockAdapter{
		kindValue:   "cargo",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return nil },
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), sourceBackedSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("report = %+v, want installed", report.Tools)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 0 || len(st.PreparationJournals) != 0 {
		t.Fatalf("completed transaction remained active: plans=%#v journals=%#v", st.PreparationPlans, st.PreparationJournals)
	}
	identity, err := source.ResourceIdentity(config.Source{Kind: "brew-tap", Name: "vendor/tools"})
	if err != nil {
		t.Fatal(err)
	}
	want := []plan.OwnedResourceState{{
		Resource: identity, Ownership: plan.OwnershipDepengine, Dependents: []string{"demo"},
	}}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].Resource != want[0].Resource || st.OwnedResources[0].Ownership != want[0].Ownership || len(st.OwnedResources[0].Dependents) != 1 || st.OwnedResources[0].Dependents[0] != "demo" {
		t.Fatalf("owned resources = %#v, want %#v", st.OwnedResources, want)
	}
}

func TestExecutorInstallResolvedReceivesReportedPreparationPlan(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{{}, {}}}
	adapter := &preparationPlanCapturingAdapter{testMockAdapter: &testMockAdapter{
		kindValue:   "cargo",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return nil },
	}}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), sourceBackedSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled {
		t.Fatalf("report = %+v, want one installed tool", report.Tools)
	}
	if adapter.installed == nil || adapter.installed.Preparation == nil {
		t.Fatalf("InstallResolved plan = %#v, want preparation projection", adapter.installed)
	}
	if report.Tools[0].PlanIntent != adapter.installed {
		t.Fatalf("reported plan = %p, InstallResolved plan = %p; want exact same plan", report.Tools[0].PlanIntent, adapter.installed)
	}
}

func TestExecutorLeavesCommittingJournalWhenInstallOutcomeIsUnresolved(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{{}, {}}}
	adapter := &testMockAdapter{
		kindValue:   "cargo",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return errors.New("installer interrupted") },
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), sourceBackedSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusFailed {
		t.Fatalf("report = %+v, want failed", report.Tools)
	}
	if !strings.Contains(report.Tools[0].Error, "commit outcome is unresolved") {
		t.Fatalf("error = %q, want unresolved commit", report.Tools[0].Error)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 1 || len(st.PreparationJournals) != 1 {
		t.Fatalf("active transaction = plans=%#v journals=%#v, want exactly one", st.PreparationPlans, st.PreparationJournals)
	}
	for key, journal := range st.PreparationJournals {
		if journal.Status != plan.PreparationCommitting {
			t.Fatalf("journal %q status = %q, want %q", key, journal.Status, plan.PreparationCommitting)
		}
		if _, ok := st.PreparationPlans[key]; !ok {
			t.Fatalf("journal %q has no persisted plan", key)
		}
	}
	if len(st.OwnedResources) != 0 {
		t.Fatalf("ownership committed before target reconciliation: %#v", st.OwnedResources)
	}

	// A later run must stop before making another mutation. Replaying either the
	// install or rollback would guess whether the interrupted install applied.
	secondRunner := &sequenceRunner{}
	second := New()
	WithRunner(secondRunner)(second)
	WithAdapters(&testMockAdapter{kindValue: "cargo"})(second)
	WithSchemaInfo("/test/schema.toml", time.Now())(second)
	_, err = second.Execute(context.Background(), sourceBackedSchema(), "")
	if err == nil || !strings.Contains(err.Error(), "commit outcome") || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("second Execute error = %v, want fail-closed unresolved commit", err)
	}
	if len(secondRunner.calls) != 0 {
		t.Fatalf("recovery made host calls before commit reconciliation: %#v", secondRunner.calls)
	}
}

func TestExecutorReconcilesCommittingJournalWhenTargetIsSatisfied(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{{}, {}}}
	first := New()
	WithRunner(runner)(first)
	WithAdapters(&testMockAdapter{
		kindValue:   "cargo",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return errors.New("installer interrupted") },
	})(first)
	WithSchemaInfo("/test/schema.toml", time.Now())(first)
	if _, err := first.Execute(context.Background(), sourceBackedSchema(), ""); err != nil {
		t.Fatal(err)
	}

	checks := 0
	second := New()
	WithRunner(&sequenceRunner{})(second)
	WithAdapters(&testMockAdapter{
		kindValue: "cargo",
		checkFunc: func(string) bool {
			checks++
			return true
		},
	})(second)
	WithSchemaInfo("/test/schema.toml", time.Now())(second)
	report, err := second.Execute(context.Background(), sourceBackedSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("candidate checks = %d, want recovery reconciliation only", checks)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || !report.Tools[0].InstallCommitted {
		t.Fatalf("report = %+v, want terminal recovered install", report.Tools)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 0 || len(st.PreparationJournals) != 0 {
		t.Fatalf("reconciled commit remained active: plans=%#v journals=%#v", st.PreparationPlans, st.PreparationJournals)
	}
	if _, ok := st.Tools["demo"]; !ok {
		t.Fatalf("reconciled installed tool missing from state: %#v", st.Tools)
	}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].Ownership != plan.OwnershipDepengine || len(st.OwnedResources[0].Dependents) != 1 || st.OwnedResources[0].Dependents[0] != "demo" {
		t.Fatalf("reconciled source ownership = %#v", st.OwnedResources)
	}
}

func TestCandidatePreparationKeyRoundTripsRecoverySubject(t *testing.T) {
	intent, mismatch := candidatePlanIntent(sourceBackedSchema().Tools["demo"], sourceBackedSchema().Tools["demo"].Methods[0])
	if mismatch != "" || intent == nil {
		t.Fatalf("intent = %#v, mismatch = %q", intent, mismatch)
	}
	key, err := candidatePreparationKey("demo", "cargo", intent)
	if err != nil {
		t.Fatal(err)
	}
	tool, method, err := candidatePreparationSubject(key)
	if err != nil {
		t.Fatal(err)
	}
	if tool != "demo" || method != "cargo" {
		t.Fatalf("subject = %q/%q, want demo/cargo", tool, method)
	}
	if strings.Contains(key, "demo") || strings.Contains(key, "cargo") {
		t.Fatalf("candidate key unexpectedly embeds raw path components: %q", key)
	}
}

func TestRecoverPreparationResolvesInFlightSourceAddAndRollsBack(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	configured := config.Source{Kind: "brew-tap", Name: "vendor/tools"}
	preparationPlan, err := source.PreparationPlan([]config.Source{configured})
	if err != nil {
		t.Fatal(err)
	}
	const key = "candidate/recovery-test"
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locked.BeginPreparation(key, preparationPlan); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if _, err := locked.PlanPreparationApply(key, preparationPlan, preparationPlan.Prepare[0].ID); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	// First probe resolves the interrupted add as applied. The rollback then
	// performs its idempotency probe and finally the untap mutation.
	runner := &sequenceRunner{results: []run.Result{
		{Stdout: []byte("vendor/tools\n")},
		{Stdout: []byte("vendor/tools\n")},
		{},
	}}
	ex := New()
	WithRunner(runner)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)
	ex.sources = source.NewManager(runner, false)
	if err := ex.recoverPreparationTransactions(context.Background()); err != nil {
		t.Fatal(err)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 0 || len(st.PreparationJournals) != 0 {
		t.Fatalf("recovered transaction remained active: plans=%#v journals=%#v", st.PreparationPlans, st.PreparationJournals)
	}
	if len(st.OwnedResources) != 0 {
		t.Fatalf("rolled-back source retained ownership: %#v", st.OwnedResources)
	}
	if len(runner.calls) != 3 || runner.calls[2].Name != "brew" || len(runner.calls[2].Args) != 2 || runner.calls[2].Args[0] != "untap" || runner.calls[2].Args[1] != "vendor/tools" {
		t.Fatalf("recovery calls = %#v, want probe, rollback probe, brew untap", runner.calls)
	}
}

func TestNeedsElevationIncludesPersistedSourceRecovery(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	preparationPlan, err := source.PreparationPlan([]config.Source{{Kind: "apt-ppa", Name: "ppa:vendor/tools"}})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locked.BeginPreparation("candidate/elevated-recovery", preparationPlan); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	ex := New()
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)
	if !ex.needsElevation(&config.Schema{Tools: map[string]*config.Tool{}}, "") {
		t.Fatal("persisted apt source recovery must request an elevation session")
	}
}

type versionedPreparationAdapter struct {
	testMockAdapter
	version    string
	versionErr error
}

func (a *versionedPreparationAdapter) InstalledVersion(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (string, error) {
	return a.version, a.versionErr
}

func sourceBackedVersionedSchema(version string) *config.Schema {
	schema := sourceBackedSchema()
	schema.Tools["demo"].Methods[0].Config["version"] = version
	return schema
}

type recoveryObservationAdapter struct {
	testMockAdapter
	observation plan.Observation
	observeErr  error
	observeCall int
	checkCalls  int
	version     string
}

func (a *recoveryObservationAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	a.checkCalls++
	return false
}

func (a *recoveryObservationAdapter) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	a.observeCall++
	return a.observation, a.observeErr
}

func (a *recoveryObservationAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *recoveryObservationAdapter) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	return nil
}

func (a *recoveryObservationAdapter) InstalledVersion(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (string, error) {
	return a.version, nil
}

func recoveryObservationIntent(t *testing.T) (*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) {
	t.Helper()
	schema := sourceBackedVersionedSchema("1.2.3")
	tool := schema.Tools["demo"]
	method := tool.Methods[0]
	intent, mismatch := candidatePlanIntent(tool, method)
	if mismatch != "" || intent == nil {
		t.Fatalf("intent = %#v, mismatch = %q", intent, mismatch)
	}
	return tool, method, intent
}

func TestObserveRecoveryCandidateUsesV2IdentityAndLegacyCheck(t *testing.T) {
	tests := []struct {
		name          string
		adapter       func() *recoveryObservationAdapter
		wantPresence  plan.PresenceState
		wantPackage   string
		wantVersion   string
		wantKnown     []plan.IdentityField
		wantCheckCall int
		wantDetail    string
	}{
		{
			name: "v2 present",
			adapter: func() *recoveryObservationAdapter {
				return &recoveryObservationAdapter{
					testMockAdapter: testMockAdapter{kindValue: "cargo"},
					observation: plan.Observation{
						Presence:    plan.PresencePresent,
						Identity:    plan.ObservedIdentity{Package: "demo"},
						KnownFields: []plan.IdentityField{plan.FieldPackage},
					},
					version: "1.2.3",
				}
			},
			wantPresence: plan.PresencePresent,
			wantPackage:  "demo",
			wantVersion:  "1.2.3",
			wantKnown:    []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
		},
		{
			name: "v2 absent",
			adapter: func() *recoveryObservationAdapter {
				return &recoveryObservationAdapter{testMockAdapter: testMockAdapter{kindValue: "cargo"}, observation: plan.Observation{Presence: plan.PresenceAbsent}}
			},
			wantPresence: plan.PresenceAbsent,
		},
		{
			name: "v2 unknown",
			adapter: func() *recoveryObservationAdapter {
				return &recoveryObservationAdapter{testMockAdapter: testMockAdapter{kindValue: "cargo"}, observation: plan.Observation{Presence: plan.PresenceUnknown, Detail: "not supported"}}
			},
			wantPresence: plan.PresenceUnknown,
			wantDetail:   "not supported",
		},
		{
			name: "v2 broken",
			adapter: func() *recoveryObservationAdapter {
				return &recoveryObservationAdapter{testMockAdapter: testMockAdapter{kindValue: "cargo"}, observation: plan.Observation{Presence: plan.PresenceBroken, Detail: "backend failed"}}
			},
			wantPresence: plan.PresenceBroken,
			wantDetail:   "backend failed",
		},
		{
			name: "v2 observe error",
			adapter: func() *recoveryObservationAdapter {
				return &recoveryObservationAdapter{
					testMockAdapter: testMockAdapter{kindValue: "cargo"},
					observeErr:      errors.New("request https://user:secret@example.test/?token=topsecret failed"),
				}
			},
			wantPresence: plan.PresenceBroken,
			wantDetail:   "observe recovery candidate failed:",
		},
	}

	tool, method, intent := recoveryObservationIntent(t)
	ex := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := tt.adapter()
			observation := ex.observeRecoveryCandidate(context.Background(), tool, method, intent, adapter)
			if observation.Presence != tt.wantPresence || observation.Identity.Package != tt.wantPackage || observation.Identity.Version != tt.wantVersion {
				t.Fatalf("observation = %#v, want presence=%q package=%q version=%q", observation, tt.wantPresence, tt.wantPackage, tt.wantVersion)
			}
			if !reflect.DeepEqual(observation.KnownFields, tt.wantKnown) {
				t.Fatalf("known fields = %#v, want %#v", observation.KnownFields, tt.wantKnown)
			}
			if !strings.Contains(observation.Detail, tt.wantDetail) {
				t.Fatalf("detail = %q, want substring %q", observation.Detail, tt.wantDetail)
			}
			if adapter.observeCall != 1 || adapter.checkCalls != tt.wantCheckCall {
				t.Fatalf("Observe/Check calls = %d/%d, want 1/%d", adapter.observeCall, adapter.checkCalls, tt.wantCheckCall)
			}
			if tt.name == "v2 observe error" && (strings.Contains(observation.Detail, "secret") || strings.Contains(observation.Detail, "topsecret")) {
				t.Fatalf("observation detail leaked secret: %q", observation.Detail)
			}
		})
	}

	legacyChecks := 0
	legacy := &testMockAdapter{kindValue: "cargo", checkFunc: func(string) bool {
		legacyChecks++
		return true
	}}
	observation := ex.observeRecoveryCandidate(context.Background(), tool, method, intent, legacy)
	if observation.Presence != plan.PresencePresent || observation.Identity.Package != intent.Identity.Package {
		t.Fatalf("legacy observation = %#v, want present package identity", observation)
	}
	if legacyChecks != 1 {
		t.Fatalf("legacy Check calls = %d, want 1", legacyChecks)
	}
}

func TestObserveRecoveryCandidateNeverFinalizesAmbiguousCommit(t *testing.T) {
	tool, method, intent := recoveryObservationIntent(t)
	planDocument := plan.PreparationPlan{Commit: []plan.Operation{{Kind: "install-package", Effect: plan.EffectMutation}}}
	committing, err := plan.NewPreparationJournal().PlanCommit(planDocument)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		observation plan.Observation
		err         error
		wantAction  plan.PreparationRecoveryAction
	}{
		{name: "present", observation: plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "demo", Version: "1.2.3"}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion}}, wantAction: plan.RecoveryFinalizeCommit},
		{name: "absent", observation: plan.Observation{Presence: plan.PresenceAbsent}, wantAction: plan.RecoveryBlocked},
		{name: "unknown", observation: plan.Observation{Presence: plan.PresenceUnknown}, wantAction: plan.RecoveryBlocked},
		{name: "broken", observation: plan.Observation{Presence: plan.PresenceBroken}, wantAction: plan.RecoveryBlocked},
		{name: "observe error", err: errors.New("observe failed: https://user:secret@example.test/?token=topsecret"), wantAction: plan.RecoveryBlocked},
	}

	ex := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &recoveryObservationAdapter{testMockAdapter: testMockAdapter{kindValue: "cargo"}, observation: tt.observation, observeErr: tt.err}
			observed := ex.observeRecoveryCandidate(context.Background(), tool, method, intent, adapter)
			decision, err := committing.RecoveryFor(planDocument, nil, &intent.Identity, &observed)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action != tt.wantAction {
				t.Fatalf("recovery action = %q, want %q (observation=%#v)", decision.Action, tt.wantAction, observed)
			}
		})
	}
}

func TestExecutorCommitRecoveryRequiresMatchingVersionEvidence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schema := sourceBackedVersionedSchema("1.2.3")
	first := New()
	WithRunner(&sequenceRunner{results: []run.Result{{}, {}}})(first)
	WithAdapters(&versionedPreparationAdapter{testMockAdapter: testMockAdapter{
		kindValue:   "cargo",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return errors.New("installer interrupted") },
	}})(first)
	WithSchemaInfo("/test/schema.toml", time.Now())(first)
	if _, err := first.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}

	drifted := New()
	WithRunner(&sequenceRunner{})(drifted)
	WithAdapters(&versionedPreparationAdapter{
		testMockAdapter: testMockAdapter{kindValue: "cargo", checkFunc: func(string) bool { return true }},
		version:         "1.2.2",
	})(drifted)
	WithSchemaInfo("/test/schema.toml", time.Now())(drifted)
	_, err := drifted.Execute(context.Background(), schema, "")
	if err == nil || !strings.Contains(err.Error(), string(plan.StateDrifted)) {
		t.Fatalf("drifted recovery error = %v, want drifted fail-closed state", err)
	}
	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationJournals) != 1 {
		t.Fatalf("drifted recovery changed active journals: %#v", st.PreparationJournals)
	}
	for _, journal := range st.PreparationJournals {
		if journal.Status != plan.PreparationCommitting {
			t.Fatalf("drifted recovery journal status = %q, want committing", journal.Status)
		}
	}

	matched := New()
	WithRunner(&sequenceRunner{})(matched)
	WithAdapters(&versionedPreparationAdapter{
		testMockAdapter: testMockAdapter{kindValue: "cargo", checkFunc: func(string) bool { return true }},
		version:         "1.2.3",
	})(matched)
	WithSchemaInfo("/test/schema.toml", time.Now())(matched)
	report, err := matched.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || !report.Tools[0].InstallCommitted {
		t.Fatalf("report = %+v, want terminal recovered install", report.Tools)
	}
	st, err = state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationJournals) != 0 || len(st.PreparationPlans) != 0 {
		t.Fatalf("matching recovery retained active transaction: plans=%#v journals=%#v", st.PreparationPlans, st.PreparationJournals)
	}
}

func TestRecoveredCommitDoesNotReplayHooksOrSecurityGate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schema := sourceBackedSchema()
	schema.Tools["demo"].PreInstall = []config.Hook{{Run: []string{"pre-hook"}}}
	firstRunner := &sequenceRunner{results: []run.Result{{}, {}, {}}}
	first := New()
	WithAllowArbitraryCode()(first)
	WithRunner(firstRunner)(first)
	WithAdapters(&testMockAdapter{
		kindValue:   "cargo",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return errors.New("installer interrupted") },
	})(first)
	WithSchemaInfo("/test/schema.toml", time.Now())(first)
	if _, err := first.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}
	if len(firstRunner.calls) != 3 || firstRunner.calls[0].Name != "pre-hook" {
		t.Fatalf("first-run calls = %#v, want pre-hook then source check/add", firstRunner.calls)
	}

	secondRunner := &sequenceRunner{}
	second := New()
	WithRunner(secondRunner)(second)
	WithAdapters(&testMockAdapter{kindValue: "cargo", checkFunc: func(string) bool { return true }})(second)
	WithSchemaInfo("/test/schema.toml", time.Now())(second)
	// Deliberately do not enable arbitrary code. Recovery must not execute or
	// re-gate a hook that belongs to the already-committed transition.
	report, err := second.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(secondRunner.calls) != 0 {
		t.Fatalf("recovered commit replayed host commands: %#v", secondRunner.calls)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || !report.Tools[0].InstallCommitted {
		t.Fatalf("report = %+v, want recovered committed install", report.Tools)
	}
}

func TestRecoveredDependencyOnlyCommitIsRecordedAndNotReplayedLazily(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"cargo"}},
		Tools: map[string]*config.Tool{
			"helper": {
				Name:           "helper",
				DependencyOnly: true,
				MethodOnly:     []string{"cargo"},
				Methods: []*config.MethodCandidate{{
					Kind:    "cargo",
					Config:  map[string]any{"pkg": "helper"},
					Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}},
				}},
			},
			"owner": {
				Name:       "owner",
				MethodOnly: []string{"cargo"},
				Methods: []*config.MethodCandidate{{
					Kind: "cargo", Config: map[string]any{"pkg": "owner"}, Requires: []string{"helper"},
				}},
			},
		},
	}

	first := New()
	WithRunner(&sequenceRunner{results: []run.Result{{}, {}}})(first)
	WithAdapters(&testMockAdapter{
		kindValue: "cargo",
		checkFunc: func(string) bool { return false },
		installFunc: func(name string) error {
			if name == "helper" {
				return errors.New("helper installer interrupted")
			}
			return nil
		},
	})(first)
	WithSchemaInfo("/test/schema.toml", time.Now())(first)
	firstReport, err := first.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(firstReport.Tools) != 2 || firstReport.Failed != 2 {
		t.Fatalf("first report = %+v, want failed helper and owner", firstReport.Tools)
	}

	helperChecks := 0
	helperInstalls := 0
	ownerInstalls := 0
	secondRunner := &sequenceRunner{}
	second := New()
	WithRunner(secondRunner)(second)
	WithAdapters(&testMockAdapter{
		kindValue: "cargo",
		checkFunc: func(name string) bool {
			if name == "helper" {
				helperChecks++
				// Recovery gets one authoritative satisfied observation. Any
				// later probe would be transiently false and must not cause replay.
				return helperChecks == 1
			}
			return false
		},
		installFunc: func(name string) error {
			switch name {
			case "helper":
				helperInstalls++
			case "owner":
				ownerInstalls++
			}
			return nil
		},
	})(second)
	WithSchemaInfo("/test/schema.toml", time.Now())(second)

	report, err := second.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if helperChecks != 1 {
		t.Fatalf("helper checks = %d, want recovery reconciliation only", helperChecks)
	}
	if helperInstalls != 0 {
		t.Fatalf("helper installs = %d, recovered dependency must not replay", helperInstalls)
	}
	if ownerInstalls != 1 {
		t.Fatalf("owner installs = %d, want one", ownerInstalls)
	}
	if len(secondRunner.calls) != 0 {
		t.Fatalf("recovered source preparation replayed host calls: %#v", secondRunner.calls)
	}

	var helper, owner *ToolResult
	for i := range report.Tools {
		switch report.Tools[i].Tool {
		case "helper":
			helper = &report.Tools[i]
		case "owner":
			owner = &report.Tools[i]
		}
	}
	if helper == nil || helper.Status != StatusInstalled || !helper.InstallCommitted {
		t.Fatalf("helper result = %#v, want terminal recovered install", helper)
	}
	if owner == nil || owner.Status != StatusInstalled {
		t.Fatalf("owner result = %#v, want installed", owner)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Tools["helper"]; !ok {
		t.Fatalf("recovered dependency-only tool missing from state: %#v", st.Tools)
	}
	if _, ok := st.Tools["owner"]; !ok {
		t.Fatalf("owner missing from state: %#v", st.Tools)
	}
}
func TestExecutorRollsBackDurableSourceWhenPostPrepareAvailabilityFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{
		{},
		{},
		{Stdout: []byte("vendor/tools\n")},
		{},
	}}
	primary := &availabilityMockAdapter{
		testMockAdapter:    testMockAdapter{kindValue: "cargo", checkFunc: func(string) bool { return false }},
		checkAvailableFunc: func(string) bool { return false },
	}
	fallback := &testMockAdapter{
		kindValue:   "http",
		checkFunc:   func(string) bool { return false },
		installFunc: func(string) error { return nil },
	}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"cargo", "http"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name: "demo",
				Methods: []*config.MethodCandidate{
					{Kind: "cargo", Config: map[string]any{"pkg": "demo"}, Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}}},
					{Kind: "http", Config: map[string]any{"url": "https://demo"}},
				},
			},
		},
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(primary, fallback)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || report.Tools[0].MethodKind != "http" {
		t.Fatalf("report=%+v, want fallback http install", report.Tools)
	}
	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 0 || len(st.PreparationJournals) != 0 {
		t.Fatalf("rolled-back unavailable candidate left active preparation state: plans=%#v journals=%#v", st.PreparationPlans, st.PreparationJournals)
	}
	if len(st.OwnedResources) != 0 {
		t.Fatalf("rolled-back unavailable candidate retained source ownership: %#v", st.OwnedResources)
	}
	if len(runner.calls) != 4 || runner.calls[3].Name != "brew" || len(runner.calls[3].Args) != 2 || runner.calls[3].Args[0] != "untap" {
		t.Fatalf("source calls=%v, want durable add followed by compensating remove", runner.calls)
	}
}

// cancelSignallingAdapter wraps blockingMockAdapter and closes entered
// when Install starts, so the test cancels exactly mid-install instead of
// guessing with a sleep.
type cancelSignallingAdapter struct {
	blockingMockAdapter
	entered chan struct{}
}

func (m *cancelSignallingAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, _ *plan.ResolvedInstallPlan) error {
	close(m.entered)
	return m.blockingMockAdapter.Install(ctx, nil, tool, mc)
}

func (m *cancelSignallingAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	close(m.entered)
	return m.blockingMockAdapter.Install(ctx, rn, tool, mc)
}

// Cancelling mid-install (SIGINT/SIGTERM under the P0 lifecycle) must
// leave the same recoverable committing journal as any other interrupted
// install: the child tree is SIGTERMed, the commit stays unresolved, and
// the next run reconciles it through PreparationRecovery instead of
// replaying the mutation blindly.
func TestExecutorCancellationLeavesRecoverableJournal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &sequenceRunner{results: []run.Result{{}, {}}}
	adapter := &cancelSignallingAdapter{
		blockingMockAdapter: blockingMockAdapter{kindValue: "cargo", block: make(chan struct{})},
		entered:             make(chan struct{}),
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("/test/schema.toml", time.Now())(ex)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		report *ExecReport
		err    error
	}
	outCh := make(chan outcome, 1)
	go func() {
		report, err := ex.Execute(ctx, sourceBackedSchema(), "")
		outCh <- outcome{report, err}
	}()

	select {
	case <-adapter.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("Execute did not reach Install")
	}
	cancel()

	var out outcome
	select {
	case out = <-outCh:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled Execute did not return")
	}
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(out.report.Tools) != 1 || out.report.Tools[0].Status != StatusFailed {
		t.Fatalf("report = %+v, want failed", out.report.Tools)
	}
	if !strings.Contains(out.report.Tools[0].Error, "unresolved") {
		t.Fatalf("error = %q, want unresolved commit", out.report.Tools[0].Error)
	}

	st, err := state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 1 || len(st.PreparationJournals) != 1 {
		t.Fatalf("cancelled transaction = plans=%#v journals=%#v, want exactly one", st.PreparationPlans, st.PreparationJournals)
	}
	for key, journal := range st.PreparationJournals {
		if journal.Status != plan.PreparationCommitting {
			t.Fatalf("journal %q status = %q, want %q", key, journal.Status, plan.PreparationCommitting)
		}
	}

	// The next run must reconcile the cancelled commit through
	// PreparationRecovery: with the target now present, it finalizes
	// without replaying the install.
	second := New()
	WithRunner(&sequenceRunner{})(second)
	WithAdapters(&testMockAdapter{
		kindValue: "cargo",
		checkFunc: func(string) bool { return true },
	})(second)
	WithSchemaInfo("/test/schema.toml", time.Now())(second)
	report, err := second.Execute(context.Background(), sourceBackedSchema(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != StatusInstalled || !report.Tools[0].InstallCommitted {
		t.Fatalf("report = %+v, want terminal recovered install", report.Tools)
	}
	st, err = state.LoadFrom(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PreparationPlans) != 0 || len(st.PreparationJournals) != 0 {
		t.Fatalf("reconciled cancelled commit remained active: plans=%#v journals=%#v", st.PreparationPlans, st.PreparationJournals)
	}
}
