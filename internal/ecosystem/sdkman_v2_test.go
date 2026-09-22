package ecosystem

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestSDKManAdapterV2ResolvesObservesAndInstallsExactVersion(t *testing.T) {
	home := t.TempDir()
	exectest.SetHome(t, home)
	version := "21.0.4-tem"
	if err := os.MkdirAll(filepath.Join(home, ".sdkman", "candidates", "java", version), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewSDKManAdapter()
	tool := &config.Tool{Name: "java"}
	mc := &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{"pkg": "java", "version": version}}
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "java"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: version}
	intent.Identity.Version = version
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}

	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if resolved.Identity.Package != "java" || resolved.Identity.Version != version || resolved.Identity.RequestedVersion == nil || resolved.Identity.RequestedVersion.Mode != plan.VersionExact {
		t.Fatalf("resolved identity = %#v, want java exact %s", resolved.Identity, version)
	}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, tool, mc)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	wantObservation := plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: "java", Version: version}, KnownFields: []plan.IdentityField{plan.FieldPackage, plan.FieldVersion}}
	if !reflect.DeepEqual(observation, wantObservation) {
		t.Fatalf("Observe() = %#v, want %#v", observation, wantObservation)
	}

	// The resolved plan is authoritative; changing the method cannot change the command.
	mc.Config["pkg"] = "legacy-java"
	mc.Config["version"] = "legacy-version"
	runner := &run.FakeRunner{}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error = %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0].Name != "sdk" || !reflect.DeepEqual(runner.Calls[0].Args, []string{"install", "java", version}) {
		t.Fatalf("InstallResolved() calls = %#v, want sdk install java %s", runner.Calls, version)
	}
}

func TestSDKManAdapterV2ObserveWithoutVersionUsesCurrent(t *testing.T) {
	home := t.TempDir()
	exectest.SetHome(t, home)
	versionDir := filepath.Join(home, ".sdkman", "candidates", "java", "17.0.12-tem")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versionDir, filepath.Join(home, ".sdkman", "candidates", "java", "current")); err != nil {
		t.Fatal(err)
	}

	observation, err := NewSDKManAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "java"}, &config.MethodCandidate{Kind: "sdkman", Config: map[string]any{"pkg": "java"}})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Package != "java" || len(observation.KnownFields) != 1 {
		t.Fatalf("Observe() = %#v, want present java package identity only", observation)
	}
}
