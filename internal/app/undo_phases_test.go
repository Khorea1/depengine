package app

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type undoPlanAdapter struct {
	kind       string
	presence   plan.PresenceState
	observeErr error
	removeErr  error
	canRemove  bool
	removed    bool
	removePkg  string
	// observedVersion overrides the version echoed back from the requested
	// config so tests can simulate a host that drifted from tracked state.
	observedVersion string
}

func (a *undoPlanAdapter) Kind() string                             { return a.kind }
func (*undoPlanAdapter) Available(context.Context, run.Runner) bool { return true }
func (a *undoPlanAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}
func (a *undoPlanAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	if a.observeErr != nil {
		return plan.Observation{}, a.observeErr
	}
	presence := a.presence
	if presence == "" {
		presence = plan.PresencePresent
	}
	pkg, _ := method.Config["pkg"].(string)
	version, _ := method.Config["version"].(string)
	observed := plan.ObservedIdentity{Package: pkg, Version: version}
	if a.observedVersion != "" {
		observed.Version = a.observedVersion
	}
	known := []plan.IdentityField{plan.FieldPackage}
	if version != "" {
		known = append(known, plan.FieldVersion)
	}
	return plan.Observation{Presence: presence, Identity: observed, KnownFields: known}, nil
}
func (*undoPlanAdapter) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	return nil
}
func (a *undoPlanAdapter) Remove(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) error {
	a.removed = true
	a.removePkg, _ = method.Config["pkg"].(string)
	return a.removeErr
}
func (a *undoPlanAdapter) CanRemove() bool { return a.canRemove }
func (*undoPlanAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}
func (*undoPlanAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
	return nil
}

func undoPlanExecutor(adapter exec.AdapterV2) *exec.Executor {
	ex := exec.New()
	exec.WithAdapters(adapter)(ex)
	exec.WithRunner(&run.FakeRunner{})(ex)
	return ex
}

func TestRemoveUndoToolsUsesResolvedTargetAndReleasesAbsentState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		presence  plan.PresenceState
		wantCall  bool
		canRemove bool
	}{
		{name: "satisfied removes resolved target", wantCall: true, canRemove: true},
		{name: "absent releases tracking without remover", presence: plan.PresenceAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &undoPlanAdapter{kind: "cargo", presence: tc.presence, canRemove: tc.canRemove}
			current := &state.State{Tools: map[string]state.ToolState{
				"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "tool", "version": "1.0.0"}},
			}}
			original, succeeded, failed := removeUndoTools(context.Background(), []string{"tool"}, current, undoPlanExecutor(adapter), "")
			if failed || !succeeded["tool"] {
				t.Fatalf("removeUndoTools failed=%t succeeded=%v", failed, succeeded)
			}
			if adapter.removed != tc.wantCall {
				t.Fatalf("Remove called=%t, want %t", adapter.removed, tc.wantCall)
			}
			if tc.wantCall && adapter.removePkg != "tool" {
				t.Fatalf("removed package = %q, want resolved package tool", adapter.removePkg)
			}
			if _, ok := original["tool"]; !ok {
				t.Fatal("original state snapshot did not retain tool")
			}
		})
	}
}

func TestRemoveUndoToolsPreservesTrackingOnUnverifiableTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		presence   plan.PresenceState
		observeErr error
	}{
		{name: "unknown", presence: plan.PresenceUnknown},
		{name: "broken", presence: plan.PresenceBroken},
		{name: "probe error", observeErr: errors.New("probe failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &undoPlanAdapter{kind: "cargo", presence: tc.presence, observeErr: tc.observeErr}
			current := &state.State{Tools: map[string]state.ToolState{
				"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "tool"}},
			}}
			_, succeeded, failed := removeUndoTools(context.Background(), []string{"tool"}, current, undoPlanExecutor(adapter), "")
			if !failed || succeeded["tool"] {
				t.Fatalf("removeUndoTools failed=%t succeeded=%v, want failure", failed, succeeded)
			}
			if adapter.removed {
				t.Fatal("Remove called for unverifiable target")
			}
		})
	}
}

func TestRemoveUndoToolsPreservesTrackingOnDriftedTarget(t *testing.T) {
	adapter := &undoPlanAdapter{kind: "cargo", canRemove: true, observedVersion: "1.0.0"}
	current := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "tool", "version": "2.0.0"}},
	}}
	_, succeeded, failed := removeUndoTools(context.Background(), []string{"tool"}, current, undoPlanExecutor(adapter), "")
	if !failed || succeeded["tool"] {
		t.Fatalf("removeUndoTools failed=%t succeeded=%v, want failure", failed, succeeded)
	}
	if adapter.removed {
		t.Fatal("Remove called for a version-drifted target")
	}
	mergeUndoTools(current, []string{"tool"}, succeeded)
	if _, ok := current.Tools["tool"]; !ok {
		t.Fatal("drifted target lost its tracked state")
	}
}

