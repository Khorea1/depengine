package pkg

import (
	"context"
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
	if exec.Lookup("macpkg") != nil {
		t.Fatal("importing internal/pkg registered an adapter")
	}
}

func pkgMethod(packageID string) *config.MethodCandidate {
	return &config.MethodCandidate{Kind: "macpkg", Config: map[string]any{
		"url":        "https://example.invalid/demo.pkg",
		"package_id": packageID,
	}}
}

func TestAdapterAvailable(t *testing.T) {
	found := &run.FakeRunner{ExitCode: 0}
	if !NewAdapter().Available(context.Background(), found) {
		t.Fatal("Available() = false when installer and pkgutil are found")
	}
	missing := &run.FakeRunner{ExitCode: 1}
	if NewAdapter().Available(context.Background(), missing) {
		t.Fatal("Available() = true when installer/pkgutil are missing")
	}
}

func TestAdapterHostCompatibilityRequiresMacOS(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("host is macOS")
	}
	if err := NewAdapter().CheckHostCompatibility(nil, nil, nil, nil, ""); err == nil {
		t.Fatal("CheckHostCompatibility() accepted a non-macOS host")
	}
}

func TestAdapterKindAndRemoveCapability(t *testing.T) {
	a := NewAdapter()
	if a.Kind() != "macpkg" {
		t.Fatalf("Kind() = %q, want pkg", a.Kind())
	}
	if a.CanRemove() {
		t.Fatal("CanRemove() = true, want false: pkg removal is unsupported")
	}
	if err := a.Remove(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo")); err == nil {
		t.Fatal("Remove() succeeded, want explicit unsupported error")
	}
}

func TestAdapterObservePresentWithVersion(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0, Stdout: "package-id: com.example.demo\nversion: 1.2.3\nvolume: /\ninstall-time: 1700000000\n"}
	observation, err := NewAdapter().Observe(context.Background(), fr, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo"))
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("Observe() presence = %q, want present", observation.Presence)
	}
	if observation.Identity.Version != "1.2.3" {
		t.Fatalf("Observe() version = %q, want 1.2.3", observation.Identity.Version)
	}
	call := fr.Calls[len(fr.Calls)-1]
	if call.Name != "pkgutil" || call.Args[0] != "--pkg-info" || call.Args[1] != "com.example.demo" {
		t.Fatalf("Observe() call = %v, want pkgutil --pkg-info", call)
	}
}

func TestAdapterObserveAbsentWhenNoReceipt(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "no receipt for 'com.example.demo'\n"}
	observation, err := NewAdapter().Observe(context.Background(), fr, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo"))
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", observation.Presence)
	}
}

func TestAdapterCheckUsesReceipt(t *testing.T) {
	installed := &run.FakeRunner{ExitCode: 0, Stdout: "package-id: com.example.demo\n"}
	if !NewAdapter().Check(context.Background(), installed, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo")) {
		t.Fatal("Check() = false with present receipt")
	}
	missing := &run.FakeRunner{ExitCode: 1}
	if NewAdapter().Check(context.Background(), missing, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo")) {
		t.Fatal("Check() = true without receipt")
	}
}

func TestAdapterObserveWithoutPackageIDIsAbsent(t *testing.T) {
	observation, err := NewAdapter().Observe(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: "macpkg", Config: map[string]any{"url": "https://example.invalid/demo.pkg"}})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Presence != plan.PresenceAbsent {
		t.Fatalf("Observe() presence = %q, want absent", observation.Presence)
	}
	if _, err := NewAdapter().Observe(context.Background(), &run.FakeRunner{}, nil, pkgMethod("com.example.demo")); err == nil {
		t.Fatal("Observe() with nil tool succeeded, want error")
	}
}

func TestAdapterResolvePlanExposesConcreteURL(t *testing.T) {
	adapter := NewAdapter()
	mc := pkgMethod("com.example.demo")
	intent := plan.New("demo", "pkg", true)
	intent.Artifacts = []plan.Artifact{{Kind: plan.ArtifactRaw, URL: "https://example.invalid/demo.pkg"}}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL != "https://example.invalid/demo.pkg" {
		t.Fatalf("ResolvePlan() artifacts = %#v, want concrete pkg URL", resolved.Artifacts)
	}
}

func TestAdapterInstallResolvedRequiresConcreteURL(t *testing.T) {
	adapter := NewAdapter()
	resolved := plan.New("demo", "pkg", true)
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo"), &resolved); err == nil {
		t.Fatal("InstallResolved() without artifact URL succeeded, want error")
	}
	if err := adapter.InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "demo"}, pkgMethod("com.example.demo"), nil); err == nil {
		t.Fatal("InstallResolved() with nil plan succeeded, want error")
	}
}

func TestAdapterInstallStagedRejectsUntrustedSignature(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "invalid signature"}
	mc := pkgMethod("com.example.demo") // allow_untrusted absent → signature must pass
	err := NewAdapter().installStaged(context.Background(), fr, mc, "/tmp/package.pkg")
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("installStaged() error = %v, want signature rejection", err)
	}
	// Never reached the installer with an untrusted package.
	for _, call := range fr.Calls {
		if call.Name == "installer" {
			t.Fatal("installer invoked despite failed signature check")
		}
	}
}

func TestAdapterInstallStagedRunsElevatedInstaller(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	mc := pkgMethod("com.example.demo")
	mc.Config["allow_untrusted"] = true
	if err := NewAdapter().installStaged(context.Background(), fr, mc, "/tmp/package.pkg"); err != nil {
		t.Fatalf("installStaged() error = %v", err)
	}
	call := fr.Calls[len(fr.Calls)-1]
	if call.Name == "sudo" && len(call.Args) > 0 {
		call.Name, call.Args = call.Args[0], call.Args[1:]
	}
	if call.Name != "installer" || len(call.Args) != 4 || call.Args[0] != "-pkg" || call.Args[1] != "/tmp/package.pkg" || call.Args[2] != "-target" || call.Args[3] != "/" {
		t.Fatalf("installer call = %v, want installer -pkg/-target argv", call)
	}
}
