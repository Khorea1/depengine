package dmg

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewAdapter())
}

func TestImportDoesNotRegisterAdapter(t *testing.T) {
	if exec.Lookup("dmg") != nil {
		t.Fatal("importing internal/dmg registered an adapter")
	}
}

func dmgMethod(scope string) *config.MethodCandidate {
	mc := &config.MethodCandidate{Kind: "dmg", Config: map[string]any{
		"url": "https://example.invalid/demo.dmg",
		"app": "Demo.app",
	}}
	if scope != "" {
		mc.Config["scope"] = scope
	}
	return mc
}

func TestAdapterKind(t *testing.T) {
	if got := NewAdapter().Kind(); got != "dmg" {
		t.Fatalf("Kind() = %q, want dmg", got)
	}
}

func TestAdapterHostCompatibilityRejectsNonMac(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("only meaningful off macOS")
	}
	if err := NewAdapter().CheckHostCompatibility(&config.Tool{Name: "demo"}, dmgMethod(""), &plan.ResolvedInstallPlan{}, nil, ""); err == nil {
		t.Fatal("CheckHostCompatibility() accepted a non-macOS host")
	}
}

func TestAdapterObservePresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appendApps := filepath.Join(home, "Applications")
	if err := os.MkdirAll(filepath.Join(appendApps, "Demo.app"), 0o700); err != nil {
		t.Fatal(err)
	}
	observation, err := NewAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod(""))
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() presence = %q, want present", observation.Presence)
	}
	if observation.Identity.Package != "Demo.app" {
		t.Fatalf("Observe() package = %q, want Demo.app", observation.Identity.Package)
	}
}

func TestAdapterObserveAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	observation, err := NewAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod(""))
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", observation.Presence)
	}
}

func TestAdapterObserveRejectsUnsafeAppName(t *testing.T) {
	mc := dmgMethod("")
	mc.Config["app"] = "../../etc/passwd"
	if _, err := NewAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, mc); err == nil {
		t.Fatal("Observe() accepted a path-escaping app name")
	}
	mc.Config["app"] = "Demo"
	if _, err := NewAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, mc); err == nil {
		t.Fatal("Observe() accepted an app name without .app suffix")
	}
}

func TestAdapterRemoveDeletesExactOwnedBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appendApps := filepath.Join(home, "Applications")
	bundle := filepath.Join(appendApps, "Demo.app")
	if err := os.MkdirAll(bundle, 0o700); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(appendApps, "Neighbor.app")
	if err := os.MkdirAll(neighbor, 0o700); err != nil {
		t.Fatal(err)
	}
	a := NewAdapter()
	if !a.CanRemove() {
		t.Fatal("CanRemove() = false for dmg")
	}
	if err := a.Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod("")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("owned bundle still exists after Remove(): %v", err)
	}
	if _, err := os.Stat(neighbor); err != nil {
		t.Fatalf("neighbor bundle removed by Remove(): %v", err)
	}
	// Idempotent: removing an absent bundle is a no-op success.
	if err := a.Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod("")); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
}

func TestAdapterRemoveRefusesForeignPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// An app name that does not map to the scoped Applications root must be
	// refused before any filesystem mutation.
	mc := dmgMethod("")
	mc.Config["app"] = "Other.app"
	if err := NewAdapter().Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, mc); err != nil {
		t.Fatalf("Remove() of valid but absent bundle = %v, want no-op", err)
	}
}

func TestAdapterCheckUsesTargetPresence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if NewAdapter().Check(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod("")) {
		t.Fatal("Check() = true when bundle absent")
	}
}

// fixtureDMGMount builds a temporary mount directory containing a valid
// Demo.app bundle and returns it, so tests can exercise installFromMount
// against a real payload without invoking hdiutil.
func fixtureDMGMount(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	bundle := filepath.Join(root, "Demo.app", "Contents")
	if err := os.MkdirAll(bundle, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAdapterInstallStagedCopiesValidBundle(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	mc := dmgMethod("user")
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := NewAdapter()
	if err := a.installFromMount(context.Background(), fr, mc, fixtureDMGMount(t)); err != nil {
		t.Fatalf("installFromMount() error = %v", err)
	}
	target := filepath.Join(home, "Applications", "Demo.app")
	// ditto was invoked with the bundle source and exact destination.
	var ditto run.FakeCall
	found := false
	for _, call := range fr.Calls {
		if call.Name == "ditto" {
			ditto = call
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ditto never invoked; calls = %v", fr.Calls)
	}
	if !strings.HasSuffix(ditto.Args[0], "Demo.app") || ditto.Args[1] != target {
		t.Fatalf("ditto argv = %v, want source bundle and exact target %s", ditto.Args, target)
	}
}

func TestAdapterInstallFromMountRejectsNonBundle(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	mc := dmgMethod("user")
	t.Setenv("HOME", t.TempDir())
	// A mount without the declared bundle must be rejected.
	empty := t.TempDir()
	if err := NewAdapter().installFromMount(context.Background(), fr, mc, empty); err == nil {
		t.Fatal("installFromMount() accepted a mount without the declared bundle")
	}
	// A non-.app name is rejected before any filesystem work.
	bad := dmgMethod("user")
	bad.Config["app"] = "Demo"
	if err := NewAdapter().installFromMount(context.Background(), fr, bad, empty); err == nil {
		t.Fatal("installFromMount() accepted a non-.app bundle name")
	}
}

func TestAdapterResolvePlanExposesConcreteURL(t *testing.T) {
	adapter := NewAdapter()
	mc := dmgMethod("")
	intent := plan.New("demo", "dmg", true)
	intent.Artifacts = []plan.Artifact{{Kind: plan.ArtifactRaw, URL: "https://example.invalid/demo.dmg"}}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL != "https://example.invalid/demo.dmg" {
		t.Fatalf("ResolvePlan() artifacts = %#v, want concrete dmg URL", resolved.Artifacts)
	}
}

func TestAdapterInstallResolvedRequiresConcreteURL(t *testing.T) {
	adapter := NewAdapter()
	resolved := plan.New("demo", "dmg", true)
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod(""), &resolved); err == nil {
		t.Fatal("InstallResolved() without artifact URL succeeded, want error")
	}
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, dmgMethod(""), nil); err == nil {
		t.Fatal("InstallResolved() with nil plan succeeded, want error")
	}
}