func TestRemoveUndoToolsPreservesPartialFailureAfterResolvedRemovalError(t *testing.T) {
	adapter := &undoPlanAdapter{kind: "cargo", canRemove: true, removeErr: errors.New("remove failed")}
	current := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "tool"}},
	}}
	_, succeeded, failed := removeUndoTools(context.Background(), []string{"tool"}, current, undoPlanExecutor(adapter), "")
	if !failed || succeeded["tool"] || !adapter.removed {
		t.Fatalf("failed=%t succeeded=%v removed=%t", failed, succeeded, adapter.removed)
	}
	mergeUndoTools(current, []string{"tool"}, succeeded)
	if _, ok := current.Tools["tool"]; !ok {
		t.Fatal("failed removal lost tracked tool")
	}
}

// exitCodeOf unwraps an ExitError to its code, or -1 when err is nil or of
// another type.
func exitCodeOf(err error) int {
	if err == nil {
		return -1
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return -2
}

func undoTestSnapshots() []state.SnapshotInfo {
	return []state.SnapshotInfo{
		{Path: "/snapshots/new.json", ToolCount: 2},
		{Path: "/snapshots/old.json", ToolCount: 1},
	}
}

func TestSelectUndoSnapshotPathIndex(t *testing.T) {
	snaps := undoTestSnapshots()
	for spec, want := range map[string]string{"1": "/snapshots/new.json", "2": "/snapshots/old.json"} {
		got, err := selectUndoSnapshotPath(spec, snaps)
		if err != nil {
			t.Fatalf("selectUndoSnapshotPath(%q): unexpected error: %v", spec, err)
		}
		if got != want {
			t.Errorf("selectUndoSnapshotPath(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestSelectUndoSnapshotPathIndexOutOfRange(t *testing.T) {
	snaps := undoTestSnapshots()
	for _, spec := range []string{"0", "3", "99"} {
		_, err := selectUndoSnapshotPath(spec, snaps)
		if code := exitCodeOf(err); code != 2 {
			t.Errorf("selectUndoSnapshotPath(%q) exit = %d, want 2 (err=%v)", spec, code, err)
		}
	}
	if _, err := selectUndoSnapshotPath("1", nil); exitCodeOf(err) != 2 {
		t.Errorf("selectUndoSnapshotPath on empty set exit = %d, want 2 (err=%v)", exitCodeOf(err), err)
	}
}

func TestSelectUndoSnapshotPathPassthrough(t *testing.T) {
	got, err := selectUndoSnapshotPath("/custom/snap.json", undoTestSnapshots())
	if err != nil {
		t.Fatalf("selectUndoSnapshotPath(path): unexpected error: %v", err)
	}
	if got != "/custom/snap.json" {
		t.Errorf("selectUndoSnapshotPath(path) = %q, want passthrough", got)
	}
}

func TestDiffUndoTools(t *testing.T) {
	mk := func(names ...string) map[string]state.ToolState {
		m := make(map[string]state.ToolState, len(names))
		for _, n := range names {
			m[n] = state.ToolState{Method: "go", MethodKind: "go"}
		}
		return m
	}

	got := diffUndoTools(mk("a", "b", "c"), mk("b"))
	sort.Strings(got)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("diffUndoTools added = %v, want [a c]", got)
	}

	// Identical sets: nothing to undo.
	if got := diffUndoTools(mk("a", "b"), mk("a", "b")); len(got) != 0 {
		t.Errorf("diffUndoTools identical = %v, want empty", got)
	}

	// Tools deliberately removed after the snapshot are not resurrected.
	if got := diffUndoTools(mk("a"), mk("a", "b")); len(got) != 0 {
		t.Errorf("diffUndoTools removed-after-snapshot = %v, want empty", got)
	}
}

func TestResolveUndoMethodKind(t *testing.T) {
	ts := state.ToolState{Method: "go", MethodKind: "golang"}
	if got := resolveUndoMethodKind(ts); got != "golang" {
		t.Errorf("resolveUndoMethodKind = %q, want MethodKind %q", got, "golang")
	}
	legacy := state.ToolState{Method: "cargo"}
	if got := resolveUndoMethodKind(legacy); got != "cargo" {
		t.Errorf("resolveUndoMethodKind legacy = %q, want Method fallback %q", got, "cargo")
	}
}

func TestMergeUndoToolsSuccess(t *testing.T) {
	cur := &state.State{Tools: map[string]state.ToolState{
		"keep":  {Method: "go", MethodKind: "go"},
		"extra": {Method: "go", MethodKind: "go"},
	}}
	mergeUndoTools(cur, []string{"extra"}, map[string]bool{"extra": true})
	if len(cur.Tools) != 1 {
		t.Fatalf("mergeUndoTools success tools = %v, want only [keep]", cur.Tools)
	}
	if _, ok := cur.Tools["keep"]; !ok {
		t.Errorf("mergeUndoTools success dropped snapshot tool; tools = %v", cur.Tools)
	}
}

func TestMergeUndoToolsPartialFailure(t *testing.T) {
	original := map[string]state.ToolState{
		"keep": {Method: "go", MethodKind: "go"},
		"ok":   {Method: "go", MethodKind: "go"},
		"bad":  {Method: "bogus", MethodKind: "bogus"},
	}
	cur := &state.State{Tools: map[string]state.ToolState{
		"keep": original["keep"],
		"ok":   original["ok"],
		"bad":  original["bad"],
	}}
	mergeUndoTools(cur, []string{"ok", "bad"}, map[string]bool{"ok": true})

	if _, ok := cur.Tools["keep"]; !ok {
		t.Errorf("mergeUndoTools partial dropped snapshot tool; tools = %v", cur.Tools)
	}
	if _, ok := cur.Tools["ok"]; ok {
		t.Errorf("mergeUndoTools partial kept removed tool ok; tools = %v", cur.Tools)
	}
	bad, ok := cur.Tools["bad"]
	if !ok {
		t.Fatalf("mergeUndoTools partial dropped failed tool bad; tools = %v", cur.Tools)
	}
	if bad.Method != "bogus" {
		t.Errorf("mergeUndoTools partial bad.Method = %q, want original %q", bad.Method, "bogus")
	}
}

func TestFinalizeUndoSuccess(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"keep":  {Method: "go", MethodKind: "go"},
		"extra": {Method: "go", MethodKind: "go"},
	})

	ls, err := state.LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked: %v", err)
	}
	defer func() { _ = ls.Close() }()
	curState := ls.State()
	snapState := &state.State{
		Version:          state.CurrentVersion,
		SchemaPath:       "schema.toml",
		SchemaModifiedAt: "2026-01-01",
		Tools:            map[string]state.ToolState{"keep": {Method: "go", MethodKind: "go"}},
	}

	if err := finalizeUndo(ls, curState, snapState, []string{"extra"}, map[string]bool{"extra": true}, false); err != nil {
		t.Fatalf("finalizeUndo success: unexpected error: %v", err)
	}
	st := loadTestState(t, stateHome)
	if len(st.Tools) != 1 || st.Tools["keep"].Method != "go" {
		t.Errorf("finalizeUndo success persisted tools = %v, want only [keep]", st.Tools)
	}
	if st.SchemaPath != "schema.toml" {
		t.Errorf("finalizeUndo success SchemaPath = %q, want snapshot value", st.SchemaPath)
	}
}

