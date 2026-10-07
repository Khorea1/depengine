package exec

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type expectedSnapshotMutationAdapter struct {
	*replacementRecoveryAdapter
	expected *state.ToolState
	mutated  bool
}

func (a *expectedSnapshotMutationAdapter) InstallResolved(ctx context.Context, runner run.Runner, tool *config.Tool, method *config.MethodCandidate, desired *plan.ResolvedInstallPlan) error {
	if tool.Name == "dependency" && !a.mutated {
		a.expected.Config["nested"].(map[string]any)["marker"] = "mutated during prerequisite"
		a.mutated = true
	}
	return a.replacementRecoveryAdapter.InstallResolved(ctx, runner, tool, method, desired)
}

func TestResolvedUpgradeSnapshotsExpectedStateBeforePrerequisiteWork(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const name = "tool"
	expected := state.ToolState{
		Method: "npm", MethodKind: "npm", Version: "1.0.0", RootRequested: true,
		Config: map[string]any{"pkg": name, "version": "1.0.0", "nested": map[string]any{"marker": "discovered"}},
	}
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": name, "version": "2.0.0"}}
	dependencyMethod := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "dependency", "version": "1.0.0"}}
	tool := &config.Tool{Name: name, Requires: []string{"dependency"}, Methods: []*config.MethodCandidate{method}}
	dependency := &config.Tool{Name: "dependency", Methods: []*config.MethodCandidate{dependencyMethod}}
	schema := &config.Schema{Tools: map[string]*config.Tool{name: tool, dependency.Name: dependency}}
	locked, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	locked.State().Tools[name] = expected
	if err := locked.Save(); err != nil {
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	base := &replacementRecoveryAdapter{
		executorAdapterV2Double: executorAdapterV2Double{testMockAdapter: testMockAdapter{kindValue: "npm"}},
		installed:               map[string]string{name: "1.0.0"},
	}
	adapter := &expectedSnapshotMutationAdapter{replacementRecoveryAdapter: base, expected: &expected}
	desired := plan.New(name, "npm", true)
	desired.Identity.Package = name
	desired.Identity.Version = "2.0.0"
	ex := New()
	var logOutput bytes.Buffer
	WithLogger(slog.New(slog.NewJSONHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelDebug})))(ex)
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithSchemaInfo("schema.yaml", time.Time{})(ex)
	result, err := ex.ExecuteResolvedUpgradeCandidate(context.Background(), schema, "", tool, method, &desired, expected)
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.mutated {
		t.Fatal("prerequisite did not mutate the caller-owned expected config")
	}
	if result.Status != StatusInstalled {
		t.Fatalf("status = %v, want installed after preserving the discovery snapshot: %+v", result.Status, result)
	}
	if !strings.Contains(logOutput.String(), `"success":2`) {
		t.Fatalf("completion report did not count installed result: %s", logOutput.String())
	}
	if len(adapter.removed) != 1 || adapter.removed[0] != name {
		t.Fatalf("removed tools = %v, want only %q", adapter.removed, name)
	}
}
