package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type replacementRecoveryAdapter struct {
	executorAdapterV2Double
	installed    map[string]string
	observations int
	order        []string
	removed      []string
}

func (*replacementRecoveryAdapter) Kind() string { return "npm" }

func (a *replacementRecoveryAdapter) Observe(_ context.Context, _ run.Runner, tool *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	a.observations++
	version, present := a.installed[tool.Name]
	if !present {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	pkg, _ := method.Config["pkg"].(string)
	return plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: pkg, Version: version},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}, nil
}

func (a *replacementRecoveryAdapter) InstallResolved(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate, desired *plan.ResolvedInstallPlan) error {
	if a.installed == nil {
		a.installed = make(map[string]string)
	}
	a.installed[tool.Name] = desired.Identity.Version
	a.order = append(a.order, tool.Name)
	return nil
}

func (a *replacementRecoveryAdapter) Remove(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) error {
	a.removed = append(a.removed, tool.Name)
	delete(a.installed, tool.Name)
	return nil
}
func (*replacementRecoveryAdapter) CanRemove() bool { return true }

func TestExactReplacementMethodRequiresPersistedIdentity(t *testing.T) {
	cases := []struct {
		name      string
		methods   []*config.MethodCandidate
		identity  plan.CandidateIdentity
		label     string
		wantLabel string
		wantError bool
	}{
		{name: "unique legacy kind", methods: []*config.MethodCandidate{{Kind: "npm"}}, identity: plan.CandidateIdentity{Method: "npm", Explicit: true}},
		{name: "ambiguous legacy kind", methods: []*config.MethodCandidate{{Kind: "npm", Label: "one"}, {Kind: "npm", Label: "two"}}, identity: plan.CandidateIdentity{Method: "npm", Explicit: true}, wantError: true},
		{name: "exact label", methods: []*config.MethodCandidate{{Kind: "npm", Label: "one"}, {Kind: "npm", Label: "two"}}, identity: plan.CandidateIdentity{Method: "npm", Explicit: true}, label: "two", wantLabel: "two"},
		{name: "missing exact label", methods: []*config.MethodCandidate{{Kind: "npm", Label: "one"}}, identity: plan.CandidateIdentity{Method: "npm", Explicit: true}, label: "missing", wantError: true},
		{name: "explicitness changed", methods: []*config.MethodCandidate{{Kind: "npm", Inferred: true}}, identity: plan.CandidateIdentity{Method: "npm", Explicit: true}, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method, err := exactReplacementMethod(&config.Tool{Name: "demo", Methods: tc.methods}, tc.identity, tc.label)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected identity mismatch or ambiguity")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if method == nil || method.Label != tc.wantLabel {
				t.Fatalf("method = %#v, want label %q", method, tc.wantLabel)
			}
		})
	}
}

