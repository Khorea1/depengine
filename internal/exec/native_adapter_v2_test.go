package exec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func canonicalInstallPlan(tool, kind string) *plan.ResolvedInstallPlan {
	resolved := plan.New(tool, kind, true)
	resolved.Identity.Package = tool
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	return &resolved
}

func TestNativeAdapterV2ResolvePreservesPlannerIntent(t *testing.T) {
	tests := []struct {
		name    string
		tool    *config.Tool
		method  *config.MethodCandidate
		adapter *NativeAdapter
	}{
		{"plain pkg", &config.Tool{Name: "git"}, &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "git"}}, NewNativeAdapter("debian")},
		{"planner package fallback", &config.Tool{Name: "git-tool"}, &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": ""}}, NewNativeAdapter("debian")},
		{"clan override", &config.Tool{Name: "fd"}, &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "fd", "pkg_overrides": map[string]any{"apt": "fd-find"}}}, NewNativeAdapter("debian")},
		{"override for another clan falls back to pkg", &config.Tool{Name: "fd"}, &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "fd", "pkg_overrides": map[string]any{"apt": "fd-find"}}}, NewNativeAdapter("arch")},
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

func TestNativeByManagerAdapterV2ResolveUsesPlannerPackageFallback(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "apt", Config: map[string]any{}}
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Identity.Package != tool.Name {
		t.Fatalf("planner package = %q, want tool name %q", intent.Identity.Package, tool.Name)
	}

	adapter := &NativeByManagerAdapter{managerName: "apt"}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, method, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Package != tool.Name {
		t.Fatalf("resolved package = %q, want %q", resolved.Identity.Package, tool.Name)
	}
}

func TestNativeAdapterV2ResolveRejectsUnresolvable(t *testing.T) {
	tool := &config.Tool{Name: "git"}
	mc := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "git"}}
	intent := canonicalInstallPlan("git", "native")
	adapter := NewNativeAdapter("debian")

	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("ResolvePlan(nil intent) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, nil, mc, intent); err == nil {
		t.Fatal("ResolvePlan(nil tool) should fail")
	}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, nil, intent); err == nil {
		t.Fatal("ResolvePlan(nil method) should fail")
	}
	noPkgIntent := plan.New("git", "native", true)
	noPkgIntent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &noPkgIntent); err == nil {
		t.Fatal("ResolvePlan(intent without package) should fail")
	}
	probing := NewNativeAdapter("")
	if _, err := probing.ResolvePlan(context.Background(), &run.FakeRunner{ExitCode: 1}, tool, mc, intent); err == nil {
		t.Fatal("ResolvePlan without a detectable manager should fail")
	}
}

func TestNativeAdapterV2Observe(t *testing.T) {
	tool := &config.Tool{Name: "git"}
	mc := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "git"}}
	adapter := NewNativeAdapter("debian")

	present, err := adapter.Observe(context.Background(), &run.FakeRunner{ExitCode: 0}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if present.Presence != plan.PresencePresent || present.Identity.Package != "git" {
		t.Fatalf("Observe() = %#v, want present git", present)
	}

	absent, err := adapter.Observe(context.Background(), &run.FakeRunner{ExitCode: 1}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if absent.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", absent.Presence)
	}
	if len(absent.KnownFields) != 0 {
		t.Fatalf("absent observation must not carry authoritative identity: %#v", absent)
	}

	boom := errors.New("spawn failed")
	broken, err := adapter.Observe(context.Background(), &run.FakeRunner{Err: boom}, tool, mc)
	if !errors.Is(err, boom) {
		t.Fatalf("Observe() error = %v, want spawn failure", err)
	}
	if broken.Presence != plan.PresenceBroken {
		t.Fatalf("Observe() presence = %q, want broken", broken.Presence)
	}

	empty, err := adapter.Observe(context.Background(), &run.FakeRunner{}, tool, &config.MethodCandidate{Kind: "native", Config: map[string]any{}})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if empty.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent for empty pkg", empty.Presence)
	}

	if _, err := adapter.Observe(context.Background(), &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe(nil tool) should fail")
	}
	if _, err := adapter.Observe(context.Background(), &run.FakeRunner{}, tool, nil); err == nil {
		t.Fatal("Observe(nil method) should fail")
	}
}

