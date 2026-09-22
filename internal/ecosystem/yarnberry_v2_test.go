package ecosystem

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestYarnBerryAdapterV2Conformance(t *testing.T) {
	adapter := NewYarnBerryAdapter()
	tool := &config.Tool{Name: "eslint"}
	mc := &config.MethodCandidate{Kind: adapter.Kind(), Config: map[string]any{"pkg": "eslint"}}

	runner := &run.FakeRunner{ExitCode: 1}
	observation, err := adapter.Observe(context.Background(), runner, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceAbsent)
	}

	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "eslint"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Package != "eslint" {
		t.Fatalf("resolved package = %q, want eslint", resolved.Identity.Package)
	}
	if resolved.Removal.Supported {
		t.Fatal("Yarn Berry removal unexpectedly reported as supported")
	}

	mc.Config["pkg"] = "legacy-value"
	runner = &run.FakeRunner{}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "yarn" || !reflect.DeepEqual(runner.Calls[0].Args, []string{"add", "eslint"}) {
		t.Fatalf("InstallResolved() calls = %#v, want yarn add eslint", runner.Calls)
	}
}

func TestYarnBerryAdapterV2ObservePresent(t *testing.T) {
	adapter := NewYarnBerryAdapter()
	tool := &config.Tool{Name: "eslint"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "eslint"}}
	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{ExitCode: 0}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Package != "eslint" {
		t.Fatalf("Observe() = %#v, want present eslint", observation)
	}
}

func TestYarnBerryAdapterV2RejectsUnsupportedOperations(t *testing.T) {
	adapter := NewYarnBerryAdapter()
	resolved := plan.New("eslint", adapter.Kind(), true)
	resolved.Identity.Package = "eslint"
	resolved.Operations = []plan.Operation{{Kind: "arbitrary", Effect: plan.EffectMutation, Command: []string{"sh", "-c", "unsafe"}, ArbitraryCode: true}}

	err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, nil, nil, &resolved)
	if err == nil || !strings.Contains(err.Error(), "operations are unsupported") {
		t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
	}
}
