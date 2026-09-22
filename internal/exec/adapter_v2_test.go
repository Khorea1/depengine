package exec

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type legacyAdapterV2TestDouble struct {
	check   bool
	install bool
}

func (a *legacyAdapterV2TestDouble) Kind() string                               { return "legacy-test" }
func (a *legacyAdapterV2TestDouble) Available(context.Context, run.Runner) bool { return true }
func (a *legacyAdapterV2TestDouble) Check(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return a.check
}
func (a *legacyAdapterV2TestDouble) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	a.install = true
	return nil
}

func TestLegacyAdapterV2PreservesCheckAndInstall(t *testing.T) {
	legacy := &legacyAdapterV2TestDouble{check: true}
	adapter := NewLegacyAdapterV2(legacy)
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Kind: legacy.Kind()}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresencePresent)
	}

	resolved := plan.New(tool.Name, mc.Kind, false)
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, tool, mc, &resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if !legacy.install {
		t.Fatal("InstallResolved() did not delegate to legacy Install()")
	}
}

func TestLegacyAdapterV2MapsFalseCheckToAbsent(t *testing.T) {
	adapter := NewLegacyAdapterV2(&legacyAdapterV2TestDouble{})
	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, &config.MethodCandidate{})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceAbsent)
	}
}

func TestLegacyAdapterV2DoesNotExecutePlanCommands(t *testing.T) {
	adapter := NewLegacyAdapterV2(&legacyAdapterV2TestDouble{})
	resolved := plan.New("demo", adapter.Kind(), false)
	resolved.Operations = []plan.Operation{{
		Kind:          "unsupported",
		Effect:        plan.EffectMutation,
		Command:       []string{"touch", "/tmp/should-not-exist"},
		ArbitraryCode: true,
	}}

	err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: adapter.Kind()}, &resolved)
	if !errors.Is(err, ErrAdapterV2OperationsUnsupported) {
		t.Fatalf("InstallResolved() error = %v, want %v", err, ErrAdapterV2OperationsUnsupported)
	}
}

func TestLegacyAdapterV2ResolveClonesIntent(t *testing.T) {
	adapter := NewLegacyAdapterV2(&legacyAdapterV2TestDouble{})
	intent := plan.New("demo", adapter.Kind(), false)
	intent.OwnedPaths = []string{"/opt/demo"}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: adapter.Kind()}, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	resolved.OwnedPaths[0] = "/opt/changed"
	if intent.OwnedPaths[0] != "/opt/demo" {
		t.Fatal("ResolvePlan() returned a plan sharing mutable state with intent")
	}
}

func TestNewLegacyAdapterV2Nil(t *testing.T) {
	if got := NewLegacyAdapterV2(nil); got != nil {
		t.Fatalf("NewLegacyAdapterV2(nil) = %v, want nil", got)
	}
}