func TestExecutorRecoversReplacementTransactionsInSortedOrder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	adapter := &replacementRecoveryAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}}, installed: make(map[string]string)}
	methods := make(map[string]*config.Tool)
	for _, name := range []string{"zeta", "alpha"} {
		method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": name, "version": "2.0.0"}}
		methods[name] = &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	}

	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	for name := range methods {
		previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": name, "version": "1.0.0"}}
		locked.State().Tools[name] = previous
		resolved, err := candidatePlanIntentErr(methods[name], methods[name].Methods[0])
		if err != nil || resolved == nil {
			_ = locked.Close()
			t.Fatalf("candidatePlanIntentErr(%q) = %v, %v", name, resolved, err)
		}
		resolved.Identity.Version = "2.0.0"
		desired, err := plan.ProjectLock(*resolved)
		if err != nil {
			t.Fatal(err)
		}
		candidate := state.ReplacementCandidate{Identity: desired.Candidate}
		if err := locked.BeginReplacement(name, "npm", candidate, candidate, previous, desired, "", nil); err != nil {
			t.Fatal(err)
		}
		if err := locked.PlanReplacementRemoval(name); err != nil {
			t.Fatal(err)
		}
		if err := locked.RecordReplacementRemoved(name); err != nil {
			t.Fatal(err)
		}
		if err := locked.PlanReplacementInstall(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	schema := &config.Schema{Tools: methods}
	rc := ex.newRunContext(context.Background(), schema, "")
	if err := ex.recoverAndRecord(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(adapter.order, want) {
		t.Fatalf("recovery install order = %v, want %v", adapter.order, want)
	}
	if len(rc.recoveredCommits) != 2 {
		t.Fatalf("recovered commits = %d, want 2", len(rc.recoveredCommits))
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "zeta"} {
		if got := persisted.Tools[name].Version; got != "2.0.0" {
			t.Errorf("persisted %s version = %q, want locked 2.0.0", name, got)
		}
		if _, ok := persisted.ReplacementTransactions[name]; ok {
			t.Errorf("replacement WAL for %s remained", name)
		}
	}
}
func TestExecuteResolvedCandidateReplacesTrackedVersionAtomically(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "tool"
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": name, "version": "2.0.0"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{name: tool}}
	adapter := &replacementRecoveryAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
		installed:               map[string]string{name: "1.0.0"},
	}
	previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", RootRequested: true, Config: map[string]any{"pkg": name, "version": "1.0.0"}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = previous
	if err := locked.Save(); err != nil {
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = name
	desired.Identity.Version = "2.0.0"
	ex := New()
	runner := &replacementBoundaryRunner{t: t}
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, method, &desired, previous)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInstalled {
		t.Fatalf("status = %v, want installed: %+v", result.Status, result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %v, want no hooks for replacement without configured hooks", runner.calls)
	}
	if !reflect.DeepEqual(adapter.removed, []string{name}) || !reflect.DeepEqual(adapter.order, []string{name}) {
		t.Fatalf("remove/install order = %v/%v, want tool/tool", adapter.removed, adapter.order)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.Tools[name]; got.Version != "2.0.0" || !got.RootRequested {
		t.Fatalf("persisted state = %+v, want version 2.0.0 with root intent", got)
	}
	if len(persisted.ReplacementTransactions) != 0 || len(persisted.PreparationJournals) != 0 {
		t.Fatalf("replacement/preparation WALs remain: %d/%d", len(persisted.ReplacementTransactions), len(persisted.PreparationJournals))
	}
}

func TestReplacementFindsPersistedLabelEqualToKind(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "tool"
	oldMethod := &config.MethodCandidate{Kind: "npm", Label: "npm", Config: map[string]any{"pkg": name, "version": "1.0.0"}}
	newMethod := &config.MethodCandidate{Kind: "npm", Label: "next", Config: map[string]any{"pkg": name, "version": "2.0.0"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{oldMethod, newMethod}}
	schema := &config.Schema{Tools: map[string]*config.Tool{name: tool}}
	adapter := &replacementRecoveryAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}}, installed: map[string]string{name: "1.0.0"}}
	previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", RootRequested: true, Config: map[string]any{"pkg": name, "version": "1.0.0"}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = previous
	if err := locked.Save(); err != nil {
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = name
	desired.Identity.Version = "2.0.0"
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, newMethod, &desired, previous)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInstalled {
		t.Fatalf("status = %v, want installed: %+v", result.Status, result)
	}
	if !reflect.DeepEqual(adapter.removed, []string{name}) || !reflect.DeepEqual(adapter.order, []string{name}) {
		t.Fatalf("remove/install order = %v/%v, want tool/tool", adapter.removed, adapter.order)
	}
}

func TestReplacementRecoveryUsesPersistedLabelForDuplicateKindCandidates(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	methods := []*config.MethodCandidate{
		{Kind: "npm", Label: "mirror-a", Config: map[string]any{"pkg": "package-a"}},
		{Kind: "npm", Label: "mirror-b", Config: map[string]any{"pkg": "package-b"}},
	}
	tool := &config.Tool{Name: name, Methods: methods}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = "package-b"
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "mirror-b", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-b"}}
	persistReplacementAtInstalling(t, name, previous, projection, "mirror-b", "mirror-b", nil, nil)

	adapter := &replacementRecoveryMutableResolverAdapter{
		replacementRecoveryAdapter: &replacementRecoveryAdapter{
			executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
			installed:               map[string]string{name: "2.0.0"},
		},
		mutableVersion: "9.9.9",
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	if err := ex.recoverAndRecord(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if len(adapter.order) != 0 || len(adapter.removed) != 0 {
		t.Fatalf("already-satisfied labeled replacement mutated adapter: install=%v remove=%v", adapter.order, adapter.removed)
	}
	recovered, ok := rc.recoveredCommits[name]
	if !ok {
		t.Fatal("recovery did not commit the exact labeled candidate")
	}
	if recovered.intent == nil || recovered.intent.Identity.Package != "package-b" || recovered.intent.Identity.Version != "2.0.0" {
		t.Fatalf("recovered desired identity = %+v, want persisted package-b@2.0.0", recovered.intent)
	}
	if adapter.resolveCall != 0 {
		t.Fatalf("mutable ResolvePlan() calls = %d, want 0", adapter.resolveCall)
	}
	if adapter.observations == 0 {
		t.Fatal("compatible persisted candidate was rejected before recovery observation")
	}
}

func TestReplacementRecoveryRejectsLegacyWALWithoutResourceIdentity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	method := &config.MethodCandidate{Kind: "npm", Sources: []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}}, Config: map[string]any{"pkg": "package-demo"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = "package-demo"
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-demo"}}
	persistReplacementAtInstalling(t, name, previous, projection, "", "", nil, nil)
	adapter := &replacementRecoveryAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}}, installed: map[string]string{name: "2.0.0"}}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	if err := ex.recoverAndRecord(context.Background(), rc); err == nil || !strings.Contains(err.Error(), "lacks resource ownership identity") {
		t.Fatalf("recovery error = %v, want missing legacy resource identity", err)
	}
	if len(adapter.order) != 0 || len(adapter.removed) != 0 {
		t.Fatalf("legacy resource failure mutated adapter: install=%v remove=%v", adapter.order, adapter.removed)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if tx, ok := persisted.ReplacementTransactions[name]; !ok || tx.Journal.Phase != plan.ReplacementInstalling {
		t.Fatalf("replacement evidence = %+v, want installing WAL retained", tx)
	}
}

