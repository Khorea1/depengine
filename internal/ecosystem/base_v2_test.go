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

func testV2BaseConfig() BaseConfig {
	return BaseConfig{
		KindName:    "testv2",
		Binary:      "testbin",
		CheckTmpl:   []string{"testbin", "show", "{pkg}"},
		InstallTmpl: []string{"testbin", "install", "{pkg}"},
	}
}

func testV2BaseRunner() *run.FakeRunner {
	return &run.FakeRunner{LookPaths: map[string]bool{"testbin": true}}
}

func TestBaseAdapterV2ResolvePreservesPlannerIntent(t *testing.T) {
	adapter := NewBaseAdapter(Configs["pip"])
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "pip", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}

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

	runner := &run.FakeRunner{LookPaths: map[string]bool{"pip": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	want := []string{"install", "demo==1.2.3"}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "pip" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want pip %#v", runner.Calls, want)
	}
}

func TestBaseAdapterV2ResolveRejectsBadInput(t *testing.T) {
	adapter := NewBaseAdapter(testV2BaseConfig())
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "testv2", Config: map[string]any{"pkg": "demo"}}
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "demo"

	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("ResolvePlan(nil intent) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, nil, mc, &intent); err == nil {
		t.Fatal("ResolvePlan(nil tool) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, nil, &intent); err == nil {
		t.Fatal("ResolvePlan(nil method) should fail")
	}
	empty := &config.MethodCandidate{Kind: "testv2", Config: map[string]any{}}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{}, empty, &intent); err == nil {
		t.Fatal("ResolvePlan(empty package) should fail")
	}
	bare := plan.New(tool.Name, mc.Kind, true)
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &bare); err == nil {
		t.Fatal("ResolvePlan(empty intent package) should fail")
	}
}

func TestBaseAdapterV2ObservePresentAbsentAndVersion(t *testing.T) {
	adapter := NewBaseAdapter(testV2BaseConfig())
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "testv2", Config: map[string]any{"pkg": "demo-pkg", "version": "1.2.3"}}

	observation, err := adapter.Observe(context.Background(), testV2BaseRunner(), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "demo-pkg", Version: "1.2.3"},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}

	absent, err := adapter.Observe(context.Background(), &run.FakeRunner{LookPaths: map[string]bool{"testbin": true}, ExitCode: 1}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if absent.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", absent.Presence, plan.PresenceAbsent)
	}

	if _, err := adapter.Observe(context.Background(), testV2BaseRunner(), nil, mc); err == nil {
		t.Fatal("Observe(nil tool) should fail")
	}
	if _, err := adapter.Observe(context.Background(), testV2BaseRunner(), tool, nil); err == nil {
		t.Fatal("Observe(nil method) should fail")
	}
}

func TestBaseAdapterV2ObservePreservesExactVersionDrift(t *testing.T) {
	adapter := NewBaseAdapter(Configs["pip"])
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "pip", Config: map[string]any{"pkg": "demo-pkg", "version": "1.2.3"}}
	runner := &run.FakeRunner{
		LookPaths: map[string]bool{"pip": true},
		Stdout:    "Name: demo-pkg\nVersion: 1.2.4\n",
	}

	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Version != "1.2.4" {
		t.Fatalf("Observe() = %+v, want present installed version 1.2.4", observation)
	}
	verification := plan.Reconcile(plan.ResolvedIdentity{Package: "demo-pkg", Version: "1.2.3"}, observation)
	if verification.State != plan.StateDrifted || len(verification.Drift) != 1 || verification.Drift[0].Field != plan.FieldVersion {
		t.Fatalf("verification = %+v, want exact-version drift", verification)
	}
}

func TestBaseAdapterV2InstallResolvedUsesResolvedIdentity(t *testing.T) {
	adapter := NewBaseAdapter(testV2BaseConfig())
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "testv2", Config: map[string]any{"pkg": "demo"}}
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "demo"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	resolved.Identity.Package = "resolved-pkg"
	// The resolved plan is authoritative; changing the method cannot change
	// the command.
	mc.Config["pkg"] = "legacy-pkg"

	runner := testV2BaseRunner()
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "testbin" || !reflect.DeepEqual(last.Args, []string{"install", "resolved-pkg"}) {
		t.Fatalf("InstallResolved() calls = %#v, want testbin install resolved-pkg", runner.Calls)
	}
}

