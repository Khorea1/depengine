package exec

import (
	"context"
	"fmt"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

func TestNativeAdapterAutoDetect(t *testing.T) {
	t.Run("finds manager when one exists", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		if !adapter.Available(context.Background(), fr) {
			t.Fatal("Available should be true when manager found")
		}
	})

	t.Run("returns false when no manager", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		adapter := NewNativeAdapter("")
		if adapter.Available(context.Background(), fr) {
			t.Fatal("Available should be false when no manager")
		}
	})

	t.Run("caches clan after detection", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.Available(context.Background(), fr)
		callsBefore := len(fr.Calls)
		adapter.Available(context.Background(), fr)
		if len(fr.Calls) != callsBefore {
			t.Fatal("second Available should not probe again")
		}
	})
}

func TestNativeAdapterCheckWithAutoDetect(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
	tool := &config.Tool{Name: "git"}

	t.Run("installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.detectClan(context.Background(), fr)
		if !adapter.Check(context.Background(), fr, tool, mc) {
			t.Fatal("Check should be true with exit 0")
		}
	})

	t.Run("not installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.detectClan(context.Background(), fr)
		fr.ExitCode = 1
		if adapter.Check(context.Background(), fr, tool, mc) {
			t.Fatal("Check should be false with exit non-zero")
		}
	})

	t.Run("empty pkg returns false", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		if adapter.Check(context.Background(), fr, tool, &config.MethodCandidate{Config: map[string]any{}}) {
			t.Fatal("Check should be false with empty pkg")
		}
	})
}

func TestNativeAdapterInstallWithAutoDetect(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
	tool := &config.Tool{Name: "git"}

	t.Run("success", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.detectClan(context.Background(), fr)
		if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("failure", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.detectClan(context.Background(), fr)
		fr.ExitCode = 1
		fr.Stderr = "E: failed"
		if err := adapter.Install(context.Background(), fr, tool, mc); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("install succeeds with or without sync", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.detectClan(context.Background(), fr)
		fr.Calls = nil

		if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The install command should contain the package name.
		installFound := false
		for _, c := range fr.Calls {
			for _, arg := range c.Args {
				if arg == "git" {
					installFound = true
					break
				}
			}
		}
		if !installFound {
			t.Fatalf("expected install command to contain package name 'git', got calls: %v", fr.Calls)
		}
	})
}

func TestPkgFromConfigResolvesClanOverrides(t *testing.T) {
	t.Run("uses override for matching clan", func(t *testing.T) {
		mc := &config.MethodCandidate{
			Config: map[string]any{
				"pkg":           "fd",
				"pkg_overrides": map[string]any{"apt": "fd-find"},
			},
		}
		if got := pkgFromConfig(mc, "debian"); got != "fd-find" {
			t.Fatalf("on debian expected fd-find, got %q", got)
		}
	})

	t.Run("falls back to default pkg for non-matching clan", func(t *testing.T) {
		mc := &config.MethodCandidate{
			Config: map[string]any{
				"pkg":           "fd",
				"pkg_overrides": map[string]any{"apt": "fd-find"},
			},
		}
		if got := pkgFromConfig(mc, "arch"); got != "fd" {
			t.Fatalf("on arch expected fd (default), got %q", got)
		}
	})

	t.Run("falls back to default pkg when no overrides exist", func(t *testing.T) {
		mc := &config.MethodCandidate{
			Config: map[string]any{"pkg": "fd"},
		}
		if got := pkgFromConfig(mc, "debian"); got != "fd" {
			t.Fatalf("expected fd, got %q", got)
		}
	})

	t.Run("empty clan falls back to pkg key", func(t *testing.T) {
		mc := &config.MethodCandidate{
			Config: map[string]any{"pkg": "git"},
		}
		if got := pkgFromConfig(mc, ""); got != "git" {
			t.Fatalf("expected git, got %q", got)
		}
	})

	t.Run("override keyed by alias resolves for multiple clans", func(t *testing.T) {
		// "apt" is Manager.Name for both debian and mint.
		mc := &config.MethodCandidate{
			Config: map[string]any{
				"pkg":           "tool",
				"pkg_overrides": map[string]any{"apt": "tool-apt"},
			},
		}
		if got := pkgFromConfig(mc, "debian"); got != "tool-apt" {
			t.Fatalf("on debian expected tool-apt, got %q", got)
		}
		if got := pkgFromConfig(mc, "mint"); got != "tool-apt" {
			t.Fatalf("on mint expected tool-apt, got %q", got)
		}
	})
}

func TestFindClanByManagerResolvesBinaryNameVariants(t *testing.T) {
	tests := []struct {
		binary   string
		wantClan string
	}{
		{"apt", "debian"},
		{"emerge", "gentoo"},  // reverse map: binary name differs from Manager.Name
		{"portage", "gentoo"}, // reverse map: backward compat
		{"dnf", "fedora"},
		{"brew", "macos"},
		{"nonexistent", ""},
	}
	for _, tc := range tests {
		t.Run(tc.binary, func(t *testing.T) {
			got := findClanByManager(tc.binary)
			if got != tc.wantClan {
				t.Fatalf("findClanByManager(%q) = %q, want %q", tc.binary, got, tc.wantClan)
			}
		})
	}

	// "pkg" is intentionally absent from managerNameToClan because both
	// termux and freebsd use it. The fallback loop must still resolve it
	// to one of those clans (non-deterministic which one, but both are
	// valid since they share the "pkg" binary).
	t.Run("pkg resolves to termux or freebsd via fallback", func(t *testing.T) {
		got := findClanByManager("pkg")
		if got != "termux" && got != "freebsd" {
			t.Fatalf("findClanByManager(\"pkg\") = %q, want \"termux\" or \"freebsd\"", got)
		}
	})
}

func TestNativeAdapterRemove(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
	tool := &config.Tool{Name: "git"}

	t.Run("runs remove command after detection", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		adapter := NewNativeAdapter("")
		adapter.detectClan(context.Background(), fr)
		fr.Calls = nil
		fr.ExitCode = 0

		if err := adapter.Remove(context.Background(), fr, tool, mc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(fr.Calls) == 0 {
			t.Fatal("expected at least one command call")
		}
		// The remove command should contain the package name.
		removeFound := false
		for _, c := range fr.Calls {
			for _, arg := range c.Args {
				if arg == "git" {
					removeFound = true
					break
				}
			}
		}
		if !removeFound {
			t.Fatalf("expected remove command to contain package name 'git', got calls: %v", fr.Calls)
		}
	})

	t.Run("error on no manager detected", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		adapter := NewNativeAdapter("")

		err := adapter.Remove(context.Background(), fr, tool, mc)
		if err == nil {
			t.Fatal("expected error when no manager detected")
		}
	})
}

