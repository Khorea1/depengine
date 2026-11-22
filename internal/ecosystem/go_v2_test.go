package ecosystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

	observation, err := adapter.Observe(context.Background(), goV2Runner("\tpath\texample.com/project/cmd/realbin\n\tmod\texample.com/project\tv1.2.3\th1:fixture\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Package: "example.com/project/cmd/realbin", Version: "v1.2.3"},
		KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}

	if result := plan.Reconcile(resolved.Identity, observation); result.State != plan.StateSatisfied {
		t.Fatalf("Reconcile() state = %q, want %q (result = %#v)", result.State, plan.StateSatisfied, result)
	}
}

func TestGoAdapterV2ObserveReportsDriftedVersion(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()

	// A different installed version remains present so reconciliation can
	// distinguish exact-version drift from a missing binary.
	observation, err := adapter.Observe(context.Background(), goV2Runner("\tpath\texample.com/project/cmd/realbin\n\tmod\texample.com/project\tv1.2.4\th1:fixture\n"), tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Version != "v1.2.4" {
		t.Fatalf("Observe() = %+v, want present version v1.2.4", observation)
	}
	if result := plan.Reconcile(plan.ResolvedIdentity{Package: "example.com/project/cmd/realbin", Version: "v1.2.3"}, observation); result.State != plan.StateDrifted {
		t.Fatalf("Reconcile() state = %q, want %q (result = %#v)", result.State, plan.StateDrifted, result)
	}

	// Missing binary entirely.
	missing := &run.FakeRunner{LookPaths: map[string]bool{"go": true, "realbin": false, "friendly-name": false}}
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

func TestGoAdapterV2ObserveFailsClosedOnBuildInfoPackageMismatch(t *testing.T) {
	adapter := NewGoAdapter()
	tool, mc := goV2Tool()
	runner := goV2Runner("\tpath\texample.com/other/cmd/realbin\n\tmod\texample.com/other\tv9.9.9\th1:fixture\n")

	observation, err := adapter.Observe(context.Background(), runner, tool, mc)
	if !errors.Is(err, errGoBuildInfoPackageMismatch) {
		t.Fatalf("Observe() error = %v, want build-info package mismatch", err)
	}
	if observation.Presence != plan.PresenceBroken {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceBroken)
	}
	if observation.Detail == "" || !strings.Contains(observation.Detail, "example.com/other/cmd/realbin") {
		t.Fatalf("Observe() detail = %q, want mismatched embedded package", observation.Detail)
	}
	if len(observation.KnownFields) != 0 {
		t.Fatalf("Observe() known fields = %#v, want none for broken verification", observation.KnownFields)
	}
}

// A PATH name or a file in GOBIN is not proof of the package that produced it.
// The Go build-info path is the evidence used for V2 package identity.
func TestGoAdapterV2ObserveRequiresBuildInfoIdentity(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	if err := os.WriteFile(goFixturePath(binDir, "realbin"), []byte("not a trusted Go binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := NewGoAdapter()
	tool, method := goV2Tool()
	tests := []struct {
		name     string
		runner   *run.FakeRunner
		presence plan.PresenceState
	}{
		{
			name: "go metadata query fails",
			runner: &run.FakeRunner{
				LookPaths: map[string]bool{"realbin": true}, ExitCode: 1,
			},
			presence: plan.PresenceUnknown,
		},
		{
			name: "non Go binary has no build info",
			runner: &run.FakeRunner{
				LookPaths: map[string]bool{"realbin": true}, Stdout: "not Go build info\n",
			},
			presence: plan.PresenceBroken,
		},
		{
			name: "unrelated binary metadata",
			runner: &run.FakeRunner{
				LookPaths: map[string]bool{"realbin": true},
				Stdout:    "\tpath\texample.com/other/cmd/realbin\n\tmod\texample.com/other\tv1.0.0\n",
			},
			presence: plan.PresenceBroken,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			observation, err := adapter.Observe(context.Background(), tc.runner, tool, method)
			if err == nil || observation.Presence != tc.presence || len(observation.KnownFields) != 0 {
				t.Fatalf("Observe() = %#v, %v; want %s with error and no known identity", observation, err, tc.presence)
			}
			if adapter.Check(context.Background(), tc.runner, tool, method) {
				t.Fatal("Check() accepted package identity without matching build info")
			}
		})
	}
}

func TestGoAdapterV2ObserveAllowsMatchingUnversionedBuildInfo(t *testing.T) {
	t.Setenv("GOBIN", t.TempDir())
	adapter := NewGoAdapter()
	tool, method := goV2Tool()
	delete(method.Config, "version")
	runner := &run.FakeRunner{
		LookPaths: map[string]bool{"realbin": true},
		Stdout:    "\tpath\texample.com/project/cmd/realbin\n\tmod\texample.com/project\t(devel)\n",
	}
	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observation.Presence != plan.PresencePresent || observation.Identity.Package != "example.com/project/cmd/realbin" || observation.Identity.Version != "" {
		t.Fatalf("Observe() = %#v, %v; want verified package without version", observation, err)
	}
	if !adapter.Check(context.Background(), runner, tool, method) {
		t.Fatal("Check() rejected Go build info with matching package")
	}
}
func resolveTempPath(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	return resolved
}

func TestGoAdapterV2InspectsGOBINTargetBeforePATH(t *testing.T) {
	pathDir := resolveTempPath(t, t.TempDir())
	gobin := resolveTempPath(t, t.TempDir())
	t.Setenv("PATH", pathDir)
	t.Setenv("GOBIN", gobin)
	pathBinary := filepath.Join(pathDir, "realbin")
	gobinBinary := filepath.Join(gobin, "realbin")
	for _, binary := range []string{pathBinary, gobinBinary} {
		if err := os.WriteFile(binary, []byte("fixture"), 0o700); err != nil { // #nosec G306 -- the fixture must be executable to test PATH lookup.
			t.Fatal(err)
		}
	}
	adapter := NewGoAdapter()
	tool, method := goV2Tool()
	runner := goV2Runner("\tpath\texample.com/project/cmd/realbin\n\tmod\texample.com/project\tv1.2.3\th1:fixture\n")
	runner.ResolvedPaths = map[string]string{gobinBinary: gobinBinary}
	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil || observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() = %#v, %v; want present", observation, err)
	}
	calls := runner.Calls
	if len(calls) == 0 || !reflect.DeepEqual(calls[len(calls)-1].Args, []string{"version", "-m", gobinBinary}) {
		t.Fatalf("go version call = %#v, want GOBIN target %q", calls, gobinBinary)
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
