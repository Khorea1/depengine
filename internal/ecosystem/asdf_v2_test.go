package ecosystem

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type asdfFallbackRunner struct {
	results map[string]run.Result
	calls   []string
}

func (r *asdfFallbackRunner) Run(_ context.Context, name string, _ ...string) run.Result {
	r.calls = append(r.calls, name)
	return r.results[name]
}

func (r *asdfFallbackRunner) LookPath(_ context.Context, name string) bool {
	_, ok := r.results[name]
	return ok
}

func TestAsdfAdapterV2ResolvesAndInstallsAsdf(t *testing.T) {
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "20.1.0"
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "nodejs"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "20.1.0"}
	intent.Identity.Version = "20.1.0"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	a := NewAsdfAdapter()
	resolved, err := a.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil || resolved.Identity.Package != "nodejs" || resolved.Identity.Version != "20.1.0" {
		t.Fatalf("ResolvePlan() = %#v, %v", resolved, err)
	}
	runner := &run.FakeRunner{ExitCode: 0}
	if err := a.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatal(err)
	}
	want := []run.FakeCall{{Name: "which", Args: []string{"asdf"}}, {Name: "asdf", Args: []string{"plugin", "list"}}, {Name: "asdf", Args: []string{"plugin", "add", "nodejs"}}, {Name: "asdf", Args: []string{"install", "nodejs", "20.1.0"}}, {Name: "asdf", Args: []string{"set", "--home", "nodejs", "20.1.0"}}}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.Calls, want)
	}
}

func TestAsdfAdapterV2InstallsMise(t *testing.T) {
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "20.1.0"
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "nodejs"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "20.1.0"}
	intent.Identity.Version = "20.1.0"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	a := NewAsdfAdapter()
	resolved, err := a.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	runner := &run.FakeRunner{ExitCode: 0, LookPaths: map[string]bool{"asdf": false, "mise": true}}
	if err := a.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatal(err)
	}
	want := []run.FakeCall{{Name: "which", Args: []string{"asdf"}}, {Name: "which", Args: []string{"mise"}}, {Name: "mise", Args: []string{"install", "nodejs@20.1.0"}}, {Name: "mise", Args: []string{"use", "-g", "nodejs@20.1.0"}}}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.Calls, want)
	}
}

func TestAsdfAdapterV2ObserveAbsentAndBackendError(t *testing.T) {
	a := NewAsdfAdapter()
	tool, mc := asdfTool("node", "nodejs")
	obs, err := a.Observe(context.Background(), &run.FakeRunner{ExitCode: 0, Stdout: ""}, tool, mc)
	if err != nil || obs.Presence != plan.PresenceAbsent {
		t.Fatalf("absent observation = %#v, %v", obs, err)
	}
	wantErr := errors.New("backend unavailable")
	obs, err = a.Observe(context.Background(), &run.FakeRunner{ExitCode: 1, Err: wantErr, LookPaths: map[string]bool{"asdf": true}}, tool, mc)
	if err == nil || obs.Presence != plan.PresenceBroken {
		t.Fatalf("error observation = %#v, %v", obs, err)
	}
}
func TestAsdfOrMiseInstalledVersionsFallsBackToMise(t *testing.T) {
	for _, tc := range []struct {
		name string
		asdf run.Result
	}{
		{name: "asdf failed", asdf: run.Result{ExitCode: 1}},
		{name: "asdf empty", asdf: run.Result{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &asdfFallbackRunner{results: map[string]run.Result{
				"asdf": tc.asdf,
				"mise": {Stdout: []byte(`[{"version":"20.17.0","installed":true}]`)},
			}}
			installed, found, err := asdfOrMiseInstalledVersions(context.Background(), runner, "nodejs")
			if err != nil {
				t.Fatalf("asdfOrMiseInstalledVersions() error = %v", err)
			}
			if !found || installed.Backend != "mise" || !reflect.DeepEqual(installed.Versions, []string{"20.17.0"}) {
				t.Fatalf("installed = %#v, found = %v; want mise version", installed, found)
			}
			if !reflect.DeepEqual(runner.calls, []string{"asdf", "mise"}) {
				t.Fatalf("backend calls = %#v, want asdf then mise", runner.calls)
			}
		})
	}
}

func TestAsdfAdapterInstalledVersionUsesObservedExactVersion(t *testing.T) {
	t.Parallel()
	a := NewAsdfAdapter()
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "18.20.4"

	version, err := a.InstalledVersion(context.Background(), &run.FakeRunner{
		LookPaths: map[string]bool{"asdf": true, "mise": false},
		Stdout:    "  18.20.4\n  20.17.0\n",
	}, tool, mc)
	if err != nil {
		t.Fatalf("InstalledVersion() error = %v", err)
	}
	if version != "18.20.4" {
		t.Fatalf("InstalledVersion() = %q, want %q", version, "18.20.4")
	}
}

func TestAsdfAdapterInstalledVersionReportsDriftWithoutInventingUnpinnedVersion(t *testing.T) {
	t.Parallel()
	a := NewAsdfAdapter()

	t.Run("unpinned", func(t *testing.T) {
		tool, mc := asdfTool("node", "nodejs")
		version, err := a.InstalledVersion(context.Background(), &run.FakeRunner{
			LookPaths: map[string]bool{"asdf": true, "mise": false},
			Stdout:    "  20.17.0\n",
		}, tool, mc)
		if err != nil {
			t.Fatalf("InstalledVersion() error = %v", err)
		}
		if version != "" {
			t.Fatalf("InstalledVersion() = %q, want empty for unpinned intent", version)
		}
	})

	t.Run("requested version absent", func(t *testing.T) {
		tool, mc := asdfTool("node", "nodejs")
		mc.Config["version"] = "18.20.4"
		version, err := a.InstalledVersion(context.Background(), &run.FakeRunner{
			LookPaths: map[string]bool{"asdf": true, "mise": false},
			Stdout:    "  20.17.0\n",
		}, tool, mc)
		if err != nil {
			t.Fatalf("InstalledVersion() error = %v", err)
		}
		if version != "20.17.0" {
			t.Fatalf("InstalledVersion() = %q, want observed drift version 20.17.0", version)
		}
		observation, err := a.Observe(context.Background(), &run.FakeRunner{
			LookPaths: map[string]bool{"asdf": true, "mise": false},
			Stdout:    "  20.17.0\n",
		}, tool, mc)
		if err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
		verification := plan.Reconcile(plan.ResolvedIdentity{Package: "nodejs", Version: "18.20.4"}, observation)
		if verification.State != plan.StateDrifted {
			t.Fatalf("verification = %+v, want drifted", verification)
		}
	})
}

func TestMiseInstalledVersionsFiltersInstalledRows(t *testing.T) {
	versions, err := miseInstalledVersions([]byte(`[
		{"version":"20.17.0","installed":true},
		{"version":"22.0.0","installed":false}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(versions, []string{"20.17.0"}) {
		t.Fatalf("versions = %#v, want installed rows only", versions)
	}
}

func TestAsdfAdapterV2RejectsOperations(t *testing.T) {
	intent := plan.New("node", "asdf", true)
	intent.Operations = []plan.Operation{{Kind: "arbitrary", Effect: plan.EffectMutation, Command: []string{"sh"}, ArbitraryCode: true}}
	err := NewAsdfAdapter().InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "node"}, nil, &intent)
	if err == nil || err.Error() != "asdf: resolved operations are unsupported" {
		t.Fatalf("error = %v", err)
	}
}
