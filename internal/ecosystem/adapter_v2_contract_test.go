package ecosystem

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func TestAdapterV2ResolversPreservePlannerIntent(t *testing.T) {
	tests := []struct {
		name    string
		tool    *config.Tool
		method  *config.MethodCandidate
		adapter planResolverAdapter
	}{
		{"asdf default version", &config.Tool{Name: "node"}, &config.MethodCandidate{Kind: "asdf", Config: map[string]any{"pkg": "nodejs"}}, NewAsdfAdapter()},
		{"conda default environment", &config.Tool{Name: "numpy"}, &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "numpy"}}, NewCondaAdapter()},
		{"sdkman default version", &config.Tool{Name: "java"}, &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{"pkg": "java"}}, NewSDKManAdapter()},
		{"yarn berry", &config.Tool{Name: "eslint"}, &config.MethodCandidate{Kind: "yarn-berry", Config: map[string]any{"pkg": "eslint"}}, NewYarnBerryAdapter()},
		{"pacstall", &config.Tool{Name: "foo"}, &config.MethodCandidate{Kind: "pacstall", Config: map[string]any{"pkg": "foo-pkg"}}, NewPacstallAdapter()},
		{"steamcmd", &config.Tool{Name: "cs2"}, &config.MethodCandidate{Kind: "steamcmd", Config: map[string]any{"pkg": "730"}}, NewSteamCMDAdapter()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := planner.BuildCandidateIntent(tt.tool, tt.method)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := tt.adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tt.tool, tt.method, &intent)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.ValidateResolution(intent, *resolved); err != nil {
				t.Fatalf("ValidateResolution() error = %v; resolved = %#v", err, resolved)
			}
		})
	}
}

func TestCondaInstallResolvedUsesDefaultEnvironmentWithoutRewritingPlan(t *testing.T) {
	tool := &config.Tool{Name: "numpy"}
	method := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "numpy"}}
	intent := plan.New(tool.Name, method.Kind, true)
	intent.Identity.Package = "numpy"
	var err error
	resolved, err := NewCondaAdapter().ResolvePlan(context.Background(), &run.FakeRunner{}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Identity.Environment != nil || resolved.Identity.RequestedVersion != nil {
		t.Fatalf("resolved identity rewrote omitted defaults: %#v", resolved.Identity)
	}
	runner := &run.FakeRunner{}
	if err := NewCondaAdapter().InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatal(err)
	}
	want := []run.FakeCall{{Name: "conda", Args: []string{"install", "-y", "-n", "base", "numpy"}}}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.Calls, want)
	}
}

type planResolverAdapter interface {
	ResolvePlan(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error)
}
