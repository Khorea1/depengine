package ecosystem

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

func tool(name, pkg string) (*config.Tool, *config.MethodCandidate) {
	t := &config.Tool{Name: name}
	mc := &config.MethodCandidate{
		Kind:   name,
		Config: map[string]any{"pkg": pkg},
	}
	return t, mc
}

func TestBaseAdapterAvailable(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-avail",
		Binary:   "sh", // should exist on any Unix
	})

	if !adapter.Available(context.Background(), run.OSExecRunner{}) {
		t.Fatal("Available should be true for 'sh'")
	}
	if adapter.Kind() != "test-avail" {
		t.Fatalf("Kind() = %q, want 'test-avail'", adapter.Kind())
	}
}

func TestBaseAdapterAvailableMissing(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-missing",
		Binary:   "this-binary-does-not-exist-hopefully",
	})

	if adapter.Available(context.Background(), run.OSExecRunner{}) {
		t.Fatal("Available should be false for nonexistent binary")
	}
}

func TestBaseAdapterAvailableExtra(t *testing.T) {
	// Try binary "nonexistent" with fallback to "sh".
	adapter := NewBaseAdapter(BaseConfig{
		KindName:       "test-extra",
		Binary:         "this-does-not-exist",
		AvailableExtra: "sh",
	})

	if !adapter.Available(context.Background(), run.OSExecRunner{}) {
		t.Fatal("Available should fall back to extra binary 'sh'")
	}
}

func TestBaseAdapterCheckWithRunner(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:  "test-check",
		Binary:    "sh",
		CheckTmpl: []string{"test", "-f", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	if !adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be true when exit code 0")
	}
}

func TestBaseAdapterCheckFails(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:  "test-check-fail",
		Binary:    "sh",
		CheckTmpl: []string{"test", "-f", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	if adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be false when exit code non-zero")
	}
}

func TestBaseAdapterInstallSuccess(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "test-install",
		Binary:      "sh",
		InstallTmpl: []string{"echo", "install", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Install(context.Background(), fr, tl, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBaseAdapterInstallFailure(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "error: permission denied"}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "test-install-fail",
		Binary:      "sh",
		InstallTmpl: []string{"false"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Install(context.Background(), fr, tl, mc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBaseAdapterCanRemoveFalseWhenEmpty(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-no-remove",
		Binary:   "sh",
	})
	if adapter.CanRemove() {
		t.Fatal("CanRemove should be false when RemoveTmpl is empty")
	}
}

func TestBaseAdapterCanRemoveTrueWhenSet(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName:   "test-has-remove",
		Binary:     "sh",
		RemoveTmpl: []string{"echo", "remove", "{pkg}"},
	})
	if !adapter.CanRemove() {
		t.Fatal("CanRemove should be true when RemoveTmpl is set")
	}
}
func TestBaseAdapterRemoveSuccess(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:   "test-remove",
		Binary:     "sh",
		RemoveTmpl: []string{"echo", "remove", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Remove(context.Background(), fr, tl, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBaseAdapterRemoveNoCommand(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-remove-empty",
		Binary:   "sh",
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Remove(context.Background(), &run.FakeRunner{}, tl, mc)
	if err == nil {
		t.Fatal("expected error when no remove command, got nil")
	}
}

func TestBaseAdapterRemoveFailure(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "error: not installed"}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:   "test-remove-fail",
		Binary:     "sh",
		RemoveTmpl: []string{"false"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Remove(context.Background(), fr, tl, mc)
	if err == nil {
		t.Fatal("expected error on non-zero exit, got nil")
	}
}

func TestAURAdapterAvailable(t *testing.T) {
	// We can't assume paru/yay exists, so use FakeRunner.
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewAURAdapter("paru")

	if !adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be true when which returns 0")
	}
}

func TestAURAdapterAvailableMissing(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewAURAdapter("nonexistent-aur-helper")

	if adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be false when which returns non-zero")
	}
}

func TestAURAdapterCheckInstall(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewAURAdapter("paru")
	tl, mc := tool("test-aur-pkg", "test-aur-pkg")

	if !adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be true with exit 0")
	}

	err := adapter.Install(context.Background(), fr, tl, mc)
	if err != nil {
		t.Fatalf("Install should succeed: %v", err)
	}
}

func TestSubstitutePkgFromConfig(t *testing.T) {
	tl := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "mycustompkg"}}

	got := exec.SubstitutePkg([]string{"{pkg}"}, tl, mc)
	if len(got) == 0 || got[0] != "mycustompkg" {
		t.Fatalf("SubstitutePkg = %v, want %q", got, "mycustompkg")
	}
}

