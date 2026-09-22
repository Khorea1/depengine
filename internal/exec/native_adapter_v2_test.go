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
	empty := &config.MethodCandidate{Kind: "native", Config: map[string]any{}}
	if _, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, empty, intent); err == nil {
		t.Fatal("ResolvePlan(empty pkg) should fail")
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
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "sudo" {
		t.Fatalf("InstallResolved() calls = %#v, want elevated apt-get install", runner.Calls)
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

func TestNativeAdapterV2InstallResolvedHonorsPkgOverrides(t *testing.T) {
	// The planner intent is host-independent, so the clan override is applied
	// at execution time from mc — InstallResolved must install fd-find, not fd.
	tool := &config.Tool{Name: "fd"}
	mc := &config.MethodCandidate{Kind: "native", Config: map[string]any{
		"pkg":           "fd",
		"pkg_overrides": map[string]any{"apt": "fd-find"},
	}}
	adapter := NewNativeAdapter("debian")
	resolved := canonicalInstallPlan("fd", "native")

	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	for _, call := range runner.Calls {
		for _, arg := range call.Args {
			if arg == "fd" {
				t.Fatalf("InstallResolved() used fallback pkg despite matching override: %#v", runner.Calls)
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
		t.Fatalf("InstallResolved() did not use matching pkg_overrides: %#v", runner.Calls)
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

	installRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), installRunner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(installRunner.Calls) != 1 || installRunner.Calls[0].Name != "sudo" {
		t.Fatalf("InstallResolved() calls = %#v, want elevated apt-get install", installRunner.Calls)
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

	// The resolved plan is authoritative for identity dimensions: mutating the
	// method afterwards must not change the installed version.
	mc.Config["version"] = "9.9.9"
	runner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "winget" {
		t.Fatalf("InstallResolved() calls = %#v, want one winget call", runner.Calls)
	}
	want := []string{"install", "--id", "Git.Git", "--exact", "--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity", "--version", "2.53.0", "--source", "winget", "--scope", "machine", "--architecture", "x64", "--installer-type", "msi"}
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
