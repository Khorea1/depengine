package ecosystem

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// goFixturePath mirrors the adapter's install target: `go install` produces
// name.exe on Windows. Fixtures must carry the suffix or Remove (correctly)
// deletes a different path.
func goFixturePath(binDir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(binDir, name)
}

func TestGoBinaryName(t *testing.T) {
	cases := []struct {
		importPath string
		want       string
	}{
		// Plain last-element import paths.
		{"github.com/junegunn/fzf", "fzf"},
		{"k8s.io/kubectl", "kubectl"},
		{"example.com/foo/bar", "bar"},
		{"simple", "simple"},
		// Multi-command repo layout: the binary is the element after /cmd/
		// (which is also the last element of the import path).
		{"golang.org/x/tools/cmd/stringer", "stringer"},
		{"golang.org/x/tools/cmd/goimports", "goimports"},
		{"github.com/foo/bar/cmd/baz", "baz"},
		// Slop tolerance.
		{"", ""},
		{"/leading/trailing/", "trailing"},
	}
	for _, tc := range cases {
		if got := goBinaryName(tc.importPath); got != tc.want {
			t.Errorf("goBinaryName(%q) = %q, want %q", tc.importPath, got, tc.want)
		}
	}
}

func TestGoAdapterCanRemove(t *testing.T) {
	if !NewGoAdapter().CanRemove() {
		t.Fatal("GoAdapter.CanRemove() should be true — go removal is supported via binary deletion")
	}
}

func TestGoAdapterRemoveDeletesBinaryFromGOBIN(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	// Explicit pkg config points at the import path; the binary that
	// `go install` produced is named after the /cmd/ element.
	binPath := goFixturePath(binDir, "stringer")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after Remove (err=%v)", binPath, err)
	}
}

func TestGoAdapterRemoveFallsBackToToolNameAsImportPath(t *testing.T) {
	// `go = true` stores an empty pkg config; the import path then comes
	// from tool.Name (which in the schema is the import path itself).
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	binPath := goFixturePath(binDir, "fzf")
	if err := os.WriteFile(binPath, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "github.com/junegunn/fzf"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": ""}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after Remove (err=%v)", binPath, err)
	}
}

func TestGoAdapterRemoveUsesGOPATHBinWhenGOBINUnset(t *testing.T) {
	gopath := t.TempDir()
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", gopath)
	binPath := goFixturePath(filepath.Join(gopath, "bin"), "fzf")
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "fzf"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "github.com/junegunn/fzf"}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Fatalf("binary %s still present after Remove (err=%v)", binPath, err)
	}
}

func TestGoAdapterRemoveMissingBinaryIsIdempotent(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)

	a := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	// Binary was never installed (or already removed) — must not error.
	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove of missing binary should be a no-op, got %v", err)
	}
}

func TestGoAdapterRemoveRejectsUnresolvableImportPath(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)

	a := NewGoAdapter()
	tool := &config.Tool{Name: ""}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": ""}}

	if err := a.Remove(context.Background(), nil, tool, mc); err == nil {
		t.Fatal("Remove with empty import path should fail")
	}
}

func TestGoAdapterRemoveDoesNotTouchUnrelatedBinaries(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	unrelated := filepath.Join(binDir, "cargo")
	if err := os.WriteFile(unrelated, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	a := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	if err := a.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated binary %s was affected by Remove: %v", unrelated, err)
	}
}

func TestGoPkgFieldControlsInstallAndCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.com/project/cmd/realbin"}}

	installRunner := &run.FakeRunner{LookPaths: map[string]bool{"go": true}}
	if err := adapter.Install(ctx, installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertLastGoCall(t, installRunner, []string{"install", "example.com/project/cmd/realbin@latest"})

	checkRunner := &run.FakeRunner{LookPaths: map[string]bool{"go": true, "realbin": true}}
	if !adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should look for the binary derived from go.pkg")
	}
	assertGoLookup(t, checkRunner, "realbin")
}

