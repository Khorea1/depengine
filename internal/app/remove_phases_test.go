package app

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/state"
)

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
	remover, _, removable := resolveRemover("tool", state.ToolState{Method: "no-such-method", MethodKind: "no-such-method"})
	if removable || remover != nil {
		t.Errorf("expected manual-remove path, got removable=%v remover=%v", removable, remover)
	}
}

func TestResolveRemoverMethodFallback(t *testing.T) {
	// Empty MethodKind falls back to Method for explicitly constructed state.
	remover, kind, removable := resolveRemover("tool", state.ToolState{Method: "go"})
	if !removable || remover == nil || kind != "go" {
		t.Errorf("expected removable go remover, got removable=%v kind=%q remover=%v", removable, kind, remover)
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
