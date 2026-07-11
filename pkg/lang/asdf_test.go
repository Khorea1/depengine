package lang

import (
	"context"
	"testing"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func asdfTool(name, pkg string) (*schema.Tool, *schema.MethodCandidate) {
	tool := &schema.Tool{Name: name}
	mc := &schema.MethodCandidate{
		Kind:   "asdf",
		Config: map[string]any{"pkg": pkg},
	}
	return tool, mc
}

func TestAsdfAdapterAvailable(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &AsdfAdapter{}
	if !a.Available(context.Background(), fr) {
		t.Fatal("expected Available=true when asdf is found")
	}
}

func TestAsdfAdapterAvailableMissing(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	a := &AsdfAdapter{}
	if a.Available(context.Background(), fr) {
		t.Fatal("expected Available=false when neither asdf nor mise found")
	}
}

func TestAsdfAdapterCheck(t *testing.T) {
	t.Parallel()
	// Both LookPath (which) and asdf list return ExitCode=0,
	// and stdout contains the package name → Check passes.
	fr := &run.FakeRunner{Stdout: "nodejs\n18.0.0\n", ExitCode: 0}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	if !a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=true when tool is installed")
	}
}

func TestAsdfAdapterCheckNotInstalled(t *testing.T) {
	t.Parallel()
	// Stdout doesn't contain the package name → Check fails.
	fr := &run.FakeRunner{Stdout: "python\n3.11.0\n", ExitCode: 0}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when tool is not installed")
	}
}

func TestAsdfAdapterCheckNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &AsdfAdapter{}
	tool := &schema.Tool{Name: "tool"}
	mc := &schema.MethodCandidate{Kind: "asdf"} // no pkg in config
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when no package name")
	}
}

func TestAsdfAdapterInstall(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	if err := a.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Install error: %v", err)
	}

	got := fr.Calls
	if len(got) < 4 {
		t.Fatalf("expected at least 4 calls, got %d", len(got))
	}
	// Call order: which asdf, plugin-add, install, global
	if got[0].Name != "which" || got[0].Args[0] != "asdf" {
		t.Errorf("expected first call 'which asdf', got %v", got[0])
	}
	if got[1].Name != "asdf" || got[1].Args[0] != "plugin-add" {
		t.Errorf("expected 'asdf plugin-add', got %v", got[1])
	}
	if got[2].Name != "asdf" || got[2].Args[0] != "install" {
		t.Errorf("expected 'asdf install', got %v", got[2])
	}
}

func TestAsdfAdapterInstallFailure(t *testing.T) {
	t.Parallel()
	// which asdf → ExitCode=1 → tries which mise → ExitCode=1 → error
	fr := &run.FakeRunner{ExitCode: 1}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected Install error when neither asdf nor mise found")
	}
}

func TestAsdfAdapterInstallNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &AsdfAdapter{}
	tool := &schema.Tool{Name: ""} // both tool name and config are empty
	mc := &schema.MethodCandidate{Kind: "asdf"}
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error for missing package name")
	}
}

func TestAsdfAdapterDoesNotRemoveViaCanRemove(t *testing.T) {
	t.Parallel()
	a := &AsdfAdapter{}
	if exec.CanRemove(a) {
		t.Fatal("AsdfAdapter should not implement Remover")
	}
}
