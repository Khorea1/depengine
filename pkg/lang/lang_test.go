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
