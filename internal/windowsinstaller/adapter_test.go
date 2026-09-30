package windowsinstaller

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type fakeCatalog struct {
	identity  string
	found     bool
	err       error
	kind      string
	name      string
	publisher string
}

func (f *fakeCatalog) Find(_ context.Context, _ run.Runner, kind, name, publisher string) (string, bool, error) {
	f.kind, f.name, f.publisher = kind, name, publisher
	return f.identity, f.found, f.err
}

func TestObserveUsesExactInstallerIdentity(t *testing.T) {
	catalog := &fakeCatalog{identity: "Example.App_1.0_x64", found: true}
	adapter := newAdapter("msix")
	adapter.catalog = catalog
	mc := &config.MethodCandidate{Kind: "msix", Config: map[string]any{"pkg": "Example.App", "publisher": "CN=Example"}}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, nil, mc)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Presence != plan.PresencePresent {
		t.Fatalf("observation = %#v", observation)
	}
	if catalog.kind != "msix" || catalog.name != "Example.App" || catalog.publisher != "CN=Example" {
		t.Fatalf("lookup = (%q, %q, %q)", catalog.kind, catalog.name, catalog.publisher)
	}
}

func TestObservePropagatesCatalogFailureAsBroken(t *testing.T) {
	adapter := newAdapter("appx")
	adapter.catalog = &fakeCatalog{err: errors.New("query failed")}
	mc := &config.MethodCandidate{Kind: "appx", Config: map[string]any{"pkg": "Example.App", "publisher": "CN=Example"}}

	observation, err := adapter.Observe(context.Background(), &run.FakeRunner{}, nil, mc)
	if err == nil || observation.Presence != plan.PresenceBroken || !strings.Contains(observation.Detail, "query failed") {
		t.Fatalf("observation = %#v, err = %v", observation, err)
	}
}

func TestRemoveEXERunsExplicitTokenizedUninstaller(t *testing.T) {
	adapter := newAdapter("exe")
	adapter.catalog = &fakeCatalog{identity: "present", found: true}
	runner := &run.FakeRunner{}
	uninstaller := "/Program Files/Example/uninstall.exe"
	if runtime.GOOS == "windows" {
		uninstaller = `C:\Program Files\Example\uninstall.exe`
	}
	mc := &config.MethodCandidate{Kind: "exe", Config: map[string]any{
		"product_name":   "Example Tool",
		"publisher":      "Example Corp",
		"uninstall_exe":  uninstaller,
		"uninstall_args": []string{"/quiet", "/norestart"},
	}}

	if err := adapter.Remove(context.Background(), runner, nil, mc); err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("calls = %#v", runner.Calls)
	}
	if got, want := append([]string{runner.Calls[0].Name}, runner.Calls[0].Args...), []string{uninstaller, "/quiet", "/norestart"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestRemoveAbsentPackageIsNoop(t *testing.T) {
	adapter := newAdapter("appx")
	adapter.catalog = &fakeCatalog{}
	runner := &run.FakeRunner{}
	mc := &config.MethodCandidate{Kind: "appx", Config: map[string]any{"pkg": "Example.App", "publisher": "CN=Example"}}

	if err := adapter.Remove(context.Background(), runner, nil, mc); err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("unexpected calls: %#v", runner.Calls)
	}
}

func TestEXERemovalRequiresAbsoluteExplicitExecutable(t *testing.T) {
	adapter := newAdapter("exe")
	adapter.catalog = &fakeCatalog{identity: "present", found: true}
	mc := &config.MethodCandidate{Kind: "exe", Config: map[string]any{
		"product_name":  "Example Tool",
		"publisher":     "Example Corp",
		"uninstall_exe": "uninstall.exe",
	}}

	err := adapter.Remove(context.Background(), &run.FakeRunner{}, nil, mc)
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("err = %v", err)
	}
}

func TestHostCompatibilityRequiresWindows(t *testing.T) {
	if err := NewEXEAdapter().CheckHostCompatibility(nil, nil, nil, nil, "linux"); err == nil {
		t.Fatal("non-Windows host accepted")
	}
	if err := NewMSIXAdapter().CheckHostCompatibility(nil, nil, nil, nil, "windows"); err != nil {
		t.Fatal(err)
	}
}

func TestImportDoesNotRegisterAdapters(t *testing.T) {
	for _, kind := range []string{"exe", "msix", "appx"} {
		if exec.Lookup(kind) != nil {
			t.Fatalf("importing windowsinstaller registered %q", kind)
		}
	}
}