func TestNativeAdapterV2InstallResolved(t *testing.T) {
	tool := &config.Tool{Name: "git"}
	mc := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "git"}}
	adapter := NewNativeAdapter("debian")

	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, canonicalInstallPlan("git", "native")); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 2 || runner.Calls[0].Name != "sudo" || runner.Calls[1].Name != "dpkg" {
		t.Fatalf("InstallResolved() calls = %#v, want elevated apt-get install + dpkg verification", runner.Calls)
	}
	found := false
	for _, arg := range runner.Calls[0].Args {
		if arg == "git" {
			found = true
		}
	}
	if !found {
		t.Fatalf("InstallResolved() args = %v, want package git", runner.Calls[0].Args)
	}

	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("InstallResolved(nil plan) should fail")
	}
}

func TestNativeAdapterV2InstallResolvedUsesResolvedPackage(t *testing.T) {
	tool := &config.Tool{Name: "fd"}
	mc := &config.MethodCandidate{Kind: "native", Config: map[string]any{
		"pkg":           "fd",
		"pkg_overrides": map[string]any{"apt": "fd-find"},
	}}
	adapter := NewNativeAdapter("debian")
	resolved := canonicalInstallPlan("fd", "native")
	resolved.Identity.Package = "fd-find"

	// The resolved plan is authoritative after resolution. Later changes to
	// method config must not alter the executable package identity.
	mc.Config["pkg"] = "wrong-package"
	mc.Config["pkg_overrides"] = map[string]any{"apt": "also-wrong"}

	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	for _, call := range runner.Calls {
		for _, arg := range call.Args {
			if arg == "wrong-package" || arg == "also-wrong" || arg == "fd" {
				t.Fatalf("InstallResolved() used method config instead of resolved package: %#v", runner.Calls)
			}
		}
	}
	found := false
	for _, call := range runner.Calls {
		for _, arg := range call.Args {
			if arg == "fd-find" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("InstallResolved() did not use resolved package identity: %#v", runner.Calls)
	}
}

func TestNativeV2InstallResolvedRejectsNonCanonicalOperations(t *testing.T) {
	adapters := []struct {
		name string
		kind string
		call func(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error
	}{
		{"native", "native", NewNativeAdapter("debian").InstallResolved},
		{"apt", "apt", (&NativeByManagerAdapter{managerName: "apt"}).InstallResolved},
		{"winget", "winget", (&NativeByManagerAdapter{managerName: "winget"}).InstallResolved},
	}
	for _, adapter := range adapters {
		t.Run(adapter.name, func(t *testing.T) {
			intent := canonicalInstallPlan("demo", adapter.kind)
			cases := []struct {
				name       string
				operations []plan.Operation
			}{
				{"extra", append(append([]plan.Operation(nil), intent.Operations...), plan.Operation{Kind: "arbitrary", Effect: plan.EffectMutation})},
				{"none", nil},
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
					tool := &config.Tool{Name: "demo"}
					mc := &config.MethodCandidate{Kind: adapter.kind, Config: map[string]any{"pkg": "demo"}}
					err := adapter.call(context.Background(), &run.FakeRunner{}, tool, mc, &resolved)
					want := adapter.name + ": resolved operations are unsupported"
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("InstallResolved() error = %v, want %q", err, want)
					}
				})
			}
		})
	}
}

