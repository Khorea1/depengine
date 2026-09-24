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

func cargoV2Runner(stdout string) *run.FakeRunner {
	return &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}, Stdout: stdout}
}

func TestCargoAdapterV2ResolvePreservesPlannerIntent(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"pkg":     "crate-name",
		"version": "1.2.3",
		"target":  "x86_64-unknown-linux-musl",
	}}

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

	runner := cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	assertLastCargoCall(t, runner, []string{"install", "--version", "1.2.3", "--target", "x86_64-unknown-linux-musl", "crate-name"})
}

func TestCargoAdapterV2ResolveRejectsBadInput(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "crate-name"

	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("ResolvePlan(nil intent) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, nil, mc, &intent); err == nil {
		t.Fatal("ResolvePlan(nil tool) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, nil, &intent); err == nil {
		t.Fatal("ResolvePlan(nil method) should fail")
	}
	empty := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{}}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{}, empty, &intent); err == nil {
		t.Fatal("ResolvePlan(empty package) should fail")
	}
	bare := plan.New(tool.Name, mc.Kind, true)
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &bare); err == nil {
		t.Fatal("ResolvePlan(empty intent package) should fail")
	}
}

func TestCargoAdapterV2ObserveReportsInstalledVersion(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "version": "v1.2.3"}}

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Version != "v1.2.3" {
		t.Fatalf("resolved version = %q, want planner spelling v1.2.3", resolved.Identity.Version)
	}

	observation, err := adapter.Observe(context.Background(), cargoV2Runner("crate-name v1.2.3:\n    crate-name\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "crate-name", Version: "v1.2.3"},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}

	if result := plan.Reconcile(resolved.Identity, observation); result.State != plan.StateSatisfied {
		t.Fatalf("Reconcile() state = %q, want %q (result = %#v)", result.State, plan.StateSatisfied, result)
	}
}

func TestCargoAdapterV2ObserveReportsDriftedVersion(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "version": "1.2.3"}}

	// A different installed version remains present so reconciliation can
	// distinguish exact-version drift from a missing crate.
	observation, err := adapter.Observe(context.Background(), cargoV2Runner("crate-name v1.2.4:\n    crate-name\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Version != "1.2.4" {
		t.Fatalf("Observe() = %+v, want present version 1.2.4", observation)
	}
	if result := plan.Reconcile(plan.ResolvedIdentity{Package: "crate-name", Version: "1.2.3"}, observation); result.State != plan.StateDrifted {
		t.Fatalf("Reconcile() state = %q, want %q (result = %#v)", result.State, plan.StateDrifted, result)
	}

	// Missing crate entirely.
	observation, err = adapter.Observe(context.Background(), cargoV2Runner("other v1.2.3:\n    other\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceAbsent)
	}

	// Failed query.
	observation, err = adapter.Observe(context.Background(), &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}, ExitCode: 1}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceAbsent)
	}

	if _, err := adapter.Observe(context.Background(), cargoV2Runner(""), nil, mc); err == nil {
		t.Fatal("Observe(nil tool) should fail")
	}
	if _, err := adapter.Observe(context.Background(), cargoV2Runner(""), tool, nil); err == nil {
		t.Fatal("Observe(nil method) should fail")
	}
}

func TestCargoAdapterV2ObserveMissingBinIsAbsent(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "bins": []string{"crate-cli"}}}

	observation, err := adapter.Observe(context.Background(), cargoV2Runner("crate-name v1.2.3:\n    other-bin\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q (missing selected bin)", observation.Presence, plan.PresenceAbsent)
	}
}

func TestCargoAdapterV2InstallResolvedUsesResolvedIdentity(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "version": "1.2.3"}}
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "crate-name"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.2.3"}
	intent.Identity.Version = "1.2.3"
	intent.Identity.Architecture = "x86_64-unknown-linux-musl"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	// The resolved plan is authoritative; changing the method cannot change
	// the command.
	mc.Config["pkg"] = "legacy-crate"
	mc.Config["version"] = "9.9.9"
	mc.Config["target"] = "aarch64-unknown-linux-gnu"

	runner := cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	assertLastCargoCall(t, runner, []string{"install", "--version", "1.2.3", "--target", "x86_64-unknown-linux-musl", "crate-name"})
}

