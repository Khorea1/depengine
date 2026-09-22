package msi

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewAdapter())
}

func TestImportDoesNotRegisterAdapter(t *testing.T) {
	if exec.Lookup("msi") != nil {
		t.Fatal("importing internal/msi registered an adapter")
	}
}

type errProducts struct{ err error }

func (f *errProducts) Find(string, string) (string, bool, error) {
	return "", false, f.err
}

func TestAdapterV2ObservePresent(t *testing.T) {
	adapter := NewAdapter()
	adapter.products = &fakeProducts{code: "{ABC-123}", ok: true}
	mc := &config.MethodCandidate{Kind: "msi", Config: map[string]any{
		"product_name": "Neovim",
		"publisher":    "Neovim Project",
	}}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "nvim"}, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() presence = %q, want present", observation.Presence)
	}
	if len(observation.KnownFields) != 0 {
		t.Fatalf("Observe() known fields = %v, want presence-only observation", observation.KnownFields)
	}
}

func TestAdapterV2ObserveAbsent(t *testing.T) {
	adapter := NewAdapter()
	adapter.products = &fakeProducts{}
	mc := &config.MethodCandidate{Kind: "msi", Config: map[string]any{"product_name": "Neovim"}}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "nvim"}, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", observation.Presence)
	}
}

func TestAdapterV2ObserveBrokenOnFinderError(t *testing.T) {
	adapter := NewAdapter()
	adapter.products = &errProducts{err: errors.New("registry unavailable")}
	mc := &config.MethodCandidate{Kind: "msi", Config: map[string]any{"product_name": "Neovim"}}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "nvim"}, mc)
	if err == nil {
		t.Fatal("Observe() with failing finder succeeded, want error")
	}
	if observation.Presence != plan.PresenceBroken {
		t.Fatalf("Observe() presence = %q, want broken", observation.Presence)
	}
}

func TestAdapterV2ObserveRequiresToolAndMethod(t *testing.T) {
	adapter := NewAdapter()
	mc := &config.MethodCandidate{Kind: "msi", Config: map[string]any{"product_name": "Neovim"}}
	if _, err := adapter.Observe(context.Background(), &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
	if _, err := adapter.Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "nvim"}, nil); err == nil {
		t.Fatal("Observe() with nil method succeeded, want error")
	}
}

func TestAdapterV2ResolvePlanExposesConcreteURL(t *testing.T) {
	ctx := context.Background()
	adapter := NewAdapter()
	tool := &config.Tool{Name: "nvim"}
	mc := &config.MethodCandidate{Kind: "msi", Config: map[string]any{
		"url":          "https://example.invalid/nvim.msi",
		"product_name": "Neovim",
	}}
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL != "https://example.invalid/nvim.msi" {
		t.Fatalf("ResolvePlan() artifacts = %#v, want concrete MSI URL", resolved.Artifacts)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}
}

func TestAdapterV2InstallResolvedRequiresConcreteURL(t *testing.T) {
	adapter := NewAdapter()
	tool := &config.Tool{Name: "nvim"}
	mc := &config.MethodCandidate{Kind: "msi", Config: map[string]any{"product_name": "Neovim"}}
	resolved := plan.New(tool.Name, "msi", true)
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved() with no artifact URL succeeded, want error")
	}
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("InstallResolved() with nil plan succeeded, want error")
	}
}
