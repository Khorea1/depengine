package ecosystem

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

func asdfTool(name, pkg string) (*config.Tool, *config.MethodCandidate) {
	tool := &config.Tool{Name: name}
	mc := &config.MethodCandidate{
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
	tool := &config.Tool{Name: "tool"}
	mc := &config.MethodCandidate{Kind: "asdf"} // no pkg in config
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
	if len(got) < 5 {
		t.Fatalf("expected at least 5 calls, got %d", len(got))
	}
	// Call order: which asdf, plugin list (empty → plugin-add), install, global
	if got[0].Name != "which" || got[0].Args[0] != "asdf" {
		t.Errorf("expected first call 'which asdf', got %v", got[0])
	}
	if got[1].Name != "asdf" || len(got[1].Args) < 2 || got[1].Args[0] != "plugin" || got[1].Args[1] != "list" {
		t.Errorf("expected 'asdf plugin list', got %v", got[1])
	}
	if got[2].Name != "asdf" || got[2].Args[0] != "plugin-add" {
		t.Errorf("expected 'asdf plugin-add nodejs', got %v", got[2])
	}
	if got[3].Name != "asdf" || got[3].Args[0] != "install" {
		t.Errorf("expected 'asdf install', got %v", got[3])
	}
	if got[4].Name != "asdf" || got[4].Args[0] != "global" {
		t.Errorf("expected 'asdf global nodejs latest', got %v", got[4])
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
	tool := &config.Tool{Name: ""} // both tool name and config are empty
	mc := &config.MethodCandidate{Kind: "asdf"}
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error for missing package name")
	}
}

func TestAsdfAdapterCanRemove(t *testing.T) {
	t.Parallel()
	a := &AsdfAdapter{}
	if !exec.CanRemove(a) {
		t.Fatal("AsdfAdapter should implement Remover with CanRemove=true")
	}
}

func TestAsdfAdapterRemove(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &AsdfAdapter{}
	tool := &config.Tool{Name: "nodejs"}
	mc := &config.MethodCandidate{Kind: "asdf", Config: map[string]any{"pkg": "nodejs", "version": "18.0.0"}}
	if err := a.Remove(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Remove error: %v", err)
	}

	got := fr.Calls
	if len(got) < 2 {
		t.Fatalf("expected at least 2 calls (which asdf, asdf uninstall), got %d", len(got))
	}
	if got[0].Name != "which" || got[0].Args[0] != "asdf" {
		t.Errorf("expected first call 'which asdf', got %v", got[0])
	}
	last := got[len(got)-1]
	if last.Name != "asdf" || len(last.Args) < 3 || last.Args[0] != "uninstall" || last.Args[1] != "nodejs" || last.Args[2] != "18.0.0" {
		t.Errorf("expected 'asdf uninstall nodejs 18.0.0', got %v", last)
	}
}

func TestAsdfAdapterRemoveRequiresVersion(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &AsdfAdapter{}
	tool := &config.Tool{Name: "nodejs"}
	mc := &config.MethodCandidate{Kind: "asdf", Config: map[string]any{"pkg": "nodejs"}} // no version
	if err := a.Remove(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error when no version is configured")
	}
}

func TestAsdfAdapterRemoveNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &AsdfAdapter{}
	tool := &config.Tool{Name: ""}
	mc := &config.MethodCandidate{Kind: "asdf", Config: map[string]any{"version": "18.0.0"}}
	if err := a.Remove(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error for missing package name")
	}
}
