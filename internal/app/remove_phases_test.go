package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type recordingRemoveAdapter struct {
	run.Runner
	err       error
	presence  plan.PresenceState
	packageID string
	called    bool
	gotTool   *config.Tool
	gotMethod *config.MethodCandidate
}

func (a *recordingRemoveAdapter) Kind() string { return "cargo" }

func (a *recordingRemoveAdapter) Available(context.Context, run.Runner) bool { return true }

func (a *recordingRemoveAdapter) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return false
}

func (a *recordingRemoveAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func (a *recordingRemoveAdapter) Remove(_ context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate) error {
	a.called = true
	a.Runner = rn
	a.gotTool = tool
	a.gotMethod = method
	return a.err
}

func (a *recordingRemoveAdapter) CanRemove() bool { return true }

func (a *recordingRemoveAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *recordingRemoveAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	pkg, _ := method.Config["pkg"].(string)
	if a.packageID != "" {
		pkg = a.packageID
	}
	presence := a.presence
	if presence == "" {
		presence = plan.PresencePresent
	}
	return plan.Observation{Presence: presence, Identity: plan.ObservedIdentity{Package: pkg}, KnownFields: []plan.IdentityField{plan.FieldPackage}}, nil
}

func TestRemoveAbsentTargetReleasesTrackingWithoutCallingRemover(t *testing.T) {
	adapter := &recordingRemoveAdapter{presence: plan.PresenceAbsent}
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "example/tool"}},
	}}
	sess := testRemoveSession(st, "tool")
	exec.WithAdapters(adapter)(sess.executor)

	if !sess.removeTrackedTool(context.Background(), "tool", false) {
		t.Fatal("removeTrackedTool returned false for proven-absent target")
	}
	if adapter.called {
		t.Fatal("Remove called for proven-absent target")
	}
	if _, exists := st.Tools["tool"]; exists {
		t.Fatal("tracking retained for proven-absent target")
	}
}

func TestRemoveDriftedTargetStillInvokesRemover(t *testing.T) {
	adapter := &recordingRemoveAdapter{presence: plan.PresencePresent, packageID: "another-package"}
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "example/tool"}},
	}}
	sess := testRemoveSession(st, "tool")
	exec.WithAdapters(adapter)(sess.executor)

	if !sess.removeTrackedTool(context.Background(), "tool", false) {
		t.Fatal("removeTrackedTool returned false for present drifted target")
	}
	if !adapter.called {
		t.Fatal("Remove was not called for a present drifted target")
	}
	if _, exists := st.Tools["tool"]; exists {
		t.Fatal("tracking retained after drifted target was removed")
	}
}

func TestRemoveUnknownOrBrokenTargetPreservesTracking(t *testing.T) {
	for _, presence := range []plan.PresenceState{plan.PresenceUnknown, plan.PresenceBroken} {
		t.Run(string(presence), func(t *testing.T) {
			adapter := &recordingRemoveAdapter{presence: presence}
			st := &state.State{Tools: map[string]state.ToolState{
				"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "example/tool"}},
			}}
			sess := testRemoveSession(st, "tool")
			exec.WithAdapters(adapter)(sess.executor)

			if sess.removeTrackedTool(context.Background(), "tool", false) {
				t.Fatal("removeTrackedTool succeeded without a trustworthy observation")
			}
			if adapter.called {
				t.Fatal("Remove called without a trustworthy observation")
			}
			if _, exists := st.Tools["tool"]; !exists {
				t.Fatal("tracking was deleted despite an unverifiable target")
			}
		})
	}
}

func TestRemoveDryRunFailsForUnknownTargetWithoutMutation(t *testing.T) {
	adapter := &recordingRemoveAdapter{presence: plan.PresenceUnknown}
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "example/tool"}},
	}}
	sess := testRemoveSession(st, "tool")
	sess.dryRun = true
	exec.WithAdapters(adapter)(sess.executor)

	if sess.planDryRunRemoval("tool", st.Tools["tool"]) {
		t.Fatal("dry-run claimed a removal plan for an unknown target")
	}
	if adapter.called {
		t.Fatal("dry-run invoked Remove")
	}
	if _, exists := st.Tools["tool"]; !exists {
		t.Fatal("dry-run changed state")
	}
}

func (a *recordingRemoveAdapter) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	return nil
}

