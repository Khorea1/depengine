package exec

import (
	"context"
	"fmt"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// lookupWinAdapter finds a winAdapter by kind from the global registry.
func lookupWinAdapter(kind string) *winAdapter {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	a, ok := adapters[kind]
	if !ok {
		panic(fmt.Sprintf("adapter %q not registered", kind))
	}
	wa, ok := a.(*winAdapter)
	if !ok {
		panic(fmt.Sprintf("adapter %q is %T, not *winAdapter", kind, a))
	}
	return wa
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
			t.Fatal("Available() should be true when which exits 0")
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "which" {
			t.Fatalf("expected 'which', got %q", fr.Calls[0].Name)
		}
		if len(fr.Calls[0].Args) != 1 || fr.Calls[0].Args[0] != "scoop" {
			t.Fatalf("expected which scoop, got %v", fr.Calls[0].Args)
		}
	})

	t.Run("binary not on PATH", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		a := lookupWinAdapter("choco")
		if a.Available(ctx, fr) {
			t.Fatal("Available() should be false when which exits non-zero")
		}
	})

	t.Run("different binary per manager", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		if !a.Available(ctx, fr) {
			t.Fatal("Available() should be true for choco")
		}
		if len(fr.Calls) != 1 || fr.Calls[0].Args[0] != "choco" {
			t.Fatalf("expected which choco, got %v", fr.Calls[0].Args)
		}
	})
}

func TestWinAdapterCheck(t *testing.T) {
	ctx := context.Background()
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "fd"}}
	tool := &schema.Tool{Name: "fd"}

	t.Run("scoop installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		if !a.Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should be true with exit 0")
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
		fr := &run.FakeRunner{ExitCode: 0}
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
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		if !a.Check(ctx, fr, tool, mc) {
			t.Fatal("Check() should be true with exit 0")
		}
	})

	t.Run("choco check command structure", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("choco")
		a.Check(ctx, fr, tool, mc)
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "cmd" {
			t.Fatalf("expected 'cmd', got %q", fr.Calls[0].Name)
		}
	})

	t.Run("uses tool name when no pkg in config", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := lookupWinAdapter("scoop")
		a.Check(ctx, fr, tool, &schema.MethodCandidate{Config: map[string]any{}})
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
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "fd"}}
	tool := &schema.Tool{Name: "fd"}

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
		toolNoConfig := &schema.Tool{Name: "neovim"}
		if err := a.Install(ctx, fr, toolNoConfig, &schema.MethodCandidate{Config: map[string]any{}}); err != nil {
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

func TestWinAdapterRemove(t *testing.T) {
	ctx := context.Background()
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "fd"}}
	tool := &schema.Tool{Name: "fd"}

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
	if _, ok := any(a).(Remover); !ok {
		t.Fatal("winAdapter must implement Remover")
	}

	b := lookupWinAdapter("choco")
	if _, ok := any(b).(Remover); !ok {
		t.Fatal("winAdapter must implement Remover")
	}
}
