package source

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

type scriptedRunner struct {
	calls   []run.FakeCall
	outputs []run.Result
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: args})
	result := r.outputs[0]
	r.outputs = r.outputs[1:]
	return result
}

func TestManagerAddsPPAAndUpdatesOnce(t *testing.T) {
	run.OverrideElevation("sudo")
	t.Cleanup(func() { run.OverrideElevation("") })
	runner := &scriptedRunner{outputs: []run.Result{{}, {}, {}, {Stdout: []byte("https://ppa.launchpadcontent.net/neovim-ppa/stable/ubuntu")}}}
	manager := NewManager(runner, false)
	source := config.Source{Kind: "apt-ppa", Name: "ppa:neovim-ppa/stable"}
	if _, err := manager.Ensure(context.Background(), []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure(context.Background(), []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if runner.calls[1].Name != "sudo" || runner.calls[2].Name != "sudo" {
		t.Fatalf("commands = %v", runner.calls)
	}
}

func TestManagerDryRunDoesNotMutate(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{}}}
	missing, err := NewManager(runner, true).Ensure(context.Background(), []config.Source{{Kind: "brew-tap", Name: "user/tap"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || len(runner.calls) != 1 {
		t.Fatalf("missing=%v calls=%v", missing, runner.calls)
	}
}

func TestManagerRejectsUnsupportedSourceKind(t *testing.T) {
	runner := &scriptedRunner{}
	_, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{{Kind: "unknown", Name: "x"}})
	if err == nil {
		t.Fatal("expected unsupported source kind error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unexpected commands: %v", runner.calls)
	}
}

func TestManagerPropagatesCheckFailure(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{Err: context.DeadlineExceeded, ExitCode: 1}}}
	_, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{{Kind: "brew-tap", Name: "user/tap"}})
	if err == nil {
		t.Fatal("expected source check failure")
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != "brew" {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestManagerPropagatesAddFailure(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: nil}, {Err: context.DeadlineExceeded, ExitCode: 1}}}
	_, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{{Kind: "brew-tap", Name: "user/tap"}})
	if err == nil {
		t.Fatal("expected source add failure")
	}
	if len(runner.calls) != 2 || runner.calls[1].Name != "brew" {
		t.Fatalf("calls=%v", runner.calls)
	}
}