func TestGoInstalledVersionUsesPkgField(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	adapter := NewGoAdapter()
	fr := &run.FakeRunner{Stdout: strings.Join([]string{
		goFixturePath(binDir, "realbin") + ": go1.27.1",
		"\tpath\texample.com/project/cmd/realbin",
		"\tmod\texample.com/project\tv1.2.3\th1:fixture",
		"",
	}, "\n")}
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.com/project/cmd/realbin"}}

	got, err := adapter.InstalledVersion(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if got != "v1.2.3" {
		t.Fatalf("InstalledVersion = %q, want %q", got, "v1.2.3")
	}
	assertLastGoCall(t, fr, []string{"version", "-m", goFixturePath(binDir, "realbin")})
}

func TestGoModuleVersionFromBuildInfo(t *testing.T) {
	t.Parallel()
	stdout := []byte("/tmp/realbin: go1.27.1\n\tpath\texample.com/project/cmd/realbin\n\tmod\texample.com/project\tv1.2.3\th1:fixture\n")
	got, err := goModuleVersionFromBuildInfo(stdout, "example.com/project/cmd/realbin")
	if err != nil {
		t.Fatalf("goModuleVersionFromBuildInfo() error = %v", err)
	}
	if got != "v1.2.3" {
		t.Fatalf("goModuleVersionFromBuildInfo() = %q, want %q", got, "v1.2.3")
	}
}

func TestGoModuleVersionFromBuildInfoRejectsWrongPackage(t *testing.T) {
	t.Parallel()
	stdout := []byte("/tmp/realbin: go1.27.1\n\tpath\texample.com/other/cmd/realbin\n\tmod\texample.com/other\tv9.9.9\th1:fixture\n")
	if _, err := goModuleVersionFromBuildInfo(stdout, "example.com/project/cmd/realbin"); err == nil {
		t.Fatal("goModuleVersionFromBuildInfo() should reject mismatched package identity")
	}
}

func TestGoPkgFieldControlsRemoveTarget(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	realBin := goFixturePath(binDir, "realbin")
	friendlyBin := goFixturePath(binDir, "friendly-name")
	if err := os.WriteFile(realBin, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(friendlyBin, []byte("keep"), 0755); err != nil {
		t.Fatal(err)
	}

	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.com/project/cmd/realbin"}}
	if err := adapter.Remove(context.Background(), nil, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(realBin); !os.IsNotExist(err) {
		t.Fatalf("pkg-derived binary %s still present after Remove (err=%v)", realBin, err)
	}
	if _, err := os.Stat(friendlyBin); err != nil {
		t.Fatalf("tool-name binary %s should not be removed: %v", friendlyBin, err)
	}
}

func assertLastGoCall(t *testing.T, fr *run.FakeRunner, want []string) {
	t.Helper()
	for i := len(fr.Calls) - 1; i >= 0; i-- {
		if fr.Calls[i].Name == "go" || fr.Calls[i].Name == "realbin" {
			if !reflect.DeepEqual(fr.Calls[i].Args, want) {
				t.Fatalf("%s argv = %#v, want %#v", fr.Calls[i].Name, fr.Calls[i].Args, want)
			}
			return
		}
	}
	t.Fatal("no go-related invocation recorded")
}

func assertGoLookup(t *testing.T, fr *run.FakeRunner, want string) {
	t.Helper()
	for _, call := range fr.Calls {
		if call.Name == "which" && len(call.Args) == 1 && call.Args[0] == want {
			return
		}
	}
	t.Fatalf("no lookup recorded for %q; calls=%#v", want, fr.Calls)
}

func TestGoVersionControlsInstallAndCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{
		"pkg": "example.com/project/cmd/realbin", "version": "v1.2.3",
	}}

	installRunner := &run.FakeRunner{LookPaths: map[string]bool{"go": true}}
	if err := adapter.Install(ctx, installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertLastGoCall(t, installRunner, []string{"install", "example.com/project/cmd/realbin@v1.2.3"})

	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	checkRunner := &run.FakeRunner{
		LookPaths: map[string]bool{"go": true, "realbin": true},
		Stdout: strings.Join([]string{
			goFixturePath(binDir, "realbin") + ": go1.27.1",
			"\tpath\texample.com/project/cmd/realbin",
			"\tmod\texample.com/project\tv1.2.3\th1:fixture",
			"",
		}, "\n"),
	}
	if !adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should accept the requested Go tool version when discoverable")
	}
	checkRunner.Stdout = strings.Replace(checkRunner.Stdout, "v1.2.3", "v1.2.4", 1)
	if adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should reject a different installed Go tool version")
	}
}

func TestGoObservePreservesExactVersionDrift(t *testing.T) {
	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	method := &config.MethodCandidate{Kind: "go", Config: map[string]any{
		"pkg": "example.com/project/cmd/realbin", "version": "v1.2.3",
	}}
	binDir := t.TempDir()
	t.Setenv("GOBIN", binDir)
	runner := &run.FakeRunner{
		LookPaths: map[string]bool{"go": true, "realbin": true},
		Stdout: strings.Join([]string{
			goFixturePath(binDir, "realbin") + ": go1.27.1",
			"\tpath\texample.com/project/cmd/realbin",
			"\tmod\texample.com/project\tv1.2.4\th1:fixture",
			"",
		}, "\n"),
	}

	observation, err := adapter.Observe(context.Background(), runner, tool, method)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observation.Presence != plan.PresencePresent || observation.Identity.Version != "1.2.4" {
		t.Fatalf("observation = %+v, want present installed version 1.2.4", observation)
	}
	desired := plan.ResolvedIdentity{Package: "example.com/project/cmd/realbin", Version: "v1.2.3"}
	verification := plan.Reconcile(desired, observation)
	if verification.State != plan.StateDrifted || len(verification.Drift) != 1 || verification.Drift[0].Field != plan.FieldVersion {
		t.Fatalf("verification = %+v, want exact-version drift", verification)
	}
}

func TestGoLegacyPkgVersionSuffixIsNotDoubleSuffixed(t *testing.T) {
	adapter := NewGoAdapter()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"go": true}}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{"pkg": "example.com/project/cmd/realbin@v1.2.3"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "realbin"}, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertLastGoCall(t, fr, []string{"install", "example.com/project/cmd/realbin@v1.2.3"})
	if got := goBinaryName("example.com/project/cmd/realbin@v1.2.3"); got != "realbin" {
		t.Fatalf("goBinaryName versioned pkg = %q, want realbin", got)
	}
}

func TestGoRejectsPkgVersionSuffixAndVersionFieldTogether(t *testing.T) {
	adapter := NewGoAdapter()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"go": true}}
	mc := &config.MethodCandidate{Kind: "go", Config: map[string]any{
		"pkg": "example.com/project/cmd/realbin@v1.2.3", "version": "v1.2.4",
	}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "realbin"}, mc); err == nil {
		t.Fatal("Install should reject duplicate Go version declarations")
	}
}