func TestNativeByManagerAdapterV2ResolveObserveInstall(t *testing.T) {
	tool := &config.Tool{Name: "git"}
	mc := &config.MethodCandidate{Kind: "apt", Config: map[string]any{"pkg": "git"}}
	adapter := &NativeByManagerAdapter{managerName: "apt"}

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{ExitCode: 0}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Package != "git" {
		t.Fatalf("Observe() = %#v, want present git", observation)
	}
	absent, err := adapter.Observe(context.Background(), &run.FakeRunner{ExitCode: 1}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if absent.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", absent.Presence)
	}

	mc.Config["pkg"] = "wrong-package"
	installRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), installRunner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(installRunner.Calls) != 2 || installRunner.Calls[0].Name != "sudo" || installRunner.Calls[1].Name != "dpkg" {
		t.Fatalf("InstallResolved() calls = %#v, want elevated apt-get install + dpkg verification", installRunner.Calls)
	}
	foundResolvedPkg := false
	for _, arg := range installRunner.Calls[0].Args {
		if arg == "wrong-package" {
			t.Fatalf("InstallResolved() used mutated method package: %#v", installRunner.Calls)
		}
		if arg == "git" {
			foundResolvedPkg = true
		}
	}
	if !foundResolvedPkg {
		t.Fatalf("InstallResolved() args = %v, want resolved package git", installRunner.Calls[0].Args)
	}

	unknown := &NativeByManagerAdapter{managerName: "nonexistent"}
	if _, err := unknown.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent); err == nil {
		t.Fatal("ResolvePlan(unknown manager) should fail")
	}
	missing, err := unknown.Observe(context.Background(), &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if missing.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent for unknown manager", missing.Presence)
	}
}

func wingetV2TestMethod() (*config.Tool, *config.MethodCandidate) {
	tool := &config.Tool{Name: "git"}
	mc := &config.MethodCandidate{Kind: "winget", Config: map[string]any{
		"pkg": "Git.Git", "version": "2.53.0", "source": "winget",
		"scope": "machine", "architecture": "x64", "installer_type": "msi",
	}}
	return tool, mc
}

func TestNativeByManagerWingetV2ObserveReportsVersionIdentity(t *testing.T) {
	tool, mc := wingetV2TestMethod()
	adapter := &NativeByManagerAdapter{managerName: "winget"}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{Stdout: "Git Git.Git 2.53.0 x64 winget\n"}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() presence = %q, want present", observation.Presence)
	}
	if observation.Identity.Package != "Git.Git" || observation.Identity.Version != "2.53.0" || observation.Identity.Source != "winget" {
		t.Fatalf("Observe() identity = %#v, want package/version/source", observation.Identity)
	}

	absent, err := adapter.Observe(context.Background(), &run.FakeRunner{ExitCode: 1}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if absent.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent on non-zero exit", absent.Presence)
	}

	unparseable, err := adapter.Observe(context.Background(), &run.FakeRunner{Stdout: "no matching packages\n"}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if unparseable.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent when output has no package row", unparseable.Presence)
	}
}

func TestNativeByManagerWingetV2ObserveReconcilesDrift(t *testing.T) {
	tool, mc := wingetV2TestMethod()
	adapter := &NativeByManagerAdapter{managerName: "winget"}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}

	drifted, err := adapter.Observe(context.Background(), &run.FakeRunner{Stdout: "Git Git.Git 2.52.0 winget\n"}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	// Version drift stays observable as present-with-identity instead of
	// collapsing to absent: the reconciler reports drifted.
	if drifted.Presence != plan.PresencePresent || drifted.Identity.Version != "2.52.0" {
		t.Fatalf("Observe() = %#v, want present with observed 2.52.0", drifted)
	}
	result := plan.Reconcile(intent.Identity, drifted)
	if result.State != plan.StateDrifted {
		t.Fatalf("Reconcile() state = %q, want drifted; result = %#v", result.State, result)
	}
	found := false
	for _, drift := range result.Drift {
		if drift.Field == plan.FieldVersion && drift.Desired == "2.53.0" && drift.Observed == "2.52.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Reconcile() drift = %#v, want version 2.53.0 -> 2.52.0", result.Drift)
	}

	matched, err := adapter.Observe(context.Background(), &run.FakeRunner{Stdout: "Git Git.Git 2.53.0 winget\n"}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	matchedResult := plan.Reconcile(intent.Identity, matched)
	if len(matchedResult.Drift) != 0 {
		t.Fatalf("Reconcile() drift = %#v, want none for matching version", matchedResult.Drift)
	}
}