func (a *recordingRemoveAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

func (a *recordingRemoveAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*recordingRemoveAdapter)(nil)

// requireExitCode unwraps an ExitError to its code, failing the test when
// err carries none. (Named to avoid colliding with undo_phases_test.go's
// non-fatal exitCodeOf helper in the same package.)
func requireExitCode(t *testing.T, err error) int {
	t.Helper()
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	return ee.Code
}

func testRemoveSession(st *state.State, requested ...string) *removeSession {
	req := make(map[string]bool)
	for _, name := range requested {
		req[name] = true
	}
	return &removeSession{
		ctx:              context.Background(),
		runner:           &run.FakeRunner{},
		executor:         exec.New(),
		state:            st,
		requestedRemoval: req,
		removedThisRun:   make(map[string]bool),
	}
}

func ownedPrerequisite(t *testing.T, helper string, dependents ...string) plan.OwnedResourceState {
	t.Helper()
	resource, err := plan.PrerequisiteResource(helper)
	if err != nil {
		t.Fatal(err)
	}
	return plan.OwnedResourceState{
		Resource:   resource,
		Ownership:  plan.OwnershipDepengine,
		Dependents: dependents,
	}
}

func TestValidateRemoveFlagsConflict(t *testing.T) {
	all, only := true, "tool"
	if err := validateRemoveFlags(&all, &only); requireExitCode(t, err) != 2 {
		t.Fatalf("expected exit 2, got %v", err)
	}
}

func TestValidateRemoveFlagsOK(t *testing.T) {
	no, yes, empty, one := false, true, "", "tool"
	for _, tc := range []struct {
		name string
		all  *bool
		only *string
	}{
		{"neither", &no, &empty},
		{"all", &yes, &empty},
		{"only", &no, &one},
	} {
		if err := validateRemoveFlags(tc.all, tc.only); err != nil {
			t.Errorf("%s: expected nil, got %v", tc.name, err)
		}
	}
}

func TestConfirmRemoveAllAborts(t *testing.T) {
	all, force := true, false
	proceed, err := confirmRemoveAllWith(&all, &force, true, strings.NewReader("n\n"))
	if err != nil {
		t.Fatalf("abort should not return an error: %v", err)
	}
	if proceed {
		t.Fatal("abort should not proceed")
	}
}

func TestConfirmRemoveAllForceSkipsPrompt(t *testing.T) {
	all, force := true, true
	proceed, err := confirmRemoveAllWith(&all, &force, false, strings.NewReader(""))
	if err != nil || !proceed {
		t.Fatalf("force should proceed without prompting: proceed=%v err=%v", proceed, err)
	}
}

func TestCollectRemovalTargets(t *testing.T) {
	st := &state.State{Tools: map[string]state.ToolState{
		"a": {}, "b": {},
	}}
	all, no := true, false
	only, empty := "b", ""

	if got := collectRemovalTargets(st, &all, &empty, nil); len(got) != 2 || !got["a"] || !got["b"] {
		t.Errorf("--all: got %v", got)
	}
	if got := collectRemovalTargets(st, &no, &only, []string{"a"}); len(got) != 1 || !got["b"] {
		t.Errorf("--only: got %v", got)
	}
	if got := collectRemovalTargets(st, &no, &empty, []string{"a"}); len(got) != 1 || !got["a"] {
		t.Errorf("args: got %v", got)
	}
	if got := collectRemovalTargets(st, &no, &empty, nil); len(got) != 0 {
		t.Errorf("default: got %v", got)
	}
}

func TestResolveRemoverUnknownMethod(t *testing.T) {
	remover, _, removable := testRemoveSession(&state.State{}).resolveRemover("tool", state.ToolState{Method: "no-such-method", MethodKind: "no-such-method"})
	if removable || remover != nil {
		t.Errorf("expected manual-remove path, got removable=%v remover=%v", removable, remover)
	}
}

func TestResolveRemoverMethodFallback(t *testing.T) {
	// Empty MethodKind falls back to Method for explicitly constructed state.
	remover, kind, removable := testRemoveSession(&state.State{}).resolveRemover("tool", state.ToolState{Method: "go"})
	if !removable || remover == nil || kind != "go" {
		t.Errorf("expected removable go remover, got removable=%v kind=%q remover=%v", removable, kind, remover)
	}
}

func TestInvokeRemoverUsesInjectedRunnerAndPropagatesFailure(t *testing.T) {
	runner := &run.FakeRunner{}
	wantErr := errors.New("remove failed")
	adapter := &recordingRemoveAdapter{err: wantErr}
	sess := testRemoveSession(&state.State{})
	sess.runner = runner
	toolState := state.ToolState{Method: "cargo", Config: map[string]any{"pkg": "example/tool"}}

	if sess.invokeRemover(context.Background(), "tool", toolState, adapter, "cargo", false) {
		t.Fatal("failed adapter removal should return false")
	}
	if !adapter.called {
		t.Fatal("Remove was not called through the Remover interface")
	}
	if adapter.Runner != runner {
		t.Fatal("Remove received a different runner than the injected runner")
	}
	if adapter.gotTool == nil || adapter.gotTool.Name != "tool" {
		t.Fatalf("Remove received tool %#v, want tool name %q", adapter.gotTool, "tool")
	}
	if adapter.gotMethod == nil || adapter.gotMethod.Kind != "cargo" || adapter.gotMethod.Config["pkg"] != "example/tool" {
		t.Fatalf("Remove received method %#v", adapter.gotMethod)
	}
	if sess.removedThisRun["tool"] {
		t.Fatal("failed removal must not be marked successful")
	}
}

func TestPrerequisiteBlockers(t *testing.T) {
	newState := func() *state.State {
		return &state.State{
			Tools:          map[string]state.ToolState{"helper": {}, "owner": {}},
			OwnedResources: []plan.OwnedResourceState{ownedPrerequisite(t, "helper", "owner")},
		}
	}

	sess := testRemoveSession(newState(), "helper")
	blockers, err := sess.prerequisiteBlockers("helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0] != "owner" {
		t.Errorf("expected [owner] blocker, got %v", blockers)
	}

	sess = testRemoveSession(newState(), "helper", "owner")
	blockers, err = sess.prerequisiteBlockers("helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 0 {
		t.Errorf("expected no blockers when dependent is also scheduled, got %v", blockers)
	}

	sess = testRemoveSession(newState(), "helper")
	blockers, err = sess.prerequisiteBlockers("plain")
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 0 {
		t.Errorf("expected no blockers for non-helper tool, got %v", blockers)
	}
}

func TestFinalizeRemovedPrerequisiteNoop(t *testing.T) {
	st := &state.State{Tools: map[string]state.ToolState{"tool": {}}}
	sess := testRemoveSession(st, "tool")
	if err := sess.finalizeRemovedPrerequisite("tool"); err != nil {
		t.Errorf("expected nil for untracked prerequisite, got %v", err)
	}
}

func TestFindOwnedResourceMiss(t *testing.T) {
	sess := testRemoveSession(&state.State{})
	resource, err := plan.PrerequisiteResource("ghost")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sess.findOwnedResource(resource); ok {
		t.Error("expected miss for untracked resource")
	}
}

func TestPlanDryRunRemovalManualMethod(t *testing.T) {
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "no-such-method", MethodKind: "no-such-method"},
	}}
	sess := testRemoveSession(st, "tool")
	sess.dryRun = true
	if sess.planDryRunRemoval("tool", st.Tools["tool"]) {
		t.Error("expected false for manual-remove-required tool")
	}
}