func TestReplacementRecoveryClaimsPersistedResourcesWithoutPreparationWAL(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "package-demo"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = "package-demo"
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(desired)
	if err != nil {
		t.Fatal(err)
	}
	uses := []plan.ResourceUse{
		{Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}},
		{Resource: plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "compiler:go"}},
	}
	wantOwned, err := plan.ClaimResourceUses(nil, name, uses)
	if err != nil {
		t.Fatal(err)
	}
	wantOwned, err = plan.ClaimResourceUses(wantOwned, "other-tool", uses)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-demo"}}
	persistReplacementAtInstalling(t, name, previous, projection, "", "", uses, wantOwned)

	adapter := &replacementRecoveryAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
		installed:               map[string]string{name: "2.0.0"},
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	if err := ex.recoverAndRecord(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted.OwnedResources, wantOwned) {
		t.Fatalf("recovered resource ownership = %+v, want %+v", persisted.OwnedResources, wantOwned)
	}
	if len(adapter.order) != 0 {
		t.Fatalf("already-satisfied replacement reinstalled target: %v", adapter.order)
	}
}

func TestReplacementRecoveryRejectsCurrentIntentDriftBeforeMutation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	method := &config.MethodCandidate{Kind: "npm", Label: "stable", Config: map[string]any{"pkg": "package-b"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = "package-a"
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "stable", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-a"}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = previous
	candidate := state.ReplacementCandidate{Identity: projection.Candidate, Label: "stable"}
	if err := locked.BeginReplacement(name, projection.Candidate.Method, candidate, candidate, previous, projection, "", nil); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.PlanReplacementRemoval(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &replacementRecoveryAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
		installed:               map[string]string{name: "1.0.0"},
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	err = ex.recoverAndRecord(context.Background(), rc)
	if !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("recoverAndRecord() error = %v, want ErrLockMismatch", err)
	}
	if adapter.observations != 0 || len(adapter.removed) != 0 || len(adapter.order) != 0 {
		t.Fatalf("recovery observed or mutated host before rejecting drift: observes=%d remove=%v install=%v", adapter.observations, adapter.removed, adapter.order)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := persisted.ReplacementTransactions[name]; !ok || got.Journal.Phase != plan.ReplacementRemoving {
		t.Fatalf("replacement WAL = %+v, want retained Removing transaction", got)
	}
	if got, ok := persisted.Tools[name]; !ok || !reflect.DeepEqual(got, previous) {
		t.Fatalf("tracked old tool state = %+v, want unchanged %+v", got, previous)
	}
}

func TestReplacementRecoveryRejectsRequestedVersionIntentDriftBeforeMutation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	method := &config.MethodCandidate{Kind: "npm", Label: "stable", Config: map[string]any{"pkg": "package-a", "version": "2.0.0"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	desired, err := candidatePlanIntentErr(tool, method)
	if err != nil || desired == nil {
		t.Fatalf("candidatePlanIntentErr() = %v, %v", desired, err)
	}
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(*desired)
	if err != nil {
		t.Fatal(err)
	}
	method.Config["version"] = "3.0.0"
	previous := state.ToolState{Method: "stable", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-a", "version": "1.0.0"}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = previous
	candidate := state.ReplacementCandidate{Identity: projection.Candidate, Label: "stable"}
	if err := locked.BeginReplacement(name, projection.Candidate.Method, candidate, candidate, previous, projection, "", nil); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.PlanReplacementRemoval(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &replacementRecoveryAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
		installed:               map[string]string{name: "1.0.0"},
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	err = ex.recoverAndRecord(context.Background(), rc)
	if !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("recoverAndRecord() error = %v, want ErrLockMismatch", err)
	}
	if adapter.observations != 0 || len(adapter.removed) != 0 || len(adapter.order) != 0 {
		t.Fatalf("recovery observed or mutated host before rejecting version drift: observes=%d remove=%v install=%v", adapter.observations, adapter.removed, adapter.order)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := persisted.ReplacementTransactions[name]; !ok || got.Journal.Phase != plan.ReplacementRemoving {
		t.Fatalf("replacement WAL = %+v, want retained Removing transaction", got)
	}
	if got, ok := persisted.Tools[name]; !ok || !reflect.DeepEqual(got, previous) {
		t.Fatalf("tracked old tool state = %+v, want unchanged %+v", got, previous)
	}
}

func persistReplacementAtInstalling(
	t *testing.T,
	name string,
	previous state.ToolState,
	desired plan.LockProjection,
	previousLabel, candidateLabel string,
	uses []plan.ResourceUse,
	owned []plan.OwnedResourceState,
) {
	t.Helper()
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = previous
	locked.State().OwnedResources = append([]plan.OwnedResourceState(nil), owned...)
	candidate := state.ReplacementCandidate{Identity: desired.Candidate, Label: candidateLabel}
	prior := state.ReplacementCandidate{Identity: desired.Candidate, Label: previousLabel}
	if err := locked.BeginReplacement(name, desired.Candidate.Method, prior, candidate, previous, desired, "", uses); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.PlanReplacementRemoval(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.RecordReplacementRemoved(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.PlanReplacementInstall(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
}

type replacementBoundaryRunner struct {
	t        *testing.T
	exitCode int
	calls    []string
	phases   []plan.ReplacementPhase
}

func (r *replacementBoundaryRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	persisted, err := state.Load()
	if err != nil {
		r.t.Errorf("load state at hook boundary: %v", err)
		return run.Result{ExitCode: r.exitCode}
	}
	if tx, ok := persisted.ReplacementTransactions["demo"]; ok {
		r.phases = append(r.phases, tx.Journal.Phase)
	} else {
		r.phases = append(r.phases, "")
	}
	return run.Result{ExitCode: r.exitCode}
}

func TestResolvedReplacementHookLifecycleAndFailureState(t *testing.T) {
	cases := []struct {
		name         string
		timing       string
		exitCode     int
		wantStatus   StatusEnum
		wantRemove   int
		wantInstall  int
		wantPhase    plan.ReplacementPhase
		wantVersion  string
		wantPostDone bool
	}{
		{"before-success", "before", 0, StatusInstalled, 1, 1, "", "2.0.0", false},
		{"before-failure", "before", 1, StatusFailed, 0, 0, "", "1.0.0", true},
		{"after-success", "after", 0, StatusInstalled, 1, 1, plan.ReplacementPostHookRunning, "2.0.0", true},
		{"after-failure", "after", 1, StatusFailed, 1, 1, plan.ReplacementPostHookRunning, "2.0.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "package-demo", "version": "2.0.0"}}
			hook := config.Hook{Run: []string{"lifecycle-hook", tc.name}}
			tool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{method}}
			if tc.timing == "before" {
				tool.PreInstall = []config.Hook{hook}
			} else {
				tool.PostInstall = []config.Hook{hook}
			}
			schema := &config.Schema{Tools: map[string]*config.Tool{"demo": tool}}
			previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", PostinstallDone: true, RootRequested: true, Config: map[string]any{"pkg": "package-demo", "version": "1.0.0"}}
			locked, err := state.LoadLocked()
			if err != nil {
				t.Fatal(err)
			}
			locked.State().Tools["demo"] = previous
			if err := locked.Save(); err != nil {
				_ = locked.Close()
				t.Fatal(err)
			}
			if err := locked.Close(); err != nil {
				t.Fatal(err)
			}
			resolved := plan.New("demo", "npm", true)
			resolved.Identity.Package = "package-demo"
			resolved.Identity.Version = "2.0.0"
			timing := plan.HookBefore
			if tc.timing == "after" {
				timing = plan.HookAfter
			}
			resolved.Hooks = []plan.LifecycleHook{{ID: "lifecycle", Transition: plan.TransitionUpgrade, Timing: timing, Operation: plan.Operation{Kind: "hook", Effect: plan.EffectMutation, Command: []string{"lifecycle-hook", tc.name}, ArbitraryCode: true}, FailurePolicy: plan.HookFailAbort}}
			adapter := &replacementRecoveryAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}}, installed: map[string]string{"demo": "1.0.0"}}
			runner := &replacementBoundaryRunner{t: t, exitCode: tc.exitCode}
			ex := New()
			WithRunner(runner)(ex)
			WithAdapters(adapter)(ex)
			WithSchemaInfo("schema.yaml", time.Time{})(ex)
			WithAllowArbitraryCode()(ex)
			result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, method, &resolved, previous)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.wantStatus {
				t.Fatalf("status = %v, want %v: %+v", result.Status, tc.wantStatus, result)
			}
			if len(adapter.removed) != tc.wantRemove || len(adapter.order) != tc.wantInstall {
				t.Fatalf("adapter calls remove/install = %d/%d, want %d/%d", len(adapter.removed), len(adapter.order), tc.wantRemove, tc.wantInstall)
			}
			if len(runner.calls) != 1 || runner.phases[0] != tc.wantPhase {
				t.Fatalf("hook calls/phases = %v/%v, want one call at phase %q", runner.calls, runner.phases, tc.wantPhase)
			}
			persisted, err := state.Load()
			if err != nil {
				t.Fatal(err)
			}
			tracked := persisted.Tools["demo"]
			if tracked.Version != tc.wantVersion || tracked.PostinstallDone != tc.wantPostDone {
				t.Fatalf("tracked state = %+v, want version=%s postinstall_done=%t", tracked, tc.wantVersion, tc.wantPostDone)
			}
			if _, ok := persisted.ReplacementTransactions["demo"]; ok {
				t.Fatal("completed or returned-failure replacement retained its WAL")
			}
		})
	}
}

func TestReplacementRecoveryRunsHookWithoutReinstallAfterVerifiedInstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	method := &config.MethodCandidate{Kind: "npm", PostInstall: []config.Hook{{Run: []string{"recovered-hook"}}}, Config: map[string]any{"pkg": "package-demo"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = "package-demo"
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-demo"}}
	persistReplacementAtInstalling(t, name, previous, projection, "", "", nil, nil)
	adapter := &replacementRecoveryAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}}, installed: map[string]string{name: "2.0.0"}}
	runner := &replacementBoundaryRunner{t: t}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	WithAllowArbitraryCode()(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	if err := ex.recoverAndRecord(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if len(adapter.order) != 0 || len(adapter.removed) != 0 {
		t.Fatalf("recovery duplicated adapter mutation: install=%v remove=%v", adapter.order, adapter.removed)
	}
	if len(runner.calls) != 1 || runner.phases[0] != plan.ReplacementPostHookRunning {
		t.Fatalf("recovered hook calls/phases = %v/%v", runner.calls, runner.phases)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Tools[name].Version != "2.0.0" || !persisted.Tools[name].PostinstallDone {
		t.Fatalf("recovered tool state = %+v", persisted.Tools[name])
	}
	if _, ok := persisted.ReplacementTransactions[name]; ok {
		t.Fatal("successful recovered hook retained replacement WAL")
	}
}

type replacementRecoveryMutableResolverAdapter struct {
	*replacementRecoveryAdapter
	mutableVersion string
}

func (a *replacementRecoveryMutableResolverAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	a.resolveCall++
	resolved := intent.Clone()
	resolved.Identity.Version = a.mutableVersion
	return &resolved, nil
}

func TestReplacementRecoveryRetryRemovalUsesRemovalElevation(t *testing.T) {
	tests := []struct {
		name       string
		startErr   error
		removeErr  error
		wantErr    bool
		wantRemove int
		wantStops  int
		wantWAL    bool
	}{
		{name: "elevated removal succeeds", wantRemove: 1, wantStops: 1},
		{name: "elevation startup fails", startErr: errors.New("elevation unavailable"), wantErr: true, wantWAL: true},
		{name: "remove fails and elevation stops", removeErr: errors.New("remove failed"), wantErr: true, wantRemove: 1, wantStops: 1, wantWAL: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &replacementRecoveryElevationRunner{FakeRunner: &run.FakeRunner{}, startErr: tc.startErr}
			resolver := &githubSecretTestResolver{}
			ex, rc, oldAdapter, previous := replacementRetryRemovalFixture(t, runner, resolver, nil, true)
			oldAdapter.removeErr = tc.removeErr

			err := ex.recoverAndRecord(context.Background(), rc)
			if tc.wantErr && err == nil {
				t.Fatal("recoverAndRecord() succeeded, want removal error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("recoverAndRecord() error = %v", err)
			}
			if oldAdapter.removeCalls != tc.wantRemove {
				t.Fatalf("Remove calls = %d, want %d", oldAdapter.removeCalls, tc.wantRemove)
			}
			if oldAdapter.removeCalls != 0 && (oldAdapter.removedLabel != "old" || oldAdapter.removedURL != "https://example.test/old.git") {
				t.Fatalf("Remove target = %q %q, want old candidate URL", oldAdapter.removedLabel, oldAdapter.removedURL)
			}
			if oldAdapter.removeCalls != 0 && !oldAdapter.removeWhileElevated {
				t.Fatal("Remove did not run while the elevation session was active")
			}
			if runner.starts != 1 || runner.stops != tc.wantStops || runner.active {
				t.Fatalf("elevation lifecycle: starts=%d stops=%d active=%v", runner.starts, runner.stops, runner.active)
			}
			if oldAdapter.removeCalls != 0 && (oldAdapter.remainingTimeout <= time.Minute || oldAdapter.remainingTimeout > 2*time.Minute) {
				t.Fatalf("removal context deadline remaining = %s, want a two-minute timeout", oldAdapter.remainingTimeout)
			}

			persisted, loadErr := state.Load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			tx, retained := persisted.ReplacementTransactions["demo"]
			if retained != tc.wantWAL {
				t.Fatalf("replacement WAL retained = %v, want %v", retained, tc.wantWAL)
			}
			if tc.wantWAL {
				if tx.Journal.Phase != plan.ReplacementRemoving {
					t.Fatalf("replacement phase = %q, want Removing", tx.Journal.Phase)
				}
				if !reflect.DeepEqual(persisted.Tools["demo"], previous) {
					t.Fatalf("tracked previous state changed after blocked removal: %+v", persisted.Tools["demo"])
				}
			}
		})
	}
}

func TestReplacementRecoveryRetryRemovalUsesMethodScopedCredentials(t *testing.T) {
	t.Run("resolved credential remains ephemeral", func(t *testing.T) {
		resolver := &githubSecretTestResolver{results: []githubSecretResult{{value: gitTestSentinel}}}
		ex, rc, oldAdapter, _ := replacementRetryRemovalFixture(
			t, &run.FakeRunner{}, resolver,
			&config.SecretReference{Provider: "env", Name: gitTestRefName}, false,
		)
		if err := ex.recoverAndRecord(context.Background(), rc); err != nil {
			t.Fatal(err)
		}
		if resolver.calls != 1 || len(resolver.refs) != 1 || resolver.refs[0].Name != gitTestRefName {
			t.Fatalf("secret resolution = calls:%d refs:%+v", resolver.calls, resolver.refs)
		}
		if oldAdapter.removeCalls != 1 || !oldAdapter.hasCredential || oldAdapter.credential != gitTestSentinel {
			t.Fatalf("Remove credential = %q, %v; calls=%d", oldAdapter.credential, oldAdapter.hasCredential, oldAdapter.removeCalls)
		}
		if oldAdapter.remainingTimeout <= time.Minute || oldAdapter.remainingTimeout > 2*time.Minute {
			t.Fatalf("removal context deadline remaining = %s, want a two-minute timeout", oldAdapter.remainingTimeout)
		}
		if oldAdapter.installCalls != 1 {
			t.Fatalf("desired install calls = %d, want recovery to continue after removal", oldAdapter.installCalls)
		}
		persisted, err := state.Load()
		if err != nil {
			t.Fatal(err)
		}
		assertReplacementStateOmitsSecret(t, persisted, gitTestSentinel)
	})

	t.Run("resolution failure blocks removal and retains WAL", func(t *testing.T) {
		resolver := &githubSecretTestResolver{results: []githubSecretResult{{err: errors.New("private resolver detail")}}}
		ex, rc, oldAdapter, previous := replacementRetryRemovalFixture(
			t, &run.FakeRunner{}, resolver,
			&config.SecretReference{Provider: "env", Name: gitTestRefName}, false,
		)
		err := ex.recoverAndRecord(context.Background(), rc)
		if err == nil || strings.Contains(err.Error(), "private resolver detail") || strings.Contains(err.Error(), gitTestSentinel) {
			t.Fatalf("credential resolution error = %v, want sanitized failure", err)
		}
		if resolver.calls != 1 || oldAdapter.removeCalls != 0 || oldAdapter.installCalls != 0 {
			t.Fatalf("blocked recovery mutated: resolves=%d removes=%d installs=%d", resolver.calls, oldAdapter.removeCalls, oldAdapter.installCalls)
		}
		persisted, loadErr := state.Load()
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		tx, ok := persisted.ReplacementTransactions["demo"]
		if !ok || tx.Journal.Phase != plan.ReplacementRemoving {
			t.Fatalf("replacement transaction = %+v, want retained Removing WAL", tx)
		}
		if !reflect.DeepEqual(persisted.Tools["demo"], previous) {
			t.Fatalf("tracked previous state changed after credential failure: %+v", persisted.Tools["demo"])
		}
		assertReplacementStateOmitsSecret(t, persisted, gitTestSentinel)
	})
}

func replacementRetryRemovalFixture(
	t *testing.T,
	runner run.Runner,
	resolver *githubSecretTestResolver,
	secretRef *config.SecretReference,
	requiresElevation bool,
) (*Executor, *runContext, *replacementRetryRemovalAdapter, state.ToolState) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	oldMethod := &config.MethodCandidate{Kind: "git", Label: "old", Config: map[string]any{"url": "https://example.test/old.git"}, SecretRef: secretRef}
	desiredMethod := &config.MethodCandidate{Kind: "git", Label: "new", Config: map[string]any{"url": "https://example.test/new.git", "rev": "2222222222222222222222222222222222222222"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{oldMethod, desiredMethod}}
	desired, err := candidatePlanIntentErr(tool, desiredMethod)
	if err != nil {
		t.Fatal(err)
	}
	desired.Identity.Revision = desiredMethod.Config["rev"].(string)
	projection, err := plan.ProjectLock(*desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "old", MethodKind: "git", Version: "1.0.0", Config: map[string]any{"url": "https://example.test/old.git"}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = previous
	prior := state.ReplacementCandidate{Identity: plan.CandidateIdentity{Method: "git", Explicit: true}, Label: "old"}
	candidate := state.ReplacementCandidate{Identity: projection.Candidate, Label: "new"}
	if err := locked.BeginReplacement(name, "git", prior, candidate, previous, projection, "", nil); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.PlanReplacementRemoval(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	oldAdapter := &replacementRetryRemovalAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "git"}},
		oldPresent:              true, requiresElevation: requiresElevation,
	}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(oldAdapter)(ex)
	WithSecretResolver(resolver)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	return ex, rc, oldAdapter, previous
}

type replacementRetryRemovalAdapter struct {
	executorAdapterV2Double
	oldPresent          bool
	desiredPresent      bool
	installCalls        int
	requiresElevation   bool
	removeErr           error
	removeCalls         int
	removedLabel        string
	removedURL          string
	removeWhileElevated bool
	credential          string
	hasCredential       bool
	remainingTimeout    time.Duration
}

func (a *replacementRetryRemovalAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	present, version := a.oldPresent, "1.0.0"
	if method.Label == "new" {
		present, version = a.desiredPresent, "2.0.0"
	}
	if !present {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	if method.Label == "new" {
		url, _ := method.Config["url"].(string)
		return plan.Observation{
			Presence:    plan.PresencePresent,
			Identity:    plan.ObservedIdentity{Package: "demo", Source: url, Revision: "2222222222222222222222222222222222222222"},
			KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldSource, plan.FieldRevision},
		}, nil
	}
	return plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "demo", Version: version},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}, nil
}

