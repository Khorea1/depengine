package exec

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

// lookupWinAdapter finds a built-in Windows adapter by kind.
func lookupWinAdapter(kind string) *winAdapter {
	for _, adapter := range WindowsAdapters() {
		if adapter.Kind() == kind {
			return adapter.(*winAdapter)
		}
	}
	panic(fmt.Sprintf("unknown Windows adapter %q", kind))
}

func TestWinAdapterKind(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{"scoop", "scoop"},
		{"choco", "choco"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			a := lookupWinAdapter(tc.kind)
			if a.Kind() != tc.want {
				t.Fatalf("Kind() = %q, want %q", a.Kind(), tc.want)
			}
		})
	}
}

func TestWinAdapterAvailable(t *testing.T) {
	ctx := context.Background()

	t.Run("binary found on PATH", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		if !a.Available(ctx, fr) {
			t.Fatal("Available() should be true when executable lookup succeeds")
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "which" {
			t.Fatalf("expected LookPath, got %q", fr.Calls[0].Name)
		}
		if len(fr.Calls[0].Args) != 1 || fr.Calls[0].Args[0] != "scoop" {
			t.Fatalf("expected LookPath for scoop, got %v", fr.Calls[0].Args)
		}
	})

	t.Run("binary not on PATH", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		a := lookupWinAdapter("choco")
		if a.Available(ctx, fr) {
			t.Fatal("Available() should be false when executable lookup fails")
		}
	})

	t.Run("different binary per manager", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		if !a.Available(ctx, fr) {
			t.Fatal("Available() should be true for choco")
		}
		if len(fr.Calls) != 1 || fr.Calls[0].Args[0] != "choco" {
			t.Fatalf("expected LookPath for choco, got %v", fr.Calls[0].Args)
		}
	})
}

func TestWinAdapterCheck(t *testing.T) {
	ctx := context.Background()
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "fd"}}
	tool := &config.Tool{Name: "fd"}

	t.Run("scoop installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "fd 10.2.0 main 2026-01-01 10:00:00\n"}
		a := lookupWinAdapter("scoop")
		if !a.Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should be true when scoop list reports the package")
		}
	})

	t.Run("scoop not installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		a := lookupWinAdapter("scoop")
		if a.Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should be false with exit non-zero")
		}
	})

	t.Run("scoop check command uses package name", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "fd 10.2.0 main 2026-01-01 10:00:00\n"}
		a := lookupWinAdapter("scoop")
		a.Check(ctx, fr, tool, mc)
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "scoop" {
			t.Fatalf("expected 'scoop', got %q", fr.Calls[0].Name)
		}
		if len(fr.Calls[0].Args) < 2 || fr.Calls[0].Args[0] != "list" || fr.Calls[0].Args[1] != "fd" {
			t.Fatalf("expected scoop list fd, got %v", fr.Calls[0].Args)
		}
	})

	t.Run("choco installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "fd|1.0.0\n"}
		a := lookupWinAdapter("choco")
		if !a.Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should be true with exit 0")
		}
	})

	t.Run("choco check command structure", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "fd|1.0.0\n"}
		a := lookupWinAdapter("choco")
		a.Check(ctx, fr, tool, mc)
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		want := []string{"list", "--local-only", "--exact", "--limit-output", "fd"}
		if fr.Calls[0].Name != "choco" || fmt.Sprint(fr.Calls[0].Args) != fmt.Sprint(want) {
			t.Fatalf("unexpected choco check call: %s %v", fr.Calls[0].Name, fr.Calls[0].Args)
		}
	})

	t.Run("uses tool name when no pkg in config", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		a.Check(ctx, fr, tool, &config.MethodCandidate{Config: map[string]any{}})
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		// pkg falls back to tool.Name ("fd")
		if len(fr.Calls[0].Args) < 2 || fr.Calls[0].Args[1] != "fd" {
			t.Fatalf("expected package name 'fd' (from tool.Name), got %v", fr.Calls[0].Args)
		}
	})
}

