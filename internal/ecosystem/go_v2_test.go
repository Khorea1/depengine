package ecosystem

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func goV2Runner(stdout string) *run.FakeRunner {
	return &run.FakeRunner{
		LookPaths: map[string]bool{"go": true, "realbin": true},
		Stdout:    stdout,
	}
}

func goV2Tool() (*config.Tool, *config.MethodCandidate) {
	return &config.Tool{Name: "friendly-name"},
		&config.MethodCandidate{Kind: "go", Config: map[string]any{
			"pkg": "example.com/project/cmd/realbin", "version": "v1.2.3",
		}}
}

func TestGoAdapterV2ResolvePreservesPlannerIntent(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v; resolved = %#v", err, resolved)
	}

	runner := goV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	want := []string{"install", "example.com/project/cmd/realbin@v1.2.3"}
	if last.Name != "go" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want go %#v", runner.Calls, want)
	}
}

func TestGoAdapterV2ResolveRejectsBadInput(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "example.com/project/cmd/realbin"

	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("ResolvePlan(nil intent) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, nil, mc, &intent); err == nil {
		t.Fatal("ResolvePlan(nil tool) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, nil, &intent); err == nil {
		t.Fatal("ResolvePlan(nil method) should fail")
	}
	empty := &config.MethodCandidate{Kind: "go", Config: map[string]any{}}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{}, empty, &intent); err == nil {
		t.Fatal("ResolvePlan(empty package) should fail")
	}
	bare := plan.New(tool.Name, mc.Kind, true)
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &bare); err == nil {
		t.Fatal("ResolvePlan(empty intent package) should fail")
	}
}

func TestGoAdapterV2ObserveReportsDiscoveredVersion(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()

	observation, err := adapter.Observe(context.Background(), goV2Runner("realbin version 1.2.3\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "example.com/project/cmd/realbin", Version: "1.2.3"},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}

	if result := plan.Reconcile(plan.ResolvedIdentity{Package: "example.com/project/cmd/realbin", Version: "1.2.3"}, observation); result.State != plan.StateSatisfied {
		t.Fatalf("Reconcile() state = %q, want %q (result = %#v)", result.State, plan.StateSatisfied, result)
	}
}

func TestGoAdapterV2ObserveReportsDriftedVersion(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()

	// A different installed version remains present so reconciliation can
	// distinguish exact-version drift from a missing binary.
	observation, err := adapter.Observe(context.Background(), goV2Runner("realbin version 1.2.4\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Version != "1.2.4" {
		t.Fatalf("Observe() = %+v, want present version 1.2.4", observation)
	}
	if result := plan.Reconcile(plan.ResolvedIdentity{Package: "example.com/project/cmd/realbin", Version: "1.2.3"}, observation); result.State != plan.StateDrifted {
		t.Fatalf("Reconcile() state = %q, want %q (result = %#v)", result.State, plan.StateDrifted, result)
	}

	// Missing binary entirely.
	missing := &run.FakeRunner{LookPaths: map[string]bool{"go": true}}
	observation, err = adapter.Observe(context.Background(), missing, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceAbsent)
	}

	if _, err := adapter.Observe(context.Background(), goV2Runner(""), nil, mc); err == nil {
		t.Fatal("Observe(nil tool) should fail")
	}
	if _, err := adapter.Observe(context.Background(), goV2Runner(""), tool, nil); err == nil {
		t.Fatal("Observe(nil method) should fail")
	}
}

func TestGoAdapterV2InstallResolvedUsesResolvedIdentity(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "example.com/project/cmd/realbin"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "v1.2.3"}
	intent.Identity.Version = "v1.2.3"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	// The resolved plan is authoritative; changing the method cannot change
	// the command.
	mc.Config["pkg"] = "example.com/legacy/cmd/legacy"
	mc.Config["version"] = "v9.9.9"

	runner := goV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	want := []string{"install", "example.com/project/cmd/realbin@v1.2.3"}
	if last.Name != "go" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want go %#v", runner.Calls, want)
	}
}

func TestGoAdapterV2InstallResolvedDefaultsToLatest(t *testing.T) {
	adapter := NewGoAdapter()
	resolved := plan.New("friendly-name", "go", true)
	resolved.Identity.Package = "example.com/project/cmd/realbin"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	runner := goV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, nil, nil, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	want := []string{"install", "example.com/project/cmd/realbin@latest"}
	if last.Name != "go" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want go %#v", runner.Calls, want)
	}
}

func TestGoAdapterV2InstallResolvedPreservesLegacyVersionSuffix(t *testing.T) {
	adapter := NewGoAdapter()
	resolved := plan.New("realbin", "go", true)
	resolved.Identity.Package = "example.com/project/cmd/realbin@v1.2.3"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	runner := goV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, nil, nil, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	want := []string{"install", "example.com/project/cmd/realbin@v1.2.3"}
	if last.Name != "go" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want go %#v", runner.Calls, want)
	}
}

func TestGoAdapterV2InstallResolvedRejectsDuplicateVersion(t *testing.T) {
	adapter := NewGoAdapter()
	resolved := plan.New("realbin", "go", true)
	resolved.Identity.Package = "example.com/project/cmd/realbin@v1.2.3"
	resolved.Identity.Version = "v1.2.4"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	if err := adapter.InstallResolved(context.Background(), goV2Runner(""), nil, nil, &resolved); err == nil {
		t.Fatal("InstallResolved(pkg suffix + version) should fail")
	}
}

func TestGoAdapterV2InstallResolvedRejectsBadInput(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()

	if err := adapter.InstallResolved(context.Background(), nil, tool, mc, &plan.ResolvedInstallPlan{}); err == nil {
		t.Fatal("InstallResolved(nil runner) should fail")
	}
	if err := adapter.InstallResolved(context.Background(), goV2Runner(""), tool, mc, nil); err == nil {
		t.Fatal("InstallResolved(nil plan) should fail")
	}
	empty := plan.New(tool.Name, mc.Kind, true)
	empty.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(context.Background(), goV2Runner(""), tool, mc, &empty); err == nil {
		t.Fatal("InstallResolved(empty package) should fail")
	}
	resolved := plan.New(tool.Name, mc.Kind, true)
	resolved.Identity.Package = "example.com/project/cmd/realbin"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{ExitCode: 1}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved(missing binary) should fail")
	}
}

func TestGoAdapterV2InstallResolvedRejectsNonCanonicalOperations(t *testing.T) {
	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.com/project/cmd/realbin"}}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	cases := []struct {
		name       string
		operations []plan.Operation
	}{
		{"extra", append(append([]plan.Operation(nil), intent.Operations...), plan.Operation{Kind: "arbitrary", Effect: plan.EffectMutation})},
		{"arbitrary", []plan.Operation{{Kind: "install", Effect: plan.EffectMutation, Command: []string{"sh"}, ArbitraryCode: true}}},
		{"command", []plan.Operation{{Kind: "install", Effect: plan.EffectMutation, Command: []string{"ignored"}}}},
		{"non-install", []plan.Operation{{Kind: "remove", Effect: plan.EffectMutation}}},
		{"wrong-effect", []plan.Operation{{Kind: "install", Effect: plan.EffectReadOnly}}},
		{"description", []plan.Operation{{Kind: "install", Description: "forged", Effect: plan.EffectMutation}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := intent.Clone()
			resolved.Operations = tc.operations
			err := adapter.InstallResolved(context.Background(), goV2Runner(""), tool, mc, &resolved)
			if err == nil || !strings.Contains(err.Error(), "go: resolved operations are unsupported") {
				t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
			}
		})
	}
}