func TestNativeByManagerAdapterAvailable(t *testing.T) {
	t.Run("manager found on PATH", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := &NativeByManagerAdapter{managerName: "apt"}

		got := a.Available(context.Background(), fr)
		if !got {
			t.Fatal("expected true when manager is on PATH")
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "which" {
			t.Fatalf("expected 'which', got %q", fr.Calls[0].Name)
		}
	})

	t.Run("manager not found on PATH", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		a := &NativeByManagerAdapter{managerName: "nonexistent-mgr"}

		got := a.Available(context.Background(), fr)
		if got {
			t.Fatal("expected false when manager is not on PATH")
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
	})

	t.Run("runner error returns false", func(t *testing.T) {
		fr := &run.FakeRunner{Err: fmt.Errorf("exec error")}
		a := &NativeByManagerAdapter{managerName: "apt"}

		got := a.Available(context.Background(), fr)
		if got {
			t.Fatal("expected false when runner errors")
		}
	})
}

func TestNativeByManagerAdapterImplementsRemover(t *testing.T) {
	a := &NativeByManagerAdapter{managerName: "apt"}
	if !CanRemove(a) {
		t.Fatal("NativeByManagerAdapter should implement Remover")
	}
	if !a.CanRemove() {
		t.Fatal("NativeByManagerAdapter.CanRemove should return true")
	}
}

