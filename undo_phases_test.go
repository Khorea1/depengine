package main

import (
	"errors"
	"sort"
	"testing"

	"github.com/Khorea1/depengine/internal/state"
)

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
	snap := &state.State{Tools: map[string]state.ToolState{
		"keep": {Method: "go", MethodKind: "go"},
	}}
	mergeUndoTools(cur, snap, []string{"extra"}, map[string]bool{"extra": true}, nil, false)
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
	snap := &state.State{Tools: map[string]state.ToolState{"keep": original["keep"]}}
	mergeUndoTools(cur, snap, []string{"ok", "bad"}, map[string]bool{"ok": true}, original, true)

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
	defer ls.Close()
	curState := ls.State()
	snapState := &state.State{
		Version:          state.CurrentVersion,
		SchemaPath:       "schema.toml",
		SchemaModifiedAt: "2026-01-01",
		Tools:            map[string]state.ToolState{"keep": {Method: "go", MethodKind: "go"}},
	}

	if err := finalizeUndo(ls, curState, snapState, []string{"extra"}, nil, map[string]bool{"extra": true}, false); err != nil {
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
	defer ls.Close()
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

	err = finalizeUndo(ls, curState, snapState, []string{"bad"}, original, map[string]bool{}, true)
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
