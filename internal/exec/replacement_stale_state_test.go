package exec

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

func TestResolvedUpgradeRejectsStaleTrackedStateBeforeReplacement(t *testing.T) {
	const name = "tool"
	baseState := state.ToolState{
		Method: "npm", MethodKind: "npm", Version: "1.0.0", RootRequested: true,
		Config: map[string]any{"pkg": name, "version": "1.0.0", "nested": map[string]any{"key": "original"}},
	}
	changedConfig := cloneExpectedPreviousState(baseState)
	changedConfig.Config["nested"].(map[string]any)["key"] = "changed"

	tests := []struct {
		name       string
		current    state.ToolState
		removeTool bool
	}{
		{name: "version changed", current: func() state.ToolState { s := cloneExpectedPreviousState(baseState); s.Version = "1.1.0"; return s }()},
		{name: "tracked label changed", current: func() state.ToolState { s := cloneExpectedPreviousState(baseState); s.Method = "other-label"; return s }()},
		{name: "tracked config changed", current: changedConfig},
		{name: "tool removed after discovery", current: baseState, removeTool: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": name, "version": "2.0.0"}}
			tool := &config.Tool{Name: name, Methods: []*config.MethodCandidate{method}}
			schema := &config.Schema{Tools: map[string]*config.Tool{name: tool}}
			adapter := &replacementRecoveryAdapter{
				executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
				installed:               map[string]string{name: "1.0.0"},
			}
			locked, err := state.LoadLocked()
			if err != nil {
				t.Fatal(err)
			}
			if !tt.removeTool {
				locked.State().Tools[name] = tt.current
			}
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
			result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, method, &desired, baseState)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusFailed {
				t.Fatalf("status = %v, want failed: %+v", result.Status, result)
			}
			if len(adapter.removed) != 0 || len(adapter.order) != 0 {
				t.Fatalf("remove/install calls = %v/%v, want none", adapter.removed, adapter.order)
			}
			persisted, err := state.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, exists := persisted.ReplacementTransactions[name]; exists {
				t.Fatal("replacement WAL was created for stale discovery state")
			}
			if tt.removeTool {
				if _, exists := persisted.Tools[name]; exists {
					t.Fatal("removed tool state was recreated")
				}
			} else if !reflect.DeepEqual(persisted.Tools[name], tt.current) {
				t.Fatalf("tracked state changed: got %#v, want %#v", persisted.Tools[name], tt.current)
			}
		})
	}
}