func TestWinAdapterInstall(t *testing.T) {
	ctx := context.Background()
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "fd"}}
	tool := &config.Tool{Name: "fd"}

	t.Run("scoop install succeeds", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		if err := a.Install(ctx, fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("scoop install command uses package name", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		_ = a.Install(ctx, fr, tool, mc)
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "scoop" {
			t.Fatalf("expected 'scoop', got %q", fr.Calls[0].Name)
		}
		if len(fr.Calls[0].Args) < 2 || fr.Calls[0].Args[0] != "install" || fr.Calls[0].Args[1] != "fd" {
			t.Fatalf("expected scoop install fd, got %v", fr.Calls[0].Args)
		}
	})

	t.Run("choco install succeeds", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		if err := a.Install(ctx, fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("choco install command includes -y", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		_ = a.Install(ctx, fr, tool, mc)
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		hasY := false
		for _, arg := range fr.Calls[0].Args {
			if arg == "-y" {
				hasY = true
				break
			}
		}
		if !hasY {
			t.Fatalf("expected choco install to include -y flag, got %v", fr.Calls[0].Args)
		}
	})

	t.Run("runner error returns wrapped error", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Err: fmt.Errorf("exec not found")}
		a := lookupWinAdapter("scoop")
		err := a.Install(ctx, fr, tool, mc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("non-zero exit returns error with stderr", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1, Stderr: "package not found"}
		a := lookupWinAdapter("scoop")
		err := a.Install(ctx, fr, tool, mc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("uses tool name when no pkg in config", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		toolNoConfig := &config.Tool{Name: "neovim"}
		if err := a.Install(ctx, fr, toolNoConfig, &config.MethodCandidate{Config: map[string]any{}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if len(fr.Calls[0].Args) < 2 || fr.Calls[0].Args[1] != "neovim" {
			t.Fatalf("expected package name 'neovim' (from tool.Name), got %v", fr.Calls[0].Args)
		}
	})
}

func TestChocoPrereleaseArgv(t *testing.T) {
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "neovim", "prerelease": true}}
	if err := lookupWinAdapter("choco").Install(context.Background(), fr, &config.Tool{Name: "nvim"}, mc); err != nil {
		t.Fatal(err)
	}
	call := fr.Calls[0]
	want := []string{"install", "neovim", "--pre", "-y"}
	if fmt.Sprint(call.Args) != fmt.Sprint(want) {
		t.Fatalf("argv=%v want=%v", call.Args, want)
	}
}

func TestWinAdapterRemove(t *testing.T) {
	ctx := context.Background()
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "fd"}}
	tool := &config.Tool{Name: "fd"}

	t.Run("scoop remove succeeds", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		if err := a.Remove(ctx, fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("scoop remove command uses package name", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		_ = a.Remove(ctx, fr, tool, mc)
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "scoop" {
			t.Fatalf("expected 'scoop', got %q", fr.Calls[0].Name)
		}
		if len(fr.Calls[0].Args) < 2 || fr.Calls[0].Args[0] != "uninstall" || fr.Calls[0].Args[1] != "fd" {
			t.Fatalf("expected scoop uninstall fd, got %v", fr.Calls[0].Args)
		}
	})

	t.Run("choco remove succeeds", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		if err := a.Remove(ctx, fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("remove error on non-zero exit", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1, Stderr: "permission denied"}
		a := lookupWinAdapter("scoop")
		err := a.Remove(ctx, fr, tool, mc)
		if err == nil {
			t.Fatal("expected error on non-zero exit, got nil")
		}
	})
}

func TestWinAdapterCanRemove(t *testing.T) {
	t.Run("scoop can remove", func(t *testing.T) {
		a := lookupWinAdapter("scoop")
		if !a.CanRemove() {
			t.Fatal("CanRemove() should be true for scoop")
		}
	})

	t.Run("choco can remove", func(t *testing.T) {
		a := lookupWinAdapter("choco")
		if !a.CanRemove() {
			t.Fatal("CanRemove() should be true for choco")
		}
	})
}

func TestWinAdapterImplementsRemover(t *testing.T) {
	a := lookupWinAdapter("scoop")
	if _, ok := any(a).(AdapterV2); !ok {
		t.Fatal("winAdapter must implement AdapterV2")
	}

	b := lookupWinAdapter("choco")
	if _, ok := any(b).(AdapterV2); !ok {
		t.Fatal("winAdapter must implement AdapterV2")
	}
}

func TestWinAdapterInstallResolvedUsesResolvedIdentity(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		config     map[string]any
		mutate     map[string]any
		wantBinary string
		wantArgs   []string
	}{
		{
			name: "scoop",
			kind: "scoop",
			config: map[string]any{
				"pkg": "neovim", "version": "0.10.4", "bucket": "extras",
				"scope": "global", "architecture": "arm64",
			},
			mutate: map[string]any{
				"pkg": "wrong", "version": "9.9.9", "bucket": "wrong-bucket",
				"scope": "user", "architecture": "32bit",
			},
			wantBinary: "scoop",
			wantArgs:   []string{"install", "extras/neovim@0.10.4", "--global", "--arch", "arm64"},
		},
		{
			name: "choco",
			kind: "choco",
			config: map[string]any{
				"pkg": "neovim", "version": "0.10.4", "source": "internal",
				"architecture": "x86", "prerelease": true,
			},
			mutate: map[string]any{
				"pkg": "wrong", "version": "9.9.9", "source": "wrong-source",
				"architecture": "x64",
			},
			wantBinary: "choco",
			wantArgs:   []string{"install", "neovim", "--version", "0.10.4", "--source", "internal", "--forcex86", "--pre", "-y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := lookupWinAdapter(tt.kind)
			tool := &config.Tool{Name: "nvim"}
			mc := &config.MethodCandidate{Kind: tt.kind, Config: tt.config}
			intent, err := planner.BuildCandidateIntent(tool, mc)
			if err != nil {
				t.Fatalf("BuildCandidateIntent() error = %v", err)
			}
			resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
			if err != nil {
				t.Fatalf("ResolvePlan() error = %v", err)
			}
			for key, value := range tt.mutate {
				mc.Config[key] = value
			}

			runner := &run.FakeRunner{}
			if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
				t.Fatalf("InstallResolved() error = %v", err)
			}
			if len(runner.Calls) != 1 {
				t.Fatalf("InstallResolved() calls = %#v, want one call", runner.Calls)
			}
			if got := runner.Calls[0]; got.Name != tt.wantBinary || fmt.Sprint(got.Args) != fmt.Sprint(tt.wantArgs) {
				t.Fatalf("InstallResolved() call = %s %v, want %s %v", got.Name, got.Args, tt.wantBinary, tt.wantArgs)
			}
		})
	}
}

