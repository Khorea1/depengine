package exec

import (
	"context"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestExecutorUsesV2LockIdentityInsteadOfLegacyPins(t *testing.T) {
	locked := plan.New("demo", "native", true)
	locked.Identity.Package = "demo"
	locked.Identity.Version = "2.0.0"
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{locked})
	if err != nil {
		t.Fatal(err)
	}

	adapter := &executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "native"},
		presence:        plan.PresenceAbsent,
	}
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	WithLockDocument(document)(ex)
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{"native"},
		Methods: []*config.MethodCandidate{{
			Kind:   "native",
			Config: map[string]any{"pkg": "demo"},
		}},
	}
	result := ToolResult{Tool: tool.Name}
	ctx := context.Background()
	rc := ex.newRunContext(ctx, nil, "")
	ex.tryMethods(ctx, rc, tool, &result, time.Now())

	if adapter.installed == nil {
		t.Fatalf("InstallResolved was not called: %+v", result)
	}
	if got := adapter.installed.Identity.Version; got != "2.0.0" {
		t.Fatalf("installed version = %q, want locked v2 version 2.0.0", got)
	}
}