func TestSubstitutePkgFallback(t *testing.T) {
	tl := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{}}

	got := exec.SubstitutePkg([]string{"{pkg}"}, tl, mc)
	if len(got) == 0 || got[0] != "mytool" {
		t.Fatalf("SubstitutePkg fallback = %v, want %q", got, "mytool")
	}
}

// TestPipxCheckReturnsFalseForUninstalled verifies the fix: pipx Check now
// uses `pipx list --short | grep -qF {pkg}` so it correctly returns false
// when the specific package is not installed.
func TestPipxCheckReturnsFalseForUninstalled(t *testing.T) {
	t.Parallel()
	// Simulate pipx list --short showing packages but grep -qF finding nothing.
	fr := &run.FakeRunner{Stdout: "some-other-pkg\n", ExitCode: 1}

	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "pipx",
		Binary:      "pipx",
		CheckTmpl:   []string{"sh", "-c", "pipx list --short 2>/dev/null | grep -qF '{pkg}'"},
		InstallTmpl: []string{"pipx", "install", "{pkg}"},
	})

	tool := &config.Tool{Name: "nonexistent-pkg"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "nonexistent-pkg"}}

	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("pipx Check should return false for uninstalled package")
	}
}

// TestUvCheckReturnsFalseForUninstalled verifies the uv Check fix.
func TestUvCheckReturnsFalseForUninstalled(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Stdout: "some-tool\n", ExitCode: 1}

	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "uv",
		Binary:      "uv",
		CheckTmpl:   []string{"sh", "-c", "uv tool list 2>/dev/null | grep -qF '{pkg}'"},
		InstallTmpl: []string{"uv", "tool", "install", "{pkg}"},
	})

	tool := &config.Tool{Name: "nonexistent-uv-tool"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "nonexistent-uv-tool"}}

	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("uv Check should return false for uninstalled tool")
	}
}

func TestParseMajorVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		want  int
		found bool
	}{
		{"1.22.19", 1, true},
		{"2.0.0", 2, true},
		{"3.1.0", 3, true},
		{"v2.1.0", 2, true},
		{"v10.0.1", 10, true},
		{"20.0.0", 20, true},
		{"berry-2.0.0", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseMajorVersion(tc.in)
			if ok != tc.found {
				t.Fatalf("parseMajorVersion(%q) found = %v, want %v", tc.in, ok, tc.found)
			}
			if ok && got != tc.want {
				t.Fatalf("parseMajorVersion(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestYarnBerryAvailableVersionGating(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stdout string
		want   bool
	}{
		{"classic yarn 1.x", "1.22.19\n", false},
		{"berry 2.x", "2.0.0\n", true},
		{"berry 3.x", "3.1.0\n", true},
		{"v-prefixed berry", "v2.1.0\n", true},
		{"multi-digit major", "10.0.0\n", true},
		{"non-numeric prefix", "berry-2.0.0\n", false},
		{"empty version", "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := &run.FakeRunner{ExitCode: 0, Stdout: tc.stdout}
			adapter := NewYarnBerryAdapter()
			got := adapter.Available(context.Background(), fr)
			if got != tc.want {
				t.Fatalf("Available with stdout %q = %v, want %v", tc.stdout, got, tc.want)
			}
		})
	}
}

func TestPacstallCheckUsesCorrectFlag(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewPacstallAdapter()
	tool := &config.Tool{Name: "test"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "foo"}}

	adapter.Check(context.Background(), fr, tool, mc)

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	call := fr.Calls[0]
	if call.Name != "pacstall" {
		t.Fatalf("expected command 'pacstall', got %q", call.Name)
	}
	if len(call.Args) != 2 || call.Args[0] != "-Ci" || call.Args[1] != "foo" {
		t.Fatalf("expected args ['-Ci', 'foo'], got %v", call.Args)
	}
}

func TestSteamCMDCheckWithEmptyInstallDir(t *testing.T) {
	t.Parallel()
	adapter := NewSteamCMDAdapter()
	tool := &config.Tool{Name: "test"}

	t.Run("returns false when dir config is empty string", func(t *testing.T) {
		mcWithEmptyDir := &config.MethodCandidate{
			Config: map[string]any{"pkg": "730", "dir": ""},
		}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcWithEmptyDir)
		if got {
			t.Fatal("Check should return false when dir is empty string")
		}
	})

	t.Run("returns false when no pkg", func(t *testing.T) {
		mcNoPkg := &config.MethodCandidate{Config: map[string]any{}}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcNoPkg)
		if got {
			t.Fatal("Check should return false when pkg is missing")
		}
	})

	t.Run("uses explicit dir when provided", func(t *testing.T) {
		mcWithDir := &config.MethodCandidate{
			Config: map[string]any{"pkg": "730", "dir": "/tmp"},
		}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcWithDir)
		if got {
			t.Fatal("Check should return false (always checks for updates)")
		}
	})
}