func TestFinalizeUndoReleasesRemovedToolClaims(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	resource, err := plan.PrerequisiteResource("helper")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFullState(t, stateHome, state.State{
		Version: state.CurrentVersion,
		Tools: map[string]state.ToolState{
			"helper": {Method: "go", MethodKind: "go"},
			"extra":  {Method: "go", MethodKind: "go"},
		},
		OwnedResources: []plan.OwnedResourceState{{
			Resource: resource, Ownership: plan.OwnershipDepengine, Dependents: []string{"extra"},
		}},
	})

	ls, err := state.LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked: %v", err)
	}
	defer func() { _ = ls.Close() }()
	curState := ls.State()
	snapState := &state.State{
		Version: state.CurrentVersion,
		Tools:   map[string]state.ToolState{"helper": {Method: "go", MethodKind: "go"}},
	}
	if err := finalizeUndo(ls, curState, snapState, []string{"extra"}, map[string]bool{"extra": true}, false); err != nil {
		t.Fatalf("finalizeUndo: %v", err)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["extra"]; ok {
		t.Fatal("removed tool remains tracked after undo")
	}
	if len(st.OwnedResources) != 1 || st.OwnedResources[0].RefCount() != 0 {
		t.Fatalf("owned resources after undo = %#v, want retained zero-ref prerequisite", st.OwnedResources)
	}
}