func TestNativeByManagerAdapterRemoveDelegates(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	a := &NativeByManagerAdapter{managerName: "sh", rn: fr}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
	tool := &config.Tool{Name: "git"}

	// Remove should not panic or return error — it delegates to NativeAdapter.Remove
	// which uses findClanByManager. Since "sh" isn't a real manager, it will fail
	// at clan resolution, but shouldn't panic or leave resources dangling.
	err := a.Remove(context.Background(), fr, tool, mc)
	if err == nil {
		t.Fatal("expected error for unknown manager 'sh'")
	}
	if err.Error() != "native(sh): no clan found for manager" {
		t.Fatalf("unexpected error: %v", err)
	}
}
func TestNativeByManagerAdapterCheck(t *testing.T) {
	t.Run("installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		got := a.Check(context.Background(), fr, tool, mc)
		if !got {
			t.Fatal("expected true for installed package")
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		if fr.Calls[0].Name != "dpkg" {
			t.Fatalf("expected 'dpkg', got %q", fr.Calls[0].Name)
		}
		wantArgs := []string{"-s", "git"}
		if len(fr.Calls[0].Args) != len(wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, fr.Calls[0].Args)
		}
		for i, arg := range wantArgs {
			if fr.Calls[0].Args[i] != arg {
				t.Fatalf("arg %d: expected %q, got %q", i, arg, fr.Calls[0].Args[i])
			}
		}
	})

	t.Run("not installed", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		got := a.Check(context.Background(), fr, tool, mc)
		if got {
			t.Fatal("expected false for uninstalled package")
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
	})

	t.Run("empty pkg returns false", func(t *testing.T) {
		fr := &run.FakeRunner{}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{}}
		tool := &config.Tool{Name: "git"}

		got := a.Check(context.Background(), fr, tool, mc)
		if got {
			t.Fatal("expected false when pkg is empty")
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("expected 0 calls when pkg is empty, got %d", len(fr.Calls))
		}
	})

	t.Run("unknown manager returns false", func(t *testing.T) {
		fr := &run.FakeRunner{}
		a := &NativeByManagerAdapter{managerName: "nonexistent"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		got := a.Check(context.Background(), fr, tool, mc)
		if got {
			t.Fatal("expected false for unknown manager")
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("expected 0 calls, got %d", len(fr.Calls))
		}
	})
}

func TestNativeByManagerAdapterInstall(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		err := a.Install(context.Background(), fr, tool, mc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fr.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(fr.Calls))
		}
		// apt uses clan "debian" which has Name "apt" and sudo=true.
		// InstallCmd = ["apt-get", "install", "-y", "{pkg}"] → with sudo → ["sudo", "apt-get", "install", "-y", "git"]
		// replaceManagerBinary sees managerName "apt" == nm.Name "apt", so no change.
		if fr.Calls[0].Name != "sudo" {
			t.Fatalf("expected 'sudo', got %q", fr.Calls[0].Name)
		}
		wantArgs := []string{"apt-get", "install", "-y", "git"}
		if len(fr.Calls[0].Args) != len(wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, fr.Calls[0].Args)
		}
		for i, arg := range wantArgs {
			if fr.Calls[0].Args[i] != arg {
				t.Fatalf("arg %d: expected %q, got %q", i, arg, fr.Calls[0].Args[i])
			}
		}
	})

	t.Run("runner error", func(t *testing.T) {
		fr := &run.FakeRunner{Err: fmt.Errorf("test error")}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		err := a.Install(context.Background(), fr, tool, mc)
		if err == nil {
			t.Fatal("expected error from runner failure")
		}
	})

	t.Run("non-zero exit", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 1, Stderr: "permission denied"}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		err := a.Install(context.Background(), fr, tool, mc)
		if err == nil {
			t.Fatal("expected error for non-zero exit")
		}
	})

	t.Run("empty pkg returns error", func(t *testing.T) {
		fr := &run.FakeRunner{}
		a := &NativeByManagerAdapter{managerName: "apt"}
		mc := &config.MethodCandidate{Config: map[string]any{}}
		tool := &config.Tool{Name: "git"}

		err := a.Install(context.Background(), fr, tool, mc)
		if err == nil {
			t.Fatal("expected error when pkg is empty")
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("expected 0 calls when pkg is empty, got %d", len(fr.Calls))
		}
	})

	t.Run("unknown manager returns error", func(t *testing.T) {
		fr := &run.FakeRunner{}
		a := &NativeByManagerAdapter{managerName: "nonexistent"}
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": "git"}}
		tool := &config.Tool{Name: "git"}

		err := a.Install(context.Background(), fr, tool, mc)
		if err == nil {
			t.Fatal("expected error for unknown manager")
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("expected 0 calls, got %d", len(fr.Calls))
		}
	})
}
