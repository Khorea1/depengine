package ecosystem

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
)

func condaTool(name, pkg string) (*config.Tool, *config.MethodCandidate) {
	tool := &config.Tool{Name: name}
	mc := &config.MethodCandidate{
		Kind:   "conda",
		Config: map[string]any{"pkg": pkg},
	}
	return tool, mc
}

func TestCondaAdapterAvailable(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &CondaAdapter{}
	if !a.Available(context.Background(), fr) {
		t.Fatal("expected Available=true when conda is found")
	}
}

func TestCondaAdapterAvailableMissing(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	a := &CondaAdapter{}
	if a.Available(context.Background(), fr) {
		t.Fatal("expected Available=false when conda is not found")
	}
}

func TestCondaAdapterCheck(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Stdout: `[{"name":"python","version":"3.11.0","build":"h123_0","channel":"defaults"}]`, ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if !a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=true when package is installed")
	}
}

func TestCondaAdapterCheckNotInstalled(t *testing.T) {
	t.Parallel()
	// Stdout doesn't contain the package name.
	fr := &run.FakeRunner{Stdout: `[]`, ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("nodejs", "nodejs")
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when package is not installed")
	}
}

func TestCondaAdapterCheckNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &CondaAdapter{}
	tool := &config.Tool{Name: "tool"}
	mc := &config.MethodCandidate{Kind: "conda"} // no pkg
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when no package name")
	}
}

func TestCondaAdapterInstall(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if err := a.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Install error: %v", err)
	}

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	got := fr.Calls[0]
	if got.Name != "conda" || !reflect.DeepEqual(got.Args, []string{"install", "-y", "-n", "base", "python"}) {
		t.Errorf("unexpected install call: %v", got)
	}
}

func TestCondaAdapterInstallFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected Install error, got nil")
	}
}

func TestCondaAdapterInstallNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &CondaAdapter{}
	tool := &config.Tool{Name: ""} // both tool name and config are empty
	mc := &config.MethodCandidate{Kind: "conda"}
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error for missing package name")
	}
}

func TestCondaAdapterCanRemove(t *testing.T) {
	t.Parallel()
	a := &CondaAdapter{}
	if !exec.CanRemove(a) {
		t.Fatal("CondaAdapter should implement Remover with CanRemove=true")
	}
}

func TestCondaAdapterRemove(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if err := a.Remove(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Remove error: %v", err)
	}

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	got := fr.Calls[0]
	if got.Name != "conda" || !reflect.DeepEqual(got.Args, []string{"remove", "-y", "-n", "base", "python"}) {
		t.Errorf("unexpected remove call: %v", got)
	}
}

func TestCondaAdapterRemoveFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "PackagesNotFoundError"}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if err := a.Remove(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected Remove error, got nil")
	}
}

func TestCondaAdapterRemoveNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &CondaAdapter{}
	tool := &config.Tool{Name: ""} // both tool name and config are empty
	mc := &config.MethodCandidate{Kind: "conda"}
	if err := a.Remove(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error for missing package name")
	}
}

func TestCondaPkgFieldOverridesToolNameAcrossOperations(t *testing.T) {
	ctx := context.Background()
	adapter := NewCondaAdapter()
	tool, mc := condaTool("friendly-name", "actual-package")

	checkRunner := &run.FakeRunner{Stdout: `[{"name":"actual-package","version":"1.2.3","build":"py_0"}]`, ExitCode: 0}
	if !adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should use conda.pkg instead of tool.Name")
	}
	assertCondaCall(t, checkRunner, []string{"list", "--json", "-n", "base", "actual-package"})

	installRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Install(ctx, installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertCondaCall(t, installRunner, []string{"install", "-y", "-n", "base", "actual-package"})

	removeRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Remove(ctx, removeRunner, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	assertCondaCall(t, removeRunner, []string{"remove", "-y", "-n", "base", "actual-package"})
}

func assertCondaCall(t *testing.T, fr *run.FakeRunner, want []string) {
	t.Helper()
	if len(fr.Calls) != 1 {
		t.Fatalf("conda calls = %d, want 1: %#v", len(fr.Calls), fr.Calls)
	}
	got := fr.Calls[0]
	if got.Name != "conda" || !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("conda call = %s %#v, want conda %#v", got.Name, got.Args, want)
	}
}

