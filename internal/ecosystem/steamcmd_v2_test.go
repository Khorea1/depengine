package ecosystem

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestSteamCMDAdapterV2Conformance(t *testing.T) {
	adapter := NewSteamCMDAdapter()
	if adapter.Kind() != "steamcmd" {
		t.Fatalf("Kind() = %q, want steamcmd", adapter.Kind())
	}

	lookup := &run.FakeRunner{LookPaths: map[string]bool{"steamcmd": true}}
	if !adapter.Available(context.Background(), lookup) {
		t.Fatal("Available() = false with steamcmd on PATH")
	}
	if len(lookup.Calls) != 1 || lookup.Calls[0].Name != "which" || !reflect.DeepEqual(lookup.Calls[0].Args, []string{"steamcmd"}) {
		t.Fatalf("Available() lookup calls = %#v", lookup.Calls)
	}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "cs2"}, &config.MethodCandidate{Kind: "steamcmd"})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceUnknown {
		t.Fatalf("Observe() presence = %q, want %q", observation.Presence, plan.PresenceUnknown)
	}
}

func TestSteamCMDAdapterV2ResolvesAndInstallsResolvedAppID(t *testing.T) {
	adapter := NewSteamCMDAdapter()
	tool := &config.Tool{Name: "cs2"}
	mc := &config.MethodCandidate{Kind: "steamcmd", Config: map[string]any{"pkg": "730"}}
	intent := plan.New(tool.Name, mc.Kind, true)

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Package != "730" {
		t.Fatalf("resolved package = %q, want 730", resolved.Identity.Package)
	}
	if resolved.Removal.Supported {
		t.Fatal("SteamCMD removal unexpectedly reported as supported")
	}

	// A different method package proves the V2 path consumes the resolved plan
	// and does not fall back to the legacy Install method.
	mc.Config["pkg"] = "legacy-value"
	runner := &run.FakeRunner{}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("InstallResolved() calls = %#v, want one call", runner.Calls)
	}
	want := []string{"+login", "anonymous", "+app_update", "730", "+quit"}
	if runner.Calls[0].Name != "steamcmd" || !reflect.DeepEqual(runner.Calls[0].Args, want) {
		t.Fatalf("InstallResolved() call = %#v, want steamcmd %#v", runner.Calls[0], want)
	}
}

func TestSteamCMDAdapterV2RejectsUnsupportedOperations(t *testing.T) {
	adapter := NewSteamCMDAdapter()
	resolved := plan.New("cs2", adapter.Kind(), true)
	resolved.Identity.Package = "730"
	resolved.Operations = []plan.Operation{{Kind: "arbitrary", Effect: plan.EffectMutation, Command: []string{"sh", "-c", "unsafe"}, ArbitraryCode: true}}

	err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, nil, nil, &resolved)
	if err == nil || !strings.Contains(err.Error(), "operations are unsupported") {
		t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
	}
}
