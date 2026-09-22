package localartifactadapter_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	localartifactadapter "github.com/Khorea1/depengine/internal/localartifactadapter"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func writeVendored(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func localMethod(root, dest string) *config.MethodCandidate {
	return &config.MethodCandidate{
		Kind:        "local",
		ProjectRoot: root,
		Config:      map[string]any{"local_path": "vendor/demo", "install_dir": dest},
	}
}

func TestAdapterV2ResolvesObservesAndInstalls(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeVendored(t, root, "demo", "payload")
	dest := t.TempDir()
	adapter := localartifactadapter.NewAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := localMethod(root, dest)

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error = %v", err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if len(resolved.Artifacts) != 1 || resolved.Artifacts[0].LocalPath != "vendor/demo" || resolved.Artifacts[0].Checksum == "" {
		t.Fatalf("ResolvePlan() artifacts = %#v, want resolved local identity with checksum", resolved.Artifacts)
	}
	if strings.Contains(resolved.Artifacts[0].Checksum, root) {
		t.Fatalf("resolved checksum leaked project root: %q", resolved.Artifacts[0].Checksum)
	}
	if err := plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatalf("ValidateResolution() error = %v", err)
	}

	observation, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent before install", observation.Presence)
	}

	runner := &run.FakeRunner{}
	if err := adapter.InstallResolved(ctx, runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("local InstallResolved invoked subprocesses: %#v", runner.Calls)
	}
	got, err := os.ReadFile(filepath.Join(dest, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("installed payload = %q, want %q", got, "payload")
	}

	observation, err = adapter.Observe(ctx, &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	wantObservation := plan.Observation{
		Presence:    plan.PresencePresent,
		Identity:    plan.ObservedIdentity{Digest: resolved.Artifacts[0].Checksum},
		KnownFields: []plan.IdentityField{plan.FieldDigest},
	}
	if !reflect.DeepEqual(observation, wantObservation) {
		t.Fatalf("Observe() = %#v, want %#v", observation, wantObservation)
	}
}

func TestAdapterV2InstallResolvedUsesPlanArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeVendored(t, root, "demo", "from-plan")
	writeVendored(t, root, "decoy", "from-candidate")
	dest := t.TempDir()
	adapter := localartifactadapter.NewAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := localMethod(root, dest)

	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	mc.Config["local_path"] = "vendor/decoy"
	if err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from-plan" {
		t.Fatalf("installed payload = %q, want plan artifact content", got)
	}
}

func TestAdapterV2InstallResolvedRejectsNonCanonicalOperations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeVendored(t, root, "demo", "payload")
	adapter := localartifactadapter.NewAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := localMethod(root, t.TempDir())
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		operations []plan.Operation
	}{
		{"empty", nil},
		{"extra", append(append([]plan.Operation(nil), resolved.Operations...), plan.Operation{Kind: "arbitrary", Effect: plan.EffectMutation})},
		{"arbitrary", []plan.Operation{{Kind: "install", Effect: plan.EffectMutation, Command: []string{"sh"}, ArbitraryCode: true}}},
		{"command", []plan.Operation{{Kind: "install", Effect: plan.EffectMutation, Command: []string{"ignored"}}}},
		{"non-install", []plan.Operation{{Kind: "remove", Effect: plan.EffectMutation}}},
		{"resolve-only", []plan.Operation{{Kind: "resolve-local-artifact", Effect: plan.EffectReadOnly}}},
		{"double-install", []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}, {Kind: "install", Effect: plan.EffectMutation}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := resolved.Clone()
			mutated.Operations = tc.operations
			err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, &mutated)
			if err == nil || !strings.Contains(err.Error(), "local: resolved operations are unsupported") {
				t.Fatalf("InstallResolved() error = %v, want %q", err, "local: resolved operations are unsupported")
			}
		})
	}
}

func TestAdapterV2InstallResolvedRequiresConcreteArtifact(t *testing.T) {
	ctx := context.Background()
	adapter := localartifactadapter.NewAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := localMethod(t.TempDir(), t.TempDir())
	resolved := plan.New(tool.Name, "local", true)
	resolved.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	if err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved() with no artifact succeeded, want error")
	}
	if err := adapter.InstallResolved(ctx, &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("InstallResolved() with nil plan succeeded, want error")
	}
	if err := adapter.InstallResolved(ctx, nil, tool, mc, &resolved); err == nil {
		t.Fatal("InstallResolved() with nil runner succeeded, want error")
	}
}

func TestAdapterV2ResolvePlanRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeVendored(t, root, "demo", "payload")
	adapter := localartifactadapter.NewAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := localMethod(root, t.TempDir())
	intent := plan.New(tool.Name, "local", true)

	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, mc, nil); err == nil {
		t.Fatal("ResolvePlan() with nil intent succeeded, want error")
	}
	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, nil, mc, &intent); err == nil {
		t.Fatal("ResolvePlan() with nil tool succeeded, want error")
	}
	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, nil, &intent); err == nil {
		t.Fatal("ResolvePlan() with nil method succeeded, want error")
	}
	missing := localMethod(root, t.TempDir())
	missing.Config["local_path"] = "vendor/missing"
	if _, err := adapter.ResolvePlan(ctx, &run.FakeRunner{}, tool, missing, &intent); err == nil {
		t.Fatal("ResolvePlan() with missing source succeeded, want error")
	}
}

func TestAdapterV2ObserveUnknownWhenSourceUnresolvable(t *testing.T) {
	ctx := context.Background()
	adapter := localartifactadapter.NewAdapter()
	tool := &config.Tool{Name: "demo"}
	mc := localMethod(t.TempDir(), t.TempDir())
	mc.Config["local_path"] = "vendor/missing"

	observation, err := adapter.Observe(ctx, &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceUnknown {
		t.Fatalf("Observe() presence = %q, want unknown", observation.Presence)
	}
	if _, err := adapter.Observe(ctx, &run.FakeRunner{}, nil, mc); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
}
