package exec

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

type nativeEffectDescriber interface {
	DescribeProspectiveEffects(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, plan.TransitionKind) (plan.ProspectiveEffectEnvelope, error)
}

func nativeEffectFixture(toolName, method, pkg string) (*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) {
	tool := &config.Tool{Name: toolName}
	methodCandidate := &config.MethodCandidate{Kind: method, Config: map[string]any{"pkg": "stale-method-config"}}
	resolved := plan.New(toolName, method, true)
	resolved.Identity.Package = pkg
	return tool, methodCandidate, &resolved
}

func describeNativeEffect(t *testing.T, a nativeEffectDescriber, toolName, method, pkg string, transition plan.TransitionKind) plan.ProspectiveEffectEnvelope {
	t.Helper()
	tool, mc, resolved := nativeEffectFixture(toolName, method, pkg)
	before := resolved.Clone()
	got, err := a.DescribeProspectiveEffects(tool, mc, resolved, transition)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*resolved, before) {
		t.Fatal("prospective effects changed the resolved plan")
	}
	if got.Completeness != plan.EffectUnknown {
		t.Fatalf("invented complete write-set: %+v", got)
	}
	for _, step := range got.Steps {
		if step.Completeness != plan.EffectUnknown {
			t.Fatalf("step classification overclaims: %+v", step)
		}
	}
	return got
}

func nativePrimaryTargets(t *testing.T, got plan.ProspectiveEffectEnvelope) []plan.ProspectiveTarget {
	t.Helper()
	for _, step := range got.Steps {
		if step.Phase == "primary_"+string(got.Transition) {
			return step.Targets
		}
	}
	t.Fatalf("no primary phase: %+v", got)
	return nil
}

func TestNativeProspectiveEffectsFindsSameDeclaredPackageAcrossLogicalNames(t *testing.T) {
	native := describeNativeEffect(t, NewNativeAdapter("debian"), "rust", "native", "rustc", plan.TransitionInstall)
	explicit := describeNativeEffect(t, &NativeByManagerAdapter{managerName: "apt"}, "rustup", "apt", "rustc", plan.TransitionInstall)
	want := []plan.ProspectiveTarget{{Scope: "declared-native-manager", Kind: "package-target", Identity: "debian/apt/rustc"}}
	for _, envelope := range []plan.ProspectiveEffectEnvelope{native, explicit} {
		if got := nativePrimaryTargets(t, envelope); !reflect.DeepEqual(got, want) {
			t.Fatalf("targets = %+v, want %+v", got, want)
		}
	}
	if got := plan.CompareProspectiveEffectEnvelopes(native, explicit); got != plan.OverlapSame {
		t.Fatalf("same package via native and apt aliases: overlap = %s", got)
	}
	different := describeNativeEffect(t, &NativeByManagerAdapter{managerName: "apt"}, "curl", "apt", "curl", plan.TransitionInstall)
	if got := plan.CompareProspectiveEffectEnvelopes(native, different); got != plan.OverlapUnknown {
		t.Fatalf("different declared packages cannot be proved disjoint: %s", got)
	}
}

// The planner and adapter, not the stale method map, establish package identity.
// This checks the real read-only candidate-intent -> ResolvePlan -> envelope path.
func TestNativeProspectiveEffectsFollowResolvedNativeIdentity(t *testing.T) {
	tool := &config.Tool{Name: "fd"}
	mc := &config.MethodCandidate{Kind: "apt", Config: map[string]any{"pkg": "fd-find"}}
	a := &NativeByManagerAdapter{managerName: "apt"}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := a.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Identity.Package != "fd-find" {
		t.Fatalf("resolved package = %q, want fd-find", resolved.Identity.Package)
	}
	// Modifying the method config after resolution must not redirect effects.
	mc.Config["pkg"] = "completely-other-package"
	envelope, err := a.DescribeProspectiveEffects(tool, mc, resolved, plan.TransitionInstall)
	if err != nil {
		t.Fatal(err)
	}
	want := []plan.ProspectiveTarget{{Scope: "declared-native-manager", Kind: "package-target", Identity: "debian/apt/fd-find"}}
	if targets := nativePrimaryTargets(t, envelope); !reflect.DeepEqual(targets, want) {
		t.Fatalf("resolved effects = %+v, want %+v", targets, want)
	}
}