func TestCargoAdapterV2InstallResolvedGitModes(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"git": "https://example.invalid/repo.git",
		"tag": "v1.2.3",
	}}
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "crate-name"
	intent.Identity.Source = "https://example.invalid/repo.git"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitTag, Value: "v1.2.3"}
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v; resolved = %#v", err, resolved)
	}
	// Omitted pkg in git mode: cargo selects the repository package itself.
	runner := cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	assertLastCargoCall(t, runner, []string{"install", "--git", "https://example.invalid/repo.git", "--tag", "v1.2.3"})

	// Explicit pkg in git mode is preserved from the resolved package.
	mc.Config["pkg"] = "crate-name"
	runner = cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	assertLastCargoCall(t, runner, []string{"install", "--git", "https://example.invalid/repo.git", "--tag", "v1.2.3", "crate-name"})
}

func TestCargoAdapterV2InstallResolvedRootFromEnvironment(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "root": "~/.local/cargo-tools"}}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Environment == nil {
		t.Fatal("ResolvePlan() should preserve the planner root environment")
	}
	mc.Config["root"] = "/tmp/legacy-root"

	runner := cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	assertLastCargoCall(t, runner, []string{"install", "--root", config.ExpandHomeDir("~/.local/cargo-tools"), "crate-name"})
}

func TestCargoRejectsUnsupportedResolvedTarget(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}
	intent := plan.New(tool.Name, "cargo", true)
	intent.Identity.Package = "crate-name"
	intent.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "tools"}
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent); err == nil {
		t.Fatal("ResolvePlan accepted named target")
	}
	fr := cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), fr, tool, mc, &intent); err == nil {
		t.Fatal("InstallResolved accepted named target")
	}
}

func TestCargoAdapterV2InstallResolvedRejectsConflicts(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate"}
	newIntent := func() *plan.ResolvedInstallPlan {
		intent := plan.New(tool.Name, "cargo", true)
		intent.Identity.Package = "crate"
		intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
		return &intent
	}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate"}}

	gitVersion := newIntent()
	gitVersion.Identity.Source = "https://example.invalid/repo.git"
	gitVersion.Identity.Version = "1.2.3"
	if err := adapter.InstallResolved(context.Background(), cargoV2Runner(""), tool, mc, gitVersion); err == nil {
		t.Fatal("InstallResolved(git+version) should fail")
	}

	refWithoutGit := newIntent()
	refWithoutGit.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "deadbeef"}
	if err := adapter.InstallResolved(context.Background(), cargoV2Runner(""), tool, mc, refWithoutGit); err == nil {
		t.Fatal("InstallResolved(rev without git) should fail")
	}
}

func TestCargoAdapterV2InstallResolvedRejectsBadInput(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate"}}

	if err := adapter.InstallResolved(context.Background(), nil, tool, mc, &plan.ResolvedInstallPlan{}); err == nil {
		t.Fatal("InstallResolved(nil runner) should fail")
	}
	if err := adapter.InstallResolved(context.Background(), cargoV2Runner(""), tool, mc, nil); err == nil {
		t.Fatal("InstallResolved(nil plan) should fail")
	}
	empty := plan.New(tool.Name, mc.Kind, true)
	empty.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(context.Background(), cargoV2Runner(""), tool, mc, &empty); err == nil {
		t.Fatal("InstallResolved(empty package) should fail")
	}
	resolved := plan.New(tool.Name, mc.Kind, true)
	resolved.Identity.Package = "crate"
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{ExitCode: 1}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved(missing binary) should fail")
	}
}

func TestCargoAdapterV2InstallResolvedRejectsNonCanonicalOperations(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate"}}
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
			err := adapter.InstallResolved(context.Background(), cargoV2Runner(""), tool, mc, &resolved)
			if err == nil || !strings.Contains(err.Error(), "cargo: resolved operations are unsupported") {
				t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
			}
		})
	}
}