func TestPlanDryRunRemovalGoTool(t *testing.T) {
	// Dry-run planning never invokes the adapter: resolver lookup plus
	// ownership planning only, so no binary is spawned.
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "go", MethodKind: "go", Config: map[string]any{"pkg": "example.com/tool"}},
	}}
	sess := testRemoveSession(st, "tool")
	sess.dryRun = true
	if !sess.planDryRunRemoval("tool", st.Tools["tool"]) {
		t.Error("expected true for removable tool with no owned resources")
	}
}

func TestRemoveSingleToolUntracked(t *testing.T) {
	sess := testRemoveSession(&state.State{Tools: map[string]state.ToolState{}}, "ghost")
	if sess.removeSingleTool("ghost") {
		t.Error("expected false for untracked tool")
	}
}

func TestRemoveSingleToolBlockedByDependent(t *testing.T) {
	st := &state.State{
		Tools:          map[string]state.ToolState{"helper": {}, "owner": {}},
		OwnedResources: []plan.OwnedResourceState{ownedPrerequisite(t, "helper", "owner")},
	}
	sess := testRemoveSession(st, "helper")
	if sess.removeSingleTool("helper") {
		t.Error("expected false when a live dependent blocks removal")
	}
	if _, ok := st.Tools["helper"]; !ok {
		t.Error("blocked tool must stay tracked")
	}
}

func TestPersistRemoveStateDryRunNilLock(t *testing.T) {
	if err := persistRemoveState(nil, true); err != nil {
		t.Errorf("expected nil for dry-run with nil lock, got %v", err)
	}
}
