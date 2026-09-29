package exec

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type resolvedRemoveCapture struct {
	executorAdapterV2Double
	tool   *config.Tool
	method *config.MethodCandidate
	runner run.Runner
}

func (a *resolvedRemoveCapture) Remove(_ context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate) error {
	a.runner, a.tool, a.method = rn, tool, method
	return nil
}

func TestRemoveResolvedCandidateUsesProvidedRunnerAndClonedIdentity(t *testing.T) {
	adapter := &resolvedRemoveCapture{executorAdapterV2Double: executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "cargo"},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	runner := &run.FakeRunner{}
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "old"}}
	resolved := plan.New("demo", "cargo", true)
	resolved.Identity.Package = "resolved"

	if err := executor.RemoveResolvedCandidate(context.Background(), runner, tool, method, &resolved); err != nil {
		t.Fatalf("RemoveResolvedCandidate() error = %v", err)
	}
	if adapter.runner != runner || adapter.tool != tool {
		t.Fatal("Remove did not receive the supplied runner and verified tool")
	}
	if adapter.method == method || adapter.method.Config["pkg"] != "resolved" {
		t.Fatalf("Remove method = %#v, want a clone with resolved package", adapter.method)
	}
	if method.Config["pkg"] != "old" {
		t.Fatalf("original method config mutated: %#v", method.Config)
	}
}

type elevationRemoveCapture struct {
	resolvedRemoveCapture
	removedWhileElevated bool
}

func (a *elevationRemoveCapture) RequiresRemovalElevation(*config.Tool, *config.MethodCandidate) bool {
	return true
}

func (a *elevationRemoveCapture) Remove(_ context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate) error {
	runner := rn.(*elevationSessionRunner)
	a.removedWhileElevated = runner.active
	return nil
}

type elevationSessionRunner struct {
	*run.FakeRunner
	active bool
	stops  int
}

func (r *elevationSessionRunner) StartElevationSession(context.Context) (func(), error) {
	r.active = true
	return func() {
		r.active = false
		r.stops++
	}, nil
}

func TestRemoveResolvedCandidateStartsElevationSession(t *testing.T) {
	adapter := &elevationRemoveCapture{resolvedRemoveCapture: resolvedRemoveCapture{executorAdapterV2Double: executorAdapterV2Double{
		testMockAdapter: testMockAdapter{kindValue: "mas"},
	}}}
	executor := New()
	WithAdapters(adapter)(executor)
	runner := &elevationSessionRunner{FakeRunner: &run.FakeRunner{}}
	tool := &config.Tool{Name: "xcode"}
	method := &config.MethodCandidate{Kind: "mas", Config: map[string]any{"pkg": "497799835"}}
	resolved := plan.New("xcode", "mas", true)
	resolved.Identity.Package = "497799835"

	if err := executor.RemoveResolvedCandidate(context.Background(), runner, tool, method, &resolved); err != nil {
		t.Fatalf("RemoveResolvedCandidate() error = %v", err)
	}
	if !adapter.removedWhileElevated || runner.active || runner.stops != 1 {
		t.Fatalf("elevation lifecycle: removedWhileElevated=%v active=%v stops=%d", adapter.removedWhileElevated, runner.active, runner.stops)
	}
}

func TestRemovalMethodForResolvedTargetProjectsIdentityWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		method *config.MethodCandidate
		plan   *plan.ResolvedInstallPlan
		check  func(*testing.T, map[string]any)
	}{
		{
			name: "Scoop package and bucket",
			method: &config.MethodCandidate{Kind: "scoop", Config: map[string]any{
				"pkg": "old", "bucket": "old-bucket", "version": "old-version",
			}},
			plan: func() *plan.ResolvedInstallPlan {
				resolved := plan.New("demo", "scoop", true)
				resolved.Identity.Package = "git"
				resolved.Identity.Version = "2.0"
				resolved.Identity.Scope = string(plan.ScopeSystem)
				resolved.Sources = []plan.SourceReference{{Role: plan.SourceSelection, Name: "main"}}
				return &resolved
			}(),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				for key, want := range map[string]any{"pkg": "git", "bucket": "main", "version": "2.0", "scope": "global"} {
					if got[key] != want {
						t.Errorf("config[%q] = %v, want %v", key, got[key], want)
					}
				}
			},
		},
		{
			name:   "environment",
			method: &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "numpy", "environment": "old"}},
			plan: func() *plan.ResolvedInstallPlan {
				resolved := plan.New("demo", "conda", true)
				resolved.Identity.Package = "numpy"
				resolved.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}
				return &resolved
			}(),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				if got["environment"] != "tools" {
					t.Errorf("environment = %v, want tools", got["environment"])
				}
			},
		},
		{
			name: "native resolved package takes precedence over overrides",
			method: &config.MethodCandidate{Kind: "native", Config: map[string]any{
				"pkg": "declared", "pkg_overrides": map[string]any{"apt": "stale"},
			}},
			plan: func() *plan.ResolvedInstallPlan {
				resolved := plan.New("demo", "native", true)
				resolved.Identity.Package = "resolved-apt-package"
				return &resolved
			}(),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				if got["pkg"] != "resolved-apt-package" {
					t.Errorf("pkg = %v, want resolved-apt-package", got["pkg"])
				}
				if _, exists := got["pkg_overrides"]; exists {
					t.Error("projected native config retained stale pkg_overrides")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := maps.Clone(tt.method.Config)
			projected, err := removalMethodForResolvedTarget(tt.method, tt.plan)
			if err != nil {
				t.Fatalf("removalMethodForResolvedTarget() error = %v", err)
			}
			tt.check(t, projected.Config)
			if !reflect.DeepEqual(tt.method.Config, before) {
				t.Fatalf("original config mutated: got %#v, want %#v", tt.method.Config, before)
			}
		})
	}
}