func TestNativeProspectiveEffectsManagerAliasesAndIsolation(t *testing.T) {
	tests := []struct {
		name, manager, want string
	}{
		{"dnf", "dnf", "fedora/dnf/ripgrep"},
		{"dnf5", "dnf5", "fedora/dnf/ripgrep"},
		{"yum", "yum", "fedora/dnf/ripgrep"},
		{"brew", "brew", "macos/brew/ripgrep"},
		{"MacPorts", "port", "macos/port/ripgrep"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeNativeEffect(t, &NativeByManagerAdapter{managerName: tc.manager}, "rg", tc.manager, "ripgrep", plan.TransitionInstall)
			want := []plan.ProspectiveTarget{{Scope: "declared-native-manager", Kind: "package-target", Identity: tc.want}}
			if targets := nativePrimaryTargets(t, got); !reflect.DeepEqual(targets, want) {
				t.Fatalf("targets = %+v, want %+v", targets, want)
			}
		})
	}
	brew := describeNativeEffect(t, &NativeByManagerAdapter{managerName: "brew"}, "rg", "brew", "ripgrep", plan.TransitionInstall)
	port := describeNativeEffect(t, &NativeByManagerAdapter{managerName: "port"}, "rg", "port", "ripgrep", plan.TransitionInstall)
	if got := plan.CompareProspectiveEffectEnvelopes(brew, port); got != plan.OverlapUnknown {
		t.Fatalf("different manager namespaces do not prove disjointness: %s", got)
	}
}

func TestNativeProspectiveEffectsFailClosedOnInsufficientIdentity(t *testing.T) {
	cases := []struct {
		name       string
		a          nativeEffectDescriber
		kind       string
		pkg        string
		transition plan.TransitionKind
	}{
		{"unresolved native clan", NewNativeAdapter(""), "native", "git", plan.TransitionInstall},
		{"unsupported clan", NewNativeAdapter("unrecognized"), "native", "git", plan.TransitionInstall},
		{"ambiguous manager", &NativeByManagerAdapter{managerName: "pkg"}, "pkg", "git", plan.TransitionInstall},
		{"unknown manager", &NativeByManagerAdapter{managerName: "unknown"}, "unknown", "git", plan.TransitionInstall},
		{"missing package", &NativeByManagerAdapter{managerName: "apt"}, "apt", "", plan.TransitionInstall},
		{"remove uses tracked identity instead", &NativeByManagerAdapter{managerName: "apt"}, "apt", "git", plan.TransitionRemove},
		{"upgrade may replace another identity", &NativeByManagerAdapter{managerName: "apt"}, "apt", "git", plan.TransitionUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := describeNativeEffect(t, tc.a, "git", tc.kind, tc.pkg, tc.transition)
			if targets := nativePrimaryTargets(t, result); len(targets) != 0 {
				t.Fatalf("invented a physical target: %+v", targets)
			}
		})
	}
	// A mutable, auto-detected clan is not consulted from the host-free
	// effect helper: doing so would race with Available's first-time probe.
	auto := NewNativeAdapter("")
	auto.clan = "debian" // Simulate a completed detection, not a constructor fact.
	got := describeNativeEffect(t, auto, "git", "native", "git", plan.TransitionInstall)
	if targets := nativePrimaryTargets(t, got); len(targets) != 0 {
		t.Fatalf("auto-detected clan must not be treated as immutable evidence: %+v", targets)
	}

	tool, mc, resolved := nativeEffectFixture("git", "apt", "git")
	for _, label := range []string{"tool mismatch", "method mismatch"} {
		t.Run(label, func(t *testing.T) {
			tool, mc, resolved := nativeEffectFixture("git", "apt", "git")
			if label == "tool mismatch" {
				tool.Name = "other"
			} else {
				mc.Kind = "dnf"
			}
			envelope, err := (&NativeByManagerAdapter{managerName: "apt"}).DescribeProspectiveEffects(tool, mc, resolved, plan.TransitionInstall)
			if err != nil {
				t.Fatal(err)
			}
			if got := nativePrimaryTargets(t, envelope); len(got) != 0 {
				t.Fatalf("mismatched input: %+v", got)
			}
		})
	}

	resolved.Version = -1
	if _, err := (&NativeByManagerAdapter{managerName: "apt"}).DescribeProspectiveEffects(tool, mc, resolved, plan.TransitionInstall); err == nil {
		t.Fatal("malformed plan must be rejected")
	}
}