func TestNativeByManagerWingetV2InstallResolvedUsesResolvedIdentity(t *testing.T) {
	tool, mc := wingetV2TestMethod()
	adapter := &NativeByManagerAdapter{managerName: "winget"}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}

	// The resolved plan is authoritative for identity dimensions. Mutating the
	// candidate after resolution must not redirect the package, version, source,
	// scope, or architecture selected for installation. installer_type is an
	// execution-only field and remains read from the candidate.
	for key, value := range map[string]any{
		"pkg": "Other.Package", "version": "9.9.9", "source": "other-source",
		"scope": "user", "architecture": "arm64", "installer_type": "zip",
	} {
		mc.Config[key] = value
	}
	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "winget" {
		t.Fatalf("InstallResolved() calls = %#v, want one winget call", runner.Calls)
	}
	want := []string{"install", "--id", "Git.Git", "--exact", "--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity", "--version", "2.53.0", "--source", "winget", "--scope", "machine", "--architecture", "x64", "--installer-type", "zip"}
	got := runner.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("InstallResolved() args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("InstallResolved() args = %v, want %v", got, want)
		}
	}
}

func TestNativeByManagerWingetV2ExecuteFieldsChangeCommand(t *testing.T) {
	fields := []struct {
		name  string
		value string
		flag  string
	}{
		{"pkg", "Other.Package", "--id"},
		{"version", "9.9.9", "--version"},
		{"source", "other-source", "--source"},
		{"scope", "user", "--scope"},
		{"architecture", "arm64", "--architecture"},
		{"installer_type", "zip", "--installer-type"},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			toolA, methodA := wingetV2TestMethod()
			toolB, methodB := wingetV2TestMethod()
			methodB.Config[field.name] = field.value
			adapter := &NativeByManagerAdapter{managerName: "winget"}
			install := func(tool *config.Tool, method *config.MethodCandidate) []run.FakeCall {
				t.Helper()
				intent, err := planner.BuildCandidateIntent(tool, method)
				if err != nil {
					t.Fatal(err)
				}
				resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, method, &intent)
				if err != nil {
					t.Fatal(err)
				}
				runner := &run.FakeRunner{}
				if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
					t.Fatal(err)
				}
				if len(runner.Calls) != 1 {
					t.Fatalf("InstallResolved() calls = %#v, want one call", runner.Calls)
				}
				return runner.Calls
			}
			callA, callB := install(toolA, methodA), install(toolB, methodB)
			if equalFakeCall(callA[0], callB[0]) {
				t.Fatalf("changing %s did not change install command: A=%#v B=%#v", field.name, callA[0], callB[0])
			}
			if field.name == "pkg" && (!hasFlagValue(callA[0].Args, field.flag, "Git.Git") || !hasFlagValue(callB[0].Args, field.flag, field.value)) {
				t.Fatalf("commands A=%v B=%v do not select configured package identities", callA[0].Args, callB[0].Args)
			}
			if field.name != "pkg" && !hasFlagValue(callB[0].Args, field.flag, field.value) {
				t.Fatalf("B command %v does not include %s=%q", callB[0].Args, field.flag, field.value)
			}
		})
	}
}

