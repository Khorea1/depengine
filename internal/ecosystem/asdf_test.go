package ecosystem

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
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
	// No installed versions reported → Check fails.
	fr := &run.FakeRunner{Stdout: "", ExitCode: 0}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when tool is not installed")
	}
}

func TestAsdfAdapterCheckHonorsConfiguredVersion(t *testing.T) {
	t.Parallel()
	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	mc.Config["version"] = "18.20.4"

	installed := &run.FakeRunner{Stdout: "  18.20.4\n  20.17.0\n", ExitCode: 0}
	if !a.Check(context.Background(), installed, tool, mc) {
		t.Fatal("expected Check=true when configured version is installed")
	}

	otherVersion := &run.FakeRunner{Stdout: "  20.17.0\n", ExitCode: 0}
	if a.Check(context.Background(), otherVersion, tool, mc) {
		t.Fatal("expected Check=false when only a different version is installed")
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
	if got[3].Name != "asdf" || len(got[3].Args) != 3 || got[3].Args[0] != "install" || got[3].Args[1] != "nodejs" || got[3].Args[2] != "latest" {
		t.Errorf("expected 'asdf install nodejs latest', got %v", got[3])
	}
	if got[4].Name != "asdf" || len(got[4].Args) != 3 || got[4].Args[0] != "global" || got[4].Args[1] != "nodejs" || got[4].Args[2] != "latest" {
		t.Errorf("expected 'asdf global nodejs latest', got %v", got[4])
	}
}

func TestAsdfAdapterInstallHonorsConfiguredVersion(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	mc.Config["version"] = "18.20.4"
	if err := a.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Install error: %v", err)
	}

	var install, global *run.FakeCall
	for i := range fr.Calls {
		call := &fr.Calls[i]
		if call.Name == "asdf" && len(call.Args) > 0 {
			switch call.Args[0] {
			case "install":
				install = call
			case "global":
				global = call
			}
		}
	}
	if install == nil || len(install.Args) != 3 || install.Args[1] != "nodejs" || install.Args[2] != "18.20.4" {
		t.Fatalf("expected 'asdf install nodejs 18.20.4', got %v", install)
	}
	if global == nil || len(global.Args) != 3 || global.Args[1] != "nodejs" || global.Args[2] != "18.20.4" {
		t.Fatalf("expected 'asdf global nodejs 18.20.4', got %v", global)
	}
}

func TestMiseInstallHonorsConfiguredVersion(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{
		ExitCode:  0,
		LookPaths: map[string]bool{"asdf": false, "mise": true},
	}

	a := &AsdfAdapter{}
	tool, mc := asdfTool("nodejs", "nodejs")
	mc.Config["version"] = "18.20.4"
	if err := a.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Install error: %v", err)
	}

	var install, use *run.FakeCall
	for i := range fr.Calls {
		call := &fr.Calls[i]
		if call.Name == "mise" && len(call.Args) > 0 {
			switch call.Args[0] {
			case "install":
				install = call
			case "use":
				use = call
			}
		}
	}
	if install == nil || len(install.Args) != 2 || install.Args[1] != "nodejs@18.20.4" {
		t.Fatalf("expected 'mise install nodejs@18.20.4', got %v", install)
	}
	if use == nil || len(use.Args) != 3 || use.Args[1] != "-g" || use.Args[2] != "nodejs@18.20.4" {
		t.Fatalf("expected 'mise use -g nodejs@18.20.4', got %v", use)
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
