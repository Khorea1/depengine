package exec

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestMethodForResolvedTargetPreservesSelectedNamespace(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		config map[string]any
		target *plan.EnvironmentTarget
		want   map[string]any
	}{
		{"conda named", "conda", map[string]any{"pkg": "numpy", "prefix": "/old"}, &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}, map[string]any{"pkg": "numpy", "environment": "tools"}},
		{"conda prefix", "conda", map[string]any{"pkg": "numpy", "environment": "old"}, &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: "/envs/tools"}, map[string]any{"pkg": "numpy", "prefix": "/envs/tools"}},
		{"conda base", "conda", map[string]any{"pkg": "numpy", "environment": "old"}, nil, map[string]any{"pkg": "numpy"}},
		{"cargo root", "cargo", map[string]any{"pkg": "ripgrep", "root": "/old"}, &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: "/tools"}, map[string]any{"pkg": "ripgrep", "root": "/tools"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := &config.MethodCandidate{Kind: tt.kind, Config: tt.config}
			original := make(map[string]any, len(tt.config))
			for key, value := range tt.config {
				original[key] = value
			}
			resolved := plan.New("demo", tt.kind, true)
			resolved.Identity.Environment = tt.target
			got := methodForResolvedTarget(method, &resolved)
			if !reflect.DeepEqual(got.Config, tt.want) {
				t.Fatalf("projected config = %#v, want %#v", got.Config, tt.want)
			}
			if !reflect.DeepEqual(method.Config, original) {
				t.Fatalf("source config changed: %#v", method.Config)
			}
		})
	}
}

type targetCaptureAdapter struct {
	executorAdapterV2Double
	observedTarget string
	versionTarget  string
}

func (a *targetCaptureAdapter) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	// Simulate a candidate modified after the planner captured its identity.
	method.Config["environment"] = "other"
	resolved := intent.Clone()
	return &resolved, nil
}

func (a *targetCaptureAdapter) Observe(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (plan.Observation, error) {
	a.observedTarget, _ = method.Config["environment"].(string)
	return plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "numpy", Environment: &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldEnvironment}}, nil
}

func (a *targetCaptureAdapter) InstalledVersion(_ context.Context, _ run.Runner, _ *config.Tool, method *config.MethodCandidate) (string, error) {
	a.versionTarget, _ = method.Config["environment"].(string)
	return "1.0", nil
}

func TestExecutorUsesResolvedEnvironmentForObservationAndStateProbe(t *testing.T) {
	adapter := &targetCaptureAdapter{}
	adapter.kindValue = "conda"
	ex := New()
	WithRunner(&run.FakeRunner{})(ex)
	WithAdapters(adapter)(ex)
	method := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "numpy", "environment": "tools"}}
	tool := &config.Tool{Name: "numpy", MethodOnly: []string{"conda"}, Methods: []*config.MethodCandidate{method}}
	result := ToolResult{Tool: tool.Name}
	ex.tryMethods(context.Background(), tool, &result, time.Now())
	if result.Status != StatusAlready || adapter.observedTarget != "tools" {
		t.Fatalf("status = %v, observed target = %q", result.Status, adapter.observedTarget)
	}
	if got := result.Config["environment"]; got != "tools" {
		t.Fatalf("state target = %v, want tools", got)
	}
	if got := ex.installedVersion(context.Background(), tool, result); got != "1.0" || adapter.versionTarget != "tools" {
		t.Fatalf("version = %q, version target = %q", got, adapter.versionTarget)
	}
}