func TestChocoExactVersion(t *testing.T) {
	ctx := context.Background()
	tool := &config.Tool{Name: "nvim"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "neovim", "version": "0.10.4"}}

	t.Run("install pins version", func(t *testing.T) {
		fr := &run.FakeRunner{}
		if err := lookupWinAdapter("choco").Install(ctx, fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		want := []string{"install", "neovim", "--version", "0.10.4", "-y"}
		if got := fr.Calls[0].Args; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("argv=%v want=%v", got, want)
		}
	})

	t.Run("check requires exact installed version", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "neovim|0.10.4\n"}
		if !lookupWinAdapter("choco").Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should accept matching exact version")
		}
		fr.Stdout = "neovim|0.10.3\n"
		if lookupWinAdapter("choco").Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should reject a different installed version")
		}
	})
}

func TestChocoInstalledVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: "neovim|0.10.4\r\n"}
	versioner := lookupWinAdapter("choco")
	got, err := versioner.InstalledVersion(context.Background(), fr, &config.Tool{Name: "nvim"}, &config.MethodCandidate{Config: map[string]any{"pkg": "neovim"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.10.4" {
		t.Fatalf("InstalledVersion=%q want 0.10.4", got)
	}
}

func TestChocoSourceAndArchitectureArgv(t *testing.T) {
	ctx := context.Background()
	tool := &config.Tool{Name: "nvim"}

	t.Run("source is structured argv", func(t *testing.T) {
		fr := &run.FakeRunner{}
		mc := &config.MethodCandidate{Config: map[string]any{
			"pkg":    "neovim",
			"source": "https://packages.example.test/api/v2/",
		}}
		if err := lookupWinAdapter("choco").Install(ctx, fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		want := []string{"install", "neovim", "--source", "https://packages.example.test/api/v2/", "-y"}
		if got := fr.Calls[0].Args; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("argv=%v want=%v", got, want)
		}
	})

	t.Run("x86 uses forcex86", func(t *testing.T) {
		fr := &run.FakeRunner{}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "neovim", "architecture": "x86"}}
		if err := lookupWinAdapter("choco").Install(ctx, fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		want := []string{"install", "neovim", "--forcex86", "-y"}
		if got := fr.Calls[0].Args; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("argv=%v want=%v", got, want)
		}
	})

	t.Run("x64 uses native architecture without unsafe override", func(t *testing.T) {
		fr := &run.FakeRunner{}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "neovim", "architecture": "x64"}}
		if err := lookupWinAdapter("choco").Install(ctx, fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		want := []string{"install", "neovim", "-y"}
		if got := fr.Calls[0].Args; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("argv=%v want=%v", got, want)
		}
	})
}

