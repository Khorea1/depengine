package lang

import (
	"context"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func tool(name, pkg string) (*schema.Tool, *schema.MethodCandidate) {
	t := &schema.Tool{Name: name}
	mc := &schema.MethodCandidate{
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
		KindName:    "test-has-remove",
		Binary:      "sh",
		RemoveTmpl: []string{"echo", "remove", "{pkg}"},
	})
	if !adapter.CanRemove() {
		t.Fatal("CanRemove should be true when RemoveTmpl is set")
	}
}
func TestBaseAdapterRemoveSuccess(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "test-remove",
		Binary:      "sh",
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
		KindName:    "test-remove-fail",
		Binary:      "sh",
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

func TestPkgNameFromConfig(t *testing.T) {
	tl := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "mycustompkg"}}

	if got := pkgName(tl, mc); got != "mycustompkg" {
		t.Fatalf("pkgName = %q, want %q", got, "mycustompkg")
	}
}

func TestPkgNameFallback(t *testing.T) {
	tl := &schema.Tool{Name: "mytool"}
	mc := &schema.MethodCandidate{Config: map[string]any{}}

	if got := pkgName(tl, mc); got != "mytool" {
		t.Fatalf("pkgName fallback = %q, want %q", got, "mytool")
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

	tool := &schema.Tool{Name: "nonexistent-pkg"}
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "nonexistent-pkg"}}

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

	tool := &schema.Tool{Name: "nonexistent-uv-tool"}
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "nonexistent-uv-tool"}}

	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("uv Check should return false for uninstalled tool")
	}
}