func TestNativeByManagerWingetV2VerifyFieldsChangeObservation(t *testing.T) {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"pkg", "Other.Package"},
		{"version", "9.9.9"},
		{"source", "other-source"},
	} {
		t.Run(field.name, func(t *testing.T) {
			toolA, methodA := wingetV2TestMethod()
			toolB, methodB := wingetV2TestMethod()
			methodB.Config[field.name] = field.value
			adapter := &NativeByManagerAdapter{managerName: "winget"}
			outputA, outputB := "Git Git.Git 2.53.0 x64 winget\n", "Git Git.Git 2.53.0 x64 winget\n"
			if field.name == "pkg" {
				outputB = "Other Other.Package 2.53.0 x64 winget\n"
			}
			runnerA := &run.FakeRunner{Stdout: outputA}
			runnerB := &run.FakeRunner{Stdout: outputB}
			observationA, err := adapter.Observe(context.Background(), runnerA, toolA, methodA)
			if err != nil {
				t.Fatal(err)
			}
			observationB, err := adapter.Observe(context.Background(), runnerB, toolB, methodB)
			if err != nil {
				t.Fatal(err)
			}
			if len(runnerA.Calls) != 1 || len(runnerB.Calls) != 1 {
				t.Fatalf("Observe calls A=%#v B=%#v, want one query each", runnerA.Calls, runnerB.Calls)
			}
			if field.name == "pkg" && equalFakeCall(runnerA.Calls[0], runnerB.Calls[0]) {
				t.Fatalf("changing pkg did not change verification query: %#v", runnerA.Calls[0])
			}
			if field.name == "source" && equalFakeCall(runnerA.Calls[0], runnerB.Calls[0]) {
				t.Fatalf("changing source did not change verification query: %#v", runnerA.Calls[0])
			}
			intentA, err := planner.BuildCandidateIntent(toolA, methodA)
			if err != nil {
				t.Fatal(err)
			}
			intentB, err := planner.BuildCandidateIntent(toolB, methodB)
			if err != nil {
				t.Fatal(err)
			}
			resultA, resultB := plan.Reconcile(intentA.Identity, observationA), plan.Reconcile(intentB.Identity, observationB)
			if field.name == "pkg" {
				if resultA.Observed.Package != "Git.Git" || resultB.Observed.Package != "Other.Package" {
					t.Fatalf("changing pkg did not change observed identity: A=%#v B=%#v", resultA, resultB)
				}
			} else {
				wantField := plan.FieldVersion
				if field.name == "source" {
					wantField = plan.FieldSource
				}
				if hasDrift(resultA, wantField) || !hasDrift(resultB, wantField) {
					t.Fatalf("changing %s did not change reconciled field drift: A=%#v B=%#v", field.name, resultA, resultB)
				}
			}
		})
	}
}

func TestNativeAdapterV2PackageFieldChangesExecuteAndVerify(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	methodA := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo"}}
	methodB := &config.MethodCandidate{Kind: "native", Config: map[string]any{"pkg": "demo-alt"}}
	adapter := NewNativeAdapter("debian")
	install := func(method *config.MethodCandidate) run.FakeCall {
		t.Helper()
		intent, err := planner.BuildCandidateIntent(tool, method)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, method, &intent)
		if err != nil {
			t.Fatal(err)
		}
		runner := &run.FakeRunner{}
		if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
			t.Fatal(err)
		}
		for _, call := range runner.Calls {
			if call.Name == "sudo" {
				return call
			}
		}
		t.Fatalf("InstallResolved() calls = %#v, want apt-get", runner.Calls)
		return run.FakeCall{}
	}
	callA, callB := install(methodA), install(methodB)
	if equalFakeCall(callA, callB) || !contains(callA.Args, "demo") || !contains(callB.Args, "demo-alt") {
		t.Fatalf("native pkg was not observable in execute command: A=%#v B=%#v", callA, callB)
	}

	observe := func(method *config.MethodCandidate) (plan.Observation, run.FakeCall) {
		t.Helper()
		runner := &run.FakeRunner{}
		observation, err := adapter.Observe(context.Background(), runner, tool, method)
		if err != nil {
			t.Fatal(err)
		}
		if len(runner.Calls) != 1 {
			t.Fatalf("Observe() calls = %#v, want one package query", runner.Calls)
		}
		return observation, runner.Calls[0]
	}
	observationA, queryA := observe(methodA)
	observationB, queryB := observe(methodB)
	if equalFakeCall(queryA, queryB) || !contains(queryA.Args, "demo") || !contains(queryB.Args, "demo-alt") {
		t.Fatalf("native pkg was not observable in verification query: A=%#v B=%#v", queryA, queryB)
	}
	if observationA.Identity.Package != "demo" || observationB.Identity.Package != "demo-alt" {
		t.Fatalf("native observations A=%#v B=%#v, want configured package identities", observationA, observationB)
	}
}

func equalFakeCall(a, b run.FakeCall) bool {
	if a.Name != b.Name || a.Dir != b.Dir || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
}

func hasFlagValue(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func hasDrift(result plan.VerificationResult, field plan.IdentityField) bool {
	for _, drift := range result.Drift {
		if drift.Field == field {
			return true
		}
	}
	return false
}
