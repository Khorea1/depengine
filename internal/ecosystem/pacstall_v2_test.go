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

func TestPacstallAdapterV2ResolveAndObserve(t *testing.T) {
	adapter := NewPacstallAdapter()
	tool := &config.Tool{Name: "foo"}
	mc := &config.MethodCandidate{Kind: "pacstall", Config: map[string]any{"pkg": "foo-pkg"}}
	intent := plan.New(tool.Name, mc.Kind, true)

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Package != "foo-pkg" {
		t.Fatalf("resolved package = %q, want foo-pkg", resolved.Identity.Package)
	}
	if resolved.Removal.Supported {
		t.Fatal("Pacstall removal unexpectedly reported as supported")
	}

	runner := &run.FakeRunner{ExitCode: 0}
	observation, err := adapter.Observe(context.Background(), runner, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Package != "foo-pkg" {
		t.Fatalf("Observe() = %#v, want present foo-pkg", observation)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "pacstall" || !reflect.DeepEqual(runner.Calls[0].Args, []string{"-Ci", "foo-pkg"}) {
		t.Fatalf("Observe() calls = %#v", runner.Calls)
	}
}

func TestPacstallAdapterV2ObserveReportsAbsent(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 1}
	observation, err := NewPacstallAdapter().Observe(context.Background(), runner, &config.Tool{Name: "foo"}, &config.MethodCandidate{Kind: "pacstall", Config: map[string]any{"pkg": "foo"}})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceAbsent)
	}
}

func TestPacstallAdapterV2InstallResolvedUsesElevationAndResolvedPackage(t *testing.T) {
	oldElevated := isElevated
	isElevated = func() bool { return false }
	run.OverrideElevation("sudo")
	t.Cleanup(func() {
		isElevated = oldElevated
		run.OverrideElevation("")
	})

	adapter := NewPacstallAdapter()
	tool := &config.Tool{Name: "foo"}
	mc := &config.MethodCandidate{Kind: "pacstall", Config: map[string]any{"pkg": "resolved-pkg"}}
	intent := plan.New(tool.Name, mc.Kind, true)
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	mc.Config["pkg"] = "legacy-pkg"

	runner := &run.FakeRunner{}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "sudo" || !reflect.DeepEqual(runner.Calls[0].Args, []string{"pacstall", "-I", "resolved-pkg"}) {
		t.Fatalf("InstallResolved() call = %#v", runner.Calls)
	}
}

func TestPacstallAdapterV2RejectsUnsupportedOperations(t *testing.T) {
	resolved := plan.New("foo", "pacstall", true)
	resolved.Identity.Package = "foo"
	resolved.Operations = []plan.Operation{{Kind: "arbitrary", Effect: plan.EffectMutation, Command: []string{"sh", "-c", "unsafe"}, ArbitraryCode: true}}

	err := NewPacstallAdapter().InstallResolved(context.Background(), &run.FakeRunner{}, nil, nil, &resolved)
	if err == nil || !strings.Contains(err.Error(), "operations are unsupported") {
		t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
	}
}
