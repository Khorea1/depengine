package ecosystem

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestCargoPkgFieldControlsInstallCheckAndRemove(t *testing.T) {
	ctx := context.Background()
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "cargo-package"}}

	installRunner := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
	if err := adapter.Install(ctx, installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertLastCargoCall(t, installRunner, []string{"install", "cargo-package"})

	checkRunner := &run.FakeRunner{
		LookPaths: map[string]bool{"cargo": true},
		Stdout:    "cargo-package v1.2.3:\n    cargo-package\n",
	}
	if !adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should match the package selected by cargo.pkg")
	}
	assertLastCargoCall(t, checkRunner, []string{"install", "--list"})

	removeRunner := &run.FakeRunner{}
	if err := adapter.Remove(ctx, removeRunner, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	assertLastCargoCall(t, removeRunner, []string{"uninstall", "cargo-package"})
}

func TestCargoGitFieldControlsInstallSource(t *testing.T) {
	ctx := context.Background()
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "tool-name"}
	mc := &config.MethodCandidate{
		Kind: "cargo",
		Config: map[string]any{
			"pkg": "crate-name",
			"git": "https://example.invalid/project.git",
		},
	}
	fr := &run.FakeRunner{}

	if err := adapter.Install(ctx, fr, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertLastCargoCall(t, fr, []string{"install", "--git", "https://example.invalid/project.git", "crate-name"})
}

func TestCargoGitFieldFallsBackToToolNameWhenPkgOmitted(t *testing.T) {
	ctx := context.Background()
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "crate-name"}
	mc := &config.MethodCandidate{
		Kind:   "cargo",
		Config: map[string]any{"git": "https://example.invalid/project.git"},
	}
	fr := &run.FakeRunner{}

	if err := adapter.Install(ctx, fr, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// In git mode an omitted pkg means cargo should select the repository's
	// package itself; do not manufacture a positional package from tool.Name.
	assertLastCargoCall(t, fr, []string{"install", "--git", "https://example.invalid/project.git"})
}

func TestCargoInstalledVersionUsesPkgField(t *testing.T) {
	adapter := NewCargoAdapter()
	fr := &run.FakeRunner{Stdout: "crate-name v0.9.1:\n    crate-name\n"}
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name"}}

	got, err := adapter.InstalledVersion(context.Background(), fr, tool, mc)
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if got != "0.9.1" {
		t.Fatalf("InstalledVersion = %q, want %q", got, "0.9.1")
	}
}

func assertLastCargoCall(t *testing.T, fr *run.FakeRunner, want []string) {
	t.Helper()
	for i := len(fr.Calls) - 1; i >= 0; i-- {
		if fr.Calls[i].Name == "cargo" {
			if !reflect.DeepEqual(fr.Calls[i].Args, want) {
				t.Fatalf("cargo argv = %#v, want %#v", fr.Calls[i].Args, want)
			}
			return
		}
	}
	t.Fatal("no cargo invocation recorded")
}