func TestBaseAdapterV2InstallResolvedUsesResolvedSourceAndScope(t *testing.T) {
	adapter := NewBaseAdapter(Configs["pipx"])
	tool := &config.Tool{Name: "black"}
	mc := &config.MethodCandidate{Kind: "pipx", Config: map[string]any{
		"pkg":       "black",
		"index_url": "https://resolved.example/simple",
		"scope":     "global",
	}}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}

	mc.Config["pkg"] = "stale"
	mc.Config["index_url"] = "https://stale.example/simple"
	mc.Config["scope"] = "user"

	runner := &run.FakeRunner{LookPaths: map[string]bool{"pipx": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	want := []string{"install", "--global", "--index-url", "https://resolved.example/simple", "black"}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "pipx" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want pipx %#v", runner.Calls, want)
	}
}

func TestBaseAdapterV2InstallResolvedUsesResolvedFlatpakSelector(t *testing.T) {
	adapter := NewBaseAdapter(Configs["flatpak"])
	tool := &config.Tool{Name: "org.example.Demo"}
	mc := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{
		"pkg":    "org.example.Demo",
		"remote": "flathub",
		"branch": "stable",
		"scope":  "user",
	}}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}

	mc.Config["remote"] = "stale-remote"
	mc.Config["branch"] = "stale-branch"
	mc.Config["scope"] = "system"

	runner := &run.FakeRunner{LookPaths: map[string]bool{"flatpak": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	want := []string{"install", "-y", "--user", "flathub", "org.example.Demo//stable"}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "flatpak" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want flatpak %#v", runner.Calls, want)
	}
}

func TestBaseAdapterV2InstallResolvedUsesResolvedChannel(t *testing.T) {
	adapter := NewBaseAdapter(Configs["snap"])
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "snap", Config: map[string]any{
		"pkg":     "demo",
		"channel": "edge",
	}}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}

	mc.Config["channel"] = "stable"

	runner := &run.FakeRunner{LookPaths: map[string]bool{"snap": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	want := []string{"install", "demo", "--channel=edge"}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "snap" || !reflect.DeepEqual(last.Args, want) {
		t.Fatalf("InstallResolved() calls = %#v, want snap %#v", runner.Calls, want)
	}
}

func TestBaseAdapterV2InstallResolvedDropsStaleVersion(t *testing.T) {
	adapter := NewBaseAdapter(Configs["pip"])
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "pip", Config: map[string]any{"pkg": "demo", "version": "9.9.9"}}
	resolved := plan.New(tool.Name, mc.Kind, true)
	resolved.Identity.Package = "demo"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	runner := &run.FakeRunner{LookPaths: map[string]bool{"pip": true}}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	if last.Name != "pip" || !reflect.DeepEqual(last.Args, []string{"install", "demo"}) {
		t.Fatalf("InstallResolved() calls = %#v, want unpinned pip install demo", runner.Calls)
	}
}

func TestBaseAdapterV2InstallResolvedRejectsBadInput(t *testing.T) {
	adapter := NewBaseAdapter(testV2BaseConfig())
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "testv2", Config: map[string]any{"pkg": "demo"}}

	if err := adapter.InstallResolved(context.Background(), nil, tool, mc, &plan.ResolvedInstallPlan{}); err == nil {
		t.Fatal("InstallResolved(nil runner) should fail")
	}
	if err := adapter.InstallResolved(context.Background(), testV2BaseRunner(), tool, mc, nil); err == nil {
		t.Fatal("InstallResolved(nil plan) should fail")
	}
	empty := plan.New(tool.Name, mc.Kind, true)
	empty.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(context.Background(), testV2BaseRunner(), tool, mc, &empty); err == nil {
		t.Fatal("InstallResolved(empty package) should fail")
	}
	missing := &run.FakeRunner{ExitCode: 1}
	resolved := plan.New(tool.Name, mc.Kind, true)
	resolved.Identity.Package = "demo"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(context.Background(), missing, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved(missing binary) should fail")
	}
}

func TestBaseAdapterV2InstallResolvedRejectsNonCanonicalOperations(t *testing.T) {
	adapter := NewBaseAdapter(testV2BaseConfig())
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: "testv2", Config: map[string]any{"pkg": "demo"}}
	intent, err := planner.BuildCandidateIntent(tool, &config.MethodCandidate{Kind: "pip", Config: map[string]any{"pkg": "demo"}})
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
			resolved.Identity.Package = "demo"
			resolved.Operations = tc.operations
			err := adapter.InstallResolved(context.Background(), testV2BaseRunner(), tool, mc, &resolved)
			if err == nil || !strings.Contains(err.Error(), "testv2: resolved operations are unsupported") {
				t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
			}
		})
	}
}
