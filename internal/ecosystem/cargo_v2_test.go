package ecosystem

import (
	"context"
	"errors"
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

func TestCargoAdapterV2ObserveProjectsQueriedRoot(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "root": "~/.cargo-tools"}}

	observation, err := adapter.Observe(context.Background(), cargoV2Runner(`crate-name v1.2.3:
    crate-name
`), tool, method)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	wantEnvironment := &plan.EnvironmentTarget{Kind: plan.EnvironmentPrefix, Value: "~/.cargo-tools"}
	if observation.Presence != plan.PresencePresent || !reflect.DeepEqual(observation.Identity.Environment, wantEnvironment) {
		t.Fatalf("Observe() = %#v, want queried cargo root evidence", observation)
	}
	if !reflect.DeepEqual(observation.KnownFields, []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldEnvironment}) {
		t.Fatalf("KnownFields = %#v, want package/version/environment", observation.KnownFields)
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

	if _, err := adapter.Observe(context.Background(), cargoV2Runner(""), nil, mc); err == nil {
		t.Fatal("Observe(nil tool) should fail")
	}
	if _, err := adapter.Observe(context.Background(), cargoV2Runner(""), tool, nil); err == nil {
		t.Fatal("Observe(nil method) should fail")
	}
}

// Cargo inventory failures are not evidence that a crate is absent. In
// particular, a failed query or incomplete output must not authorize an
// installation based on a false absence observation.
func TestCargoAdapterV2InventoryQueryFailures(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}
	for _, tc := range []struct {
		name   string
		runner *run.FakeRunner
	}{
		{name: "command exit", runner: &run.FakeRunner{ExitCode: 1, Stderr: "cargo metadata unavailable"}},
		{name: "spawn error", runner: &run.FakeRunner{Err: errors.New("cargo could not start")}},
		{name: "incomplete output", runner: &run.FakeRunner{Stdout: "other v1.0.0:\n    other\n", WaitErr: errors.New("truncated stream")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation, err := adapter.Observe(context.Background(), tc.runner, tool, method)
			if err == nil || observation.Presence != plan.PresenceBroken {
				t.Fatalf("Observe() = %#v, %v; want broken observation with error", observation, err)
			}
			if observation.Detail == "" {
				t.Fatal("Observe() must preserve the failure diagnostic")
			}
			if version, err := adapter.InstalledVersion(context.Background(), tc.runner, tool, method); err == nil || version != "" {
				t.Fatalf("InstalledVersion() = %q, %v; want empty version with error", version, err)
			}
			if adapter.Check(context.Background(), tc.runner, tool, method) {
				t.Fatal("Check() must not accept failed inventory query")
			}
		})
	}
}

// A successful cargo query must contain a complete, well-formed inventory.
// Malformed unrelated entries matter too: they cannot establish absence.
func TestCargoAdapterV2RejectsMalformedInventories(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}
	for _, tc := range []struct{ name, stdout string }{
		{"diagnostic instead of inventory", "unexpected text\n"},
		{"missing version", "crate-name v:\n    crate-name\n"},
		{"invalid version", "crate-name vbanana:\n    crate-name\n"},
		{"missing binaries", "crate-name v1.2.3:\n"},
		{"unindented binary", "crate-name v1.2.3:\ncrate-name\n"},
		{"orphan binary", "    crate-name\n"},
		{"truncated final line", "crate-name v1.2.3:\n    crate-name"},
		{"garbage after valid entry", "crate-name v1.2.3:\n    crate-name\nunexpected\n"},
		{"malformed unrelated entry", "other v1.0.0:\n    other\ncrate-name v1.2.3:\n"},
		{"unexpected header suffix", "crate-name v1.2.3: extra\n    crate-name\n"},
		{"whitespace only", " \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := cargoV2Runner(tc.stdout)
			observation, err := adapter.Observe(context.Background(), runner, tool, method)
			if err == nil || observation.Presence != plan.PresenceBroken || observation.Detail == "" {
				t.Fatalf("Observe() = %#v, %v; want broken observation with diagnostic", observation, err)
			}
			if version, err := adapter.InstalledVersion(context.Background(), runner, tool, method); err == nil || version != "" {
				t.Fatalf("InstalledVersion() = %q, %v; want empty version and error", version, err)
			}
			if adapter.Check(context.Background(), runner, tool, method) {
				t.Fatal("Check() must not accept malformed inventory")
			}
		})
	}
}

func TestCargoAdapterV2AcceptsCompleteInventories(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}
	for _, tc := range []struct {
		name, stdout string
		presence     plan.PresenceState
		version      string
	}{
		{"empty inventory", "", plan.PresenceAbsent, ""},
		{"other package", "other v1.0.0:\n    other\n", plan.PresenceAbsent, ""},
		{"multiple packages", "other v1.0.0:\n    other\ncrate-name v1.2.3:\n    tool-one\n    tool-two\n", plan.PresencePresent, "1.2.3"},
		{"prerelease and CRLF", "crate-name v1.2.3-rc.1+build.2:\r\n    crate-name\r\n", plan.PresencePresent, "1.2.3-rc.1+build.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation, err := adapter.Observe(context.Background(), cargoV2Runner(tc.stdout), tool, method)
			if err != nil || observation.Presence != tc.presence || observation.Identity.Version != tc.version {
				t.Fatalf("Observe() = %#v, %v; want presence=%q version=%q", observation, err, tc.presence, tc.version)
			}
		})
	}
}

func TestCargoAdapterV2SelectsRepeatedPackageByBinaries(t *testing.T) {
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly"}
	method := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}
	stdout := "crate-name v1.2.3 (/work/crate):\n    local-bin\n" +
		"crate-name v1.2.4 (git+https://example.com/crate#abc123):\n    git-bin\n" +
		"crate-name v1.2.5 (registry `custom`):\n    registry-bin\n"

	observation, err := adapter.Observe(context.Background(), cargoV2Runner(stdout), tool, method)
	if err == nil || observation.Presence != plan.PresenceBroken {
		t.Fatalf("Observe() = %#v, %v; want ambiguous inventory error", observation, err)
	}
	method.Config["bins"] = []string{"git-bin"}
	observation, err = adapter.Observe(context.Background(), cargoV2Runner(stdout), tool, method)
	if err != nil || observation.Presence != plan.PresencePresent || observation.Identity.Version != "1.2.4" {
		t.Fatalf("Observe() with git-bin = %#v, %v; want present version 1.2.4", observation, err)
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

func TestCargoAdapterV2LockedGitBranchInstallsPinnedRevision(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{
		Kind:           "cargo",
		LockedRevision: commit,
		Config: map[string]any{
			"pkg":    "demo",
			"git":    "https://example.test/demo.git",
			"branch": "main",
		},
	}
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Identity.Revision != commit || resolved.Identity.RequestedVersion == nil || resolved.Identity.RequestedVersion.Mode != plan.VersionGitBranch {
		t.Fatalf("resolved identity = %+v, want branch intent pinned to %s", resolved.Identity, commit)
	}
	runner := cargoV2Runner("")
	if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatal(err)
	}
	assertLastCargoCall(t, runner, []string{"install", "--git", "https://example.test/demo.git", "--rev", commit, "demo"})
}