func TestCondaEnvironmentTargetIsStableAcrossLifecycle(t *testing.T) {
	ctx := context.Background()
	adapter := NewCondaAdapter()
	tool := &config.Tool{Name: "python"}
	mc := &config.MethodCandidate{Kind: "conda", Config: map[string]any{
		"pkg":         "python",
		"environment": "tools",
	}}

	check := &run.FakeRunner{Stdout: `[{"name":"python","version":"3.12.1"}]`}
	if !adapter.Check(ctx, check, tool, mc) {
		t.Fatal("expected package in named environment to satisfy check")
	}
	assertCondaCall(t, check, []string{"list", "--json", "-n", "tools", "python"})

	install := &run.FakeRunner{}
	if err := adapter.Install(ctx, install, tool, mc); err != nil {
		t.Fatal(err)
	}
	assertCondaCall(t, install, []string{"install", "-y", "-n", "tools", "python"})

	remove := &run.FakeRunner{}
	if err := adapter.Remove(ctx, remove, tool, mc); err != nil {
		t.Fatal(err)
	}
	assertCondaCall(t, remove, []string{"remove", "-y", "-n", "tools", "python"})
}

func TestCondaPrefixVersionBuildAndChannels(t *testing.T) {
	ctx := context.Background()
	adapter := NewCondaAdapter()
	tool := &config.Tool{Name: "numpy"}
	mc := &config.MethodCandidate{Kind: "conda", Config: map[string]any{
		"pkg":      "numpy",
		"version":  "2.1.0",
		"build":    "py312_0",
		"prefix":   "~/.conda/envs/data",
		"channels": []string{"conda-forge", "defaults"},
	}}

	install := &run.FakeRunner{}
	if err := adapter.Install(ctx, install, tool, mc); err != nil {
		t.Fatal(err)
	}
	wantPrefix := config.ExpandHomeDir("~/.conda/envs/data")
	assertCondaCall(t, install, []string{
		"install", "-y", "-p", wantPrefix,
		"-c", "conda-forge", "-c", "defaults",
		"numpy=2.1.0=py312_0",
	})

	check := &run.FakeRunner{Stdout: `[{"name":"numpy","version":"2.1.0","build":"py312_0","channel":"conda-forge"}]`}
	if !adapter.Check(ctx, check, tool, mc) {
		t.Fatal("exact conda version/build should satisfy check")
	}
	check.Stdout = `[{"name":"numpy","version":"2.1.1","build":"py312_0","channel":"conda-forge"}]`
	if adapter.Check(ctx, check, tool, mc) {
		t.Fatal("version drift must not satisfy conda check")
	}
}

func TestCondaBuildRequiresVersionAtRuntime(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "numpy", "build": "py312_0"}}
	fr := &run.FakeRunner{}
	if err := NewCondaAdapter().Install(context.Background(), fr, &config.Tool{Name: "numpy"}, mc); err == nil {
		t.Fatal("build without version should fail")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("invalid conda intent reached runner: %+v", fr.Calls)
	}
}

func TestCondaInstalledVersionUsesDeclaredTarget(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "conda", Config: map[string]any{"pkg": "numpy", "environment": "data"}}
	fr := &run.FakeRunner{Stdout: `[{"name":"numpy","version":"2.1.0","build":"py312_0"}]`}
	got, err := NewCondaAdapter().InstalledVersion(context.Background(), fr, &config.Tool{Name: "numpy"}, mc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2.1.0" {
		t.Fatalf("InstalledVersion=%q want 2.1.0", got)
	}
	assertCondaCall(t, fr, []string{"list", "--json", "-n", "data", "numpy"})
}

func TestCondaCheckVerifiesDeclaredChannelWhenReported(t *testing.T) {
	mc := &config.MethodCandidate{Kind: "conda", Config: map[string]any{
		"pkg":      "numpy",
		"channels": []string{"conda-forge"},
	}}
	adapter := NewCondaAdapter()
	tool := &config.Tool{Name: "numpy"}

	fr := &run.FakeRunner{Stdout: `[{"name":"numpy","version":"2.1.0","channel":"conda-forge"}]`}
	if !adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("matching declared channel should satisfy check")
	}

	fr = &run.FakeRunner{Stdout: `[{"name":"numpy","version":"2.1.0","channel":"defaults"}]`}
	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("package from undeclared channel must be reported as drift")
	}

	fr = &run.FakeRunner{Stdout: `[{"name":"numpy","version":"2.1.0","base_url":"https://conda.anaconda.org/conda-forge"}]`}
	if !adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("canonical channel URL should match named channel")
	}
}
