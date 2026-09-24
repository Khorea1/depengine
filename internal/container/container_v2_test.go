package container

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

func containerMethod(mc map[string]any) *config.MethodCandidate {
	mc["manager"] = "podman"
	return &config.MethodCandidate{Kind: "container", Config: mc}
}

func TestContainerAdapterV2ResolvesObservesAndInstallsTag(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	tool := tool("tool")
	mc := containerMethod(map[string]any{"source": "registry.example.test/team/tool", "tag": "1.2.3"})

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}

	probe := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "sha256:deadbeef\n"}
	observation, err := adapter.Observe(ctx, probe, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	wantObservation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Source: "registry.example.test/team/tool"},
		KnownFields: []plan.IdentityField{plan.FieldSource},
	}
	if !reflect.DeepEqual(observation, wantObservation) {
		t.Fatalf("Observe() = %#v, want %#v", observation, wantObservation)
	}
	if result := plan.Reconcile(resolved.Identity, observation); result.State != plan.StateSatisfied {
		t.Fatalf("Reconcile() = %#v, want satisfied", result)
	}

	// The resolved plan is authoritative; changing the candidate cannot
	// change the pulled reference.
	mc.Config["source"] = "registry.example.test/team/decoy"
	mc.Config["tag"] = "9.9.9"
	installRunner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if err := adapter.InstallResolved(ctx, installRunner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := installRunner.calls[len(installRunner.calls)-1]
	want := []string{"pull", "registry.example.test/team/tool:1.2.3"}
	if last.Name != "podman" || !equalArgs(last.Args, want) {
		t.Fatalf("InstallResolved ran %v %v, want podman %v", last.Name, last.Args, want)
	}
}

func TestContainerAdapterV2ObserveDigestIdentity(t *testing.T) {
	ctx := context.Background()
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mc := containerMethod(map[string]any{"source": "ghcr.io/owner/tool", "digest": digest})
	probe := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}

	observation, err := NewContainerAdapter().Observe(ctx, probe, tool("tool"), mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Source: "ghcr.io/owner/tool", Digest: digest},
		KnownFields: []plan.IdentityField{plan.FieldSource, plan.FieldDigest},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}
	if got := probe.calls[len(probe.calls)-1].Args; !equalArgs(got, []string{"image", "inspect", "ghcr.io/owner/tool@" + digest}) {
		t.Fatalf("Observe probe argv = %v", got)
	}
}

func TestContainerAdapterV2ObservePlatformIdentity(t *testing.T) {
	ctx := context.Background()
	mc := containerMethod(map[string]any{"source": "redis", "tag": "7", "platform": "linux/arm64/v8"})
	probe := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "linux/arm64/v8\n"}

	observation, err := NewContainerAdapter().Observe(ctx, probe, tool("redis"), mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Source: "redis", Platform: "linux/arm64/v8"},
		KnownFields: []plan.IdentityField{plan.FieldSource, plan.FieldPlatform},
	}
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("Observe() = %#v, want %#v", observation, want)
	}

	drift := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "linux/amd64\n"}
	observation, err = NewContainerAdapter().Observe(ctx, drift, tool("redis"), mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent on platform drift", observation.Presence)
	}
}

func TestContainerAdapterV2ObserveAbsentWhenImageMissing(t *testing.T) {
	ctx := context.Background()
	mc := containerMethod(map[string]any{"source": "redis", "tag": "7"})
	probe := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: ""}

	observation, err := NewContainerAdapter().Observe(ctx, probe, tool("redis"), mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", observation.Presence)
	}
}

func TestContainerAdapterV2ObserveUnknownOnInvalidConfig(t *testing.T) {
	ctx := context.Background()
	probe := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "sha256:abc\n"}
	mc := &config.MethodCandidate{Kind: "container", Config: map[string]any{}}

	observation, err := NewContainerAdapter().Observe(ctx, probe, tool("x"), mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceUnknown {
		t.Fatalf("Observe() presence = %q, want unknown", observation.Presence)
	}
	if len(probe.calls) != 0 {
		t.Fatalf("Observe should not invoke the runner with incomplete config, got calls: %v", probe.calls)
	}
}

func TestContainerAdapterV2ObserveUnknownWhenProbeFails(t *testing.T) {
	ctx := context.Background()
	mc := containerMethod(map[string]any{"source": "redis"})
	runner := &run.FakeRunner{Err: errors.New("exec: podman not found")}

	observation, err := NewContainerAdapter().Observe(ctx, runner, tool("redis"), mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceUnknown {
		t.Fatalf("Observe() presence = %q, want unknown", observation.Presence)
	}
}

func TestContainerAdapterV2ObserveRequiresToolAndMethod(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	mc := containerMethod(map[string]any{"source": "redis"})
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, tool("redis"), nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

func TestContainerAdapterV2ResolvePlanRejectsInvalidConfig(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	tool := tool("redis")
	intent := plan.New(tool.Name, "container", true)

	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, &config.MethodCandidate{Kind: "container", Config: map[string]any{}}, &intent); err == nil {
		t.Fatal("ResolvePlan() with empty config succeeded, want error")
	}
	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, containerMethod(map[string]any{"source": "redis"}), nil); err == nil {
		t.Fatal("ResolvePlan() with nil intent succeeded, want error")
	}
	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, nil, containerMethod(map[string]any{"source": "redis"}), &intent); err == nil {
		t.Fatal("ResolvePlan() with nil tool succeeded, want error")
	}
}

func TestContainerAdapterV2InstallResolvedRejectsNonCanonicalOperations(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	tool := tool("redis")
	mc := containerMethod(map[string]any{"source": "redis"})
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
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
		{"empty", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := intent.Clone()
			resolved.Operations = tc.operations
			err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, &resolved)
			if err == nil || !strings.Contains(err.Error(), "container: resolved operations are unsupported") {
				t.Fatalf("InstallResolved() error = %v, want %q", err, "container: resolved operations are unsupported")
			}
		})
	}
}

func TestContainerAdapterV2InstallResolvedRequiresConcreteIdentity(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	tool := tool("redis")
	mc := containerMethod(map[string]any{"source": "redis"})
	resolved := plan.New(tool.Name, "container", true)
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved() with no source succeeded, want error")
	}
	if err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("InstallResolved() with nil plan succeeded, want error")
	}
}

func TestContainerAdapterV2InstallResolvedFailsClosedWithoutDeclaredCredential(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	tool := tool("widget")
	mc := containerMethod(map[string]any{"source": "registry.example/acme/widget"})
	mc.SecretRef = &config.SecretReference{Provider: "env", Name: "REGISTRY_TOKEN"}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	runner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if err := adapter.InstallResolved(ctx, runner, tool, mc, resolved); err == nil || !strings.Contains(err.Error(), "declared registry credential is unavailable") {
		t.Fatalf("InstallResolved() error = %v, want missing declared credential error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("missing declared credential reached runner: %#v", runner.calls)
	}
}

func TestContainerAdapterV2InstallResolvedUsesPlatform(t *testing.T) {
	ctx := context.Background()
	adapter := NewContainerAdapter()
	tool := tool("redis")
	mc := containerMethod(map[string]any{"source": "redis", "tag": "7", "platform": "linux/arm64/v8"})
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	runner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if err := adapter.InstallResolved(ctx, runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	last := runner.calls[len(runner.calls)-1]
	want := []string{"pull", "--platform", "linux/arm64/v8", "redis:7"}
	if last.Name != "podman" || !equalArgs(last.Args, want) {
		t.Fatalf("InstallResolved ran %v %v, want podman %v", last.Name, last.Args, want)
	}
}
