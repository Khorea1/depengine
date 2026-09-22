package ecosystem

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
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

func TestCargoVersionControlsInstallAndCheck(t *testing.T) {
	ctx := context.Background()
	adapter := NewCargoAdapter()
	tool := &config.Tool{Name: "friendly-name"}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate-name", "version": "1.2.3"}}

	installRunner := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
	if err := adapter.Install(ctx, installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	assertLastCargoCall(t, installRunner, []string{"install", "--version", "1.2.3", "crate-name"})

	checkRunner := &run.FakeRunner{Stdout: "crate-name v1.2.3:\n    crate-name\n"}
	if !adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should accept the requested cargo version")
	}
	checkRunner.Stdout = "crate-name v1.2.4:\n    crate-name\n"
	if adapter.Check(ctx, checkRunner, tool, mc) {
		t.Fatal("Check should reject a different installed cargo version")
	}
}

func TestCargoRejectsGitAndVersionTogetherAtRuntime(t *testing.T) {
	adapter := NewCargoAdapter()
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"pkg": "crate-name", "git": "https://example.invalid/repo.git", "version": "1.2.3",
	}}
	if err := adapter.Install(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "crate-name"}, mc); err == nil {
		t.Fatal("Install should reject cargo.git with cargo.version")
	}
}

func TestCargoRegistryControlsInstallSource(t *testing.T) {
	adapter := NewCargoAdapter()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"pkg":      "crate-name",
		"registry": "corp",
		"version":  "1.2.3",
	}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "friendly"}, mc); err != nil {
		t.Fatal(err)
	}
	assertLastCargoCall(t, fr, []string{"install", "--registry", "corp", "--version", "1.2.3", "crate-name"})
}

func TestCargoGitRevisionSelection(t *testing.T) {
	for _, tc := range []struct {
		name, field, value, flag string
	}{
		{name: "branch", field: "branch", value: "next", flag: "--branch"},
		{name: "tag", field: "tag", value: "v1.2.3", flag: "--tag"},
		{name: "rev", field: "rev", value: "0123456789abcdef", flag: "--rev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := NewCargoAdapter()
			fr := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
			mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
				"git":    "https://example.invalid/repo.git",
				tc.field: tc.value,
			}}
			if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "friendly"}, mc); err != nil {
				t.Fatal(err)
			}
			assertLastCargoCall(t, fr, []string{"install", "--git", "https://example.invalid/repo.git", tc.flag, tc.value})
		})
	}
}

func TestCargoRevisionRequiresGitAtRuntime(t *testing.T) {
	adapter := NewCargoAdapter()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{"pkg": "crate", "rev": "deadbeef"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "crate"}, mc); err == nil {
		t.Fatal("Install should reject cargo.rev without cargo.git")
	}
}

func TestCargoRejectsMultipleGitRefsAtRuntime(t *testing.T) {
	adapter := NewCargoAdapter()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"git": "https://example.invalid/repo.git", "tag": "v1", "rev": "deadbeef",
	}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "crate"}, mc); err == nil {
		t.Fatal("Install should reject multiple cargo git refs")
	}
}

func TestCargoTypedBuildAndTargetOptions(t *testing.T) {
	adapter := NewCargoAdapter()
	fr := &run.FakeRunner{LookPaths: map[string]bool{"cargo": true}}
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"pkg":                 "crate-name",
		"features":            []string{"tls", "json"},
		"no_default_features": true,
		"bins":                []string{"crate-cli", "crate-admin"},
		"target":              "x86_64-unknown-linux-musl",
		"root":                "~/.local/cargo-tools",
	}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "friendly"}, mc); err != nil {
		t.Fatal(err)
	}
	root := config.ExpandHomeDir("~/.local/cargo-tools")
	assertLastCargoCall(t, fr, []string{
		"install",
		"--features", "tls,json",
		"--no-default-features",
		"--bin", "crate-cli",
		"--bin", "crate-admin",
		"--target", "x86_64-unknown-linux-musl",
		"--root", root,
		"crate-name",
	})
}

func TestCargoRootAndBinsParticipateInCheckAndRemove(t *testing.T) {
	adapter := NewCargoAdapter()
	root := config.ExpandHomeDir("~/.local/cargo-tools")
	mc := &config.MethodCandidate{Kind: "cargo", Config: map[string]any{
		"pkg":  "crate-name",
		"root": "~/.local/cargo-tools",
		"bins": []string{"crate-cli"},
	}}
	tool := &config.Tool{Name: "friendly"}

	check := &run.FakeRunner{Stdout: "crate-name v1.2.3:\n    crate-cli\n    other-bin\n"}
	if !adapter.Check(context.Background(), check, tool, mc) {
		t.Fatal("expected declared bin in declared root to satisfy check")
	}
	assertLastCargoCall(t, check, []string{"install", "--list", "--root", root})

	check.Stdout = "crate-name v1.2.3:\n    other-bin\n"
	if adapter.Check(context.Background(), check, tool, mc) {
		t.Fatal("missing selected bin must be reported as drift")
	}

	remove := &run.FakeRunner{}
	if err := adapter.Remove(context.Background(), remove, tool, mc); err != nil {
		t.Fatal(err)
	}
	assertLastCargoCall(t, remove, []string{"uninstall", "--root", root, "crate-name"})
}
