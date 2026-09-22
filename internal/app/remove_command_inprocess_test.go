package app

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/state"
)

// TestRunRemoveInvokesAdapterRemover exercises the command orchestration in
// process: state load, target selection, adapter lookup, Remove, and state
// persistence. The adapter is deliberately inert, so no host package manager
// or child depengine process is involved.
func TestRunRemoveInvokesAdapterRemover(t *testing.T) {
	adapter := &recordingRemoveAdapter{}
	exec.Replace(adapter)

	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"tool": {Method: "test-remove", MethodKind: "test-remove", Config: map[string]any{"package": "example/tool"}},
	})

	all, dryRun, force := false, false, false
	schema, only := "", "tool"
	if err := runRemove(context.Background(), nil, &all, &dryRun, &schema, &only, &force); err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if !adapter.called {
		t.Fatal("runRemove did not invoke Adapter.Remove")
	}
	if adapter.gotTool == nil || adapter.gotTool.Name != "tool" {
		t.Fatalf("Remove tool = %#v, want tool", adapter.gotTool)
	}
	if adapter.gotMethod == nil || adapter.gotMethod.Config["package"] != "example/tool" {
		t.Fatalf("Remove method = %#v, want persisted method config", adapter.gotMethod)
	}
	if _, ok := loadTestState(t, stateHome).Tools["tool"]; ok {
		t.Fatal("successful removal remained in persisted state")
	}
}

func TestRunRemoveAdapterFailureRetainsState(t *testing.T) {
	adapter := &recordingRemoveAdapter{err: errors.New("remove failed")}
	exec.Replace(adapter)

	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"tool": {Method: "test-remove", MethodKind: "test-remove"},
	})

	all, dryRun, force := false, false, false
	schema, only := "", "tool"
	if code := requireExitCode(t, runRemove(context.Background(), nil, &all, &dryRun, &schema, &only, &force)); code != 1 {
		t.Fatalf("runRemove exit code = %d, want 1", code)
	}
	if !adapter.called {
		t.Fatal("runRemove did not invoke Adapter.Remove")
	}
	if _, ok := loadTestState(t, stateHome).Tools["tool"]; !ok {
		t.Fatal("failed removal must retain persisted state")
	}
}

func TestRunRemoveDryRunDoesNotInvokeAdapterRemover(t *testing.T) {
	adapter := &recordingRemoveAdapter{}
	exec.Replace(adapter)

	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"tool": {Method: "test-remove", MethodKind: "test-remove"},
	})

	all, dryRun, force := false, true, false
	schema, only := "", "tool"
	if err := runRemove(context.Background(), nil, &all, &dryRun, &schema, &only, &force); err != nil {
		t.Fatalf("runRemove dry-run: %v", err)
	}
	if adapter.called {
		t.Fatal("dry-run invoked Adapter.Remove")
	}
	if _, ok := loadTestState(t, stateHome).Tools["tool"]; !ok {
		t.Fatal("dry-run changed persisted state")
	}
}