func (a *replacementRetryRemovalAdapter) InstallResolved(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, _ *plan.ResolvedInstallPlan) error {
	a.installCalls++
	a.desiredPresent = true
	return nil
}

func (a *replacementRetryRemovalAdapter) Remove(ctx context.Context, runner run.Runner, _ *config.Tool, method *config.MethodCandidate) error {
	a.removeCalls++
	a.removedLabel = method.Label
	a.removedURL, _ = method.Config["url"].(string)
	if session, ok := runner.(*replacementRecoveryElevationRunner); ok {
		a.removeWhileElevated = session.active
	}
	a.credential, a.hasCredential = GitCredential(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		a.remainingTimeout = time.Until(deadline)
	}
	if a.removeErr != nil {
		return a.removeErr
	}
	a.oldPresent = false
	return nil
}

func (*replacementRetryRemovalAdapter) CanRemove() bool { return true }

func (a *replacementRetryRemovalAdapter) RequiresRemovalElevation(*config.Tool, *config.MethodCandidate) bool {
	return a.requiresElevation
}

type replacementRecoveryElevationRunner struct {
	*run.FakeRunner
	active   bool
	starts   int
	stops    int
	startErr error
}

func (r *replacementRecoveryElevationRunner) StartElevationSession(context.Context) (func(), error) {
	r.starts++
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.active = true
	return func() {
		r.active = false
		r.stops++
	}, nil
}