func TestScoopTypedIdentityAndDesiredState(t *testing.T) {
	a := lookupWinAdapter("scoop")
	mc := &config.MethodCandidate{Config: map[string]any{
		"pkg": "git", "version": "2.53.0.2", "bucket": "main", "scope": "global", "architecture": "64bit",
	}}
	tool := &config.Tool{Name: "git"}

	install := &run.FakeRunner{}
	if err := a.Install(context.Background(), install, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(install.Calls) != 1 || install.Calls[0].Name != "scoop" {
		t.Fatalf("install calls = %#v", install.Calls)
	}
	wantInstall := []string{"install", "main/git@2.53.0.2", "--global", "--arch", "64bit"}
	if !slices.Equal(install.Calls[0].Args, wantInstall) {
		t.Fatalf("install args = %v, want %v", install.Calls[0].Args, wantInstall)
	}

	check := &run.FakeRunner{Stdout: "git 2.53.0.2 main 2026-03-26 11:58:42\n"}
	if !a.Check(context.Background(), check, tool, mc) {
		t.Fatal("Check should accept matching version/source")
	}
	if !slices.Equal(check.Calls[0].Args, []string{"list", "git", "--global"}) {
		t.Fatalf("check args = %v", check.Calls[0].Args)
	}

	drift := &run.FakeRunner{Stdout: "git 2.52.0 main 2026-03-26 11:58:42\n"}
	if a.Check(context.Background(), drift, tool, mc) {
		t.Fatal("Check should reject version drift")
	}

	sourceDrift := &run.FakeRunner{Stdout: "git 2.53.0.2 extras 2026-03-26 11:58:42\n"}
	if a.Check(context.Background(), sourceDrift, tool, mc) {
		t.Fatal("Check should reject bucket drift")
	}

	remove := &run.FakeRunner{}
	if err := a.Remove(context.Background(), remove, tool, mc); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !slices.Equal(remove.Calls[0].Args, []string{"uninstall", "git", "--global"}) {
		t.Fatalf("remove args = %v", remove.Calls[0].Args)
	}
}

func TestScoopInstalledVersion(t *testing.T) {
	a := lookupWinAdapter("scoop")
	v, err := a.InstalledVersion(context.Background(), &run.FakeRunner{Stdout: "fd 10.2.0 main 2026-01-01 10:00:00\n"}, &config.Tool{Name: "fd"}, &config.MethodCandidate{Config: map[string]any{"pkg": "fd"}})
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if v != "10.2.0" {
		t.Fatalf("InstalledVersion = %q, want 10.2.0", v)
	}
}
