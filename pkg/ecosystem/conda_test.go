package ecosystem

import (
	"context"
	"testing"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func condaTool(name, pkg string) (*schema.Tool, *schema.MethodCandidate) {
	tool := &schema.Tool{Name: name}
	mc := &schema.MethodCandidate{
		Kind:   "conda",
		Config: map[string]any{"pkg": pkg},
	}
	return tool, mc
}

func TestCondaAdapterAvailable(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &CondaAdapter{}
	if !a.Available(context.Background(), fr) {
		t.Fatal("expected Available=true when conda is found")
	}
}

func TestCondaAdapterAvailableMissing(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	a := &CondaAdapter{}
	if a.Available(context.Background(), fr) {
		t.Fatal("expected Available=false when conda is not found")
	}
}

func TestCondaAdapterCheck(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Stdout: "# packages in environment\npython 3.11.0\n", ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if !a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=true when package is installed")
	}
}

func TestCondaAdapterCheckNotInstalled(t *testing.T) {
	t.Parallel()
	// Stdout doesn't contain the package name.
	fr := &run.FakeRunner{Stdout: "# packages in environment\n", ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("nodejs", "nodejs")
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when package is not installed")
	}
}

func TestCondaAdapterCheckNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &CondaAdapter{}
	tool := &schema.Tool{Name: "tool"}
	mc := &schema.MethodCandidate{Kind: "conda"} // no pkg
	if a.Check(context.Background(), fr, tool, mc) {
		t.Fatal("expected Check=false when no package name")
	}
}

func TestCondaAdapterInstall(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if err := a.Install(context.Background(), fr, tool, mc); err != nil {
		t.Fatalf("unexpected Install error: %v", err)
	}

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	got := fr.Calls[0]
	if got.Name != "conda" || got.Args[0] != "install" || got.Args[1] != "-y" || got.Args[2] != "python" {
		t.Errorf("unexpected install call: %v", got)
	}
}

func TestCondaAdapterInstallFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	a := &CondaAdapter{}
	tool, mc := condaTool("python", "python")
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected Install error, got nil")
	}
}

func TestCondaAdapterInstallNoPkg(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}

	a := &CondaAdapter{}
	tool := &schema.Tool{Name: ""} // both tool name and config are empty
	mc := &schema.MethodCandidate{Kind: "conda"}
	if err := a.Install(context.Background(), fr, tool, mc); err == nil {
		t.Fatal("expected error for missing package name")
	}
}

func TestCondaAdapterDoesNotRemoveViaCanRemove(t *testing.T) {
	t.Parallel()
	a := &CondaAdapter{}
	if exec.CanRemove(a) {
		t.Fatal("CondaAdapter should not implement Remover")
	}
}