func TestFinalizeUndoPartialFailure(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"keep": {Method: "go", MethodKind: "go"},
		"bad":  {Method: "bogus", MethodKind: "bogus"},
	})

	ls, err := state.LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked: %v", err)
	}
	defer func() { _ = ls.Close() }()
	curState := ls.State()
	original := map[string]state.ToolState{
		"keep": curState.Tools["keep"],
		"bad":  curState.Tools["bad"],
	}
	snapState := &state.State{
		Version:    state.CurrentVersion,
		SchemaPath: "schema.toml",
		Tools:      map[string]state.ToolState{"keep": original["keep"]},
	}

	err = finalizeUndo(ls, curState, snapState, []string{"bad"}, map[string]bool{}, true)
	if code := exitCodeOf(err); code != 1 {
		t.Fatalf("finalizeUndo partial exit = %d, want 1 (err=%v)", code, err)
	}
	st := loadTestState(t, stateHome)
	if _, ok := st.Tools["keep"]; !ok {
		t.Errorf("finalizeUndo partial dropped snapshot tool; tools = %v", st.Tools)
	}
	if bad, ok := st.Tools["bad"]; !ok || bad.Method != "bogus" {
		t.Errorf("finalizeUndo partial did not preserve failed tool; tools = %v", st.Tools)
	}
}

// An undo is a rollback of additions, not a request to resurrect tools that
// the user deliberately removed after the snapshot was taken.
func TestMergeUndoToolsDoesNotResurrectRemovedEntries(t *testing.T) {
	current := &state.State{Tools: map[string]state.ToolState{
		"existing": {Method: "go"},
		"new":      {Method: "cargo"},
	}}
	mergeUndoTools(current, []string{"new"}, map[string]bool{"new": true})
	if _, exists := current.Tools["removed-since-snapshot"]; exists {
		t.Fatal("undo resurrected a tool that was removed after the snapshot")
	}
	if _, exists := current.Tools["new"]; exists {
		t.Fatal("undo retained an added tool successfully removed")
	}
	if _, exists := current.Tools["existing"]; !exists {
		t.Fatal("undo dropped a tool present in both states")
	}
}

func TestValidateUndoSnapshotRejectsCrossSchemaAndSharedDrift(t *testing.T) {
	old := state.ToolState{Method: "native", MethodKind: "native", Version: "1.0.0", Config: map[string]any{"pkg": "rust"}}
	changed := state.ToolState{Method: "native", MethodKind: "native", Version: "2.0.0", Config: map[string]any{"pkg": "rust"}}
	tests := []struct {
		name     string
		snapshot *state.State
		current  *state.State
		want     string
	}{
		{"same schema and identity", &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": old}}, &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": old, "new": {Method: "cargo"}}}, ""},
		{"different schema", &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": old}}, &state.State{SchemaPath: "/b/schema.toml", Tools: map[string]state.ToolState{"rust": old, "new": {Method: "cargo"}}}, "snapshot schema"},
		{"changed existing identity", &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": old}}, &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": changed, "new": {Method: "cargo"}}}, "changed since snapshot"},
		{"missing schema provenance", &state.State{Tools: map[string]state.ToolState{"rust": old}}, &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": old, "new": {Method: "cargo"}}}, "no schema identity"},
		{"empty first-install snapshot", &state.State{Tools: map[string]state.ToolState{}}, &state.State{SchemaPath: "/a/schema.toml", Tools: map[string]state.ToolState{"rust": old}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUndoSnapshot(tc.current, tc.snapshot)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunUndoRejectsExternalSchemaBeforeRemovalAndKeepsState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := state.ToolState{Method: "native", MethodKind: "native", Config: map[string]any{"pkg": "rust"}}
	initial := &state.State{Version: state.CurrentVersion, SchemaPath: "/project-a/schema.toml", Tools: map[string]state.ToolState{"rust": old}}
	if err := state.Save(initial); err != nil {
		t.Fatalf("seed initial state: %v", err)
	}
	snapshot, err := state.SaveSnapshot()
	if err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	current := &state.State{Version: state.CurrentVersion, SchemaPath: "/project-b/schema.toml", Tools: map[string]state.ToolState{
		"rust":   old,
		"rustup": {Method: "cargo", MethodKind: "cargo", Config: map[string]any{"pkg": "rustup"}},
	}}
	if err := state.Save(current); err != nil {
		t.Fatalf("seed other project: %v", err)
	}
	before, err := state.Load()
	if err != nil {
		t.Fatalf("read state before undo: %v", err)
	}
	list, jsonOut := false, false
	specific := snapshot.Path
	err = runUndo(context.Background(), &list, &specific, &jsonOut)
	if exitCodeOf(err) != 3 {
		t.Fatalf("unsafe cross-schema undo returned %v (exit %d), want exit 3", err, exitCodeOf(err))
	}
	after, err := state.Load()
	if err != nil {
		t.Fatalf("read state after undo: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("undo modified global state on cross-schema refusal: before=%+v after=%+v", before, after)
	}
}