func assertReplacementStateOmitsSecret(t *testing.T, persisted *state.State, secret string) {
	t.Helper()
	encoded, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatal("persisted replacement state contains the resolved credential")
	}
}

func TestReplacementRecoveryBlocksRunningHookWithoutReplay(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "demo"
	method := &config.MethodCandidate{Kind: "npm", PostInstall: []config.Hook{{Run: []string{"recovered-hook"}}}, Config: map[string]any{"pkg": "package-demo"}}
	tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = "package-demo"
	desired.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(desired)
	if err != nil {
		t.Fatal(err)
	}
	previous := state.ToolState{Method: "npm", MethodKind: "npm", Version: "1.0.0", Config: map[string]any{"pkg": "package-demo"}}
	persistReplacementAtInstalling(t, name, previous, projection, "", "", nil, nil)
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	installed := state.ToolState{Method: "npm", MethodKind: "npm", Version: "2.0.0", Config: map[string]any{"pkg": "package-demo"}}
	if err := locked.CommitReplacementInstallWithPreparation(name, installed, nil, "", plan.PreparationPlan{}, name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.PlanReplacementPostHook(name); err != nil {
		_ = locked.Close()
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
	adapter := &replacementRecoveryAdapter{executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}}, installed: map[string]string{name: "2.0.0"}}
	runner := &replacementBoundaryRunner{t: t}
	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	WithAllowArbitraryCode()(ex)
	rc := ex.newRunContext(context.Background(), &config.Schema{Tools: map[string]*config.Tool{name: tool}}, "")
	if err := ex.recoverAndRecord(context.Background(), rc); err == nil {
		t.Fatal("recovery accepted an ambiguous running hook")
	}
	if len(runner.calls) != 0 || len(adapter.order) != 0 || len(adapter.removed) != 0 {
		t.Fatalf("ambiguous hook recovery replayed work: hooks=%v install=%v remove=%v", runner.calls, adapter.order, adapter.removed)
	}
	persisted, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if tx := persisted.ReplacementTransactions[name]; tx.Journal.Phase != plan.ReplacementPostHookRunning {
		t.Fatalf("running hook WAL = %+v", tx)
	}
}
