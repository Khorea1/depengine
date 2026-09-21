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

func TestManagerDryRunMutationBoundaryBlocksAdd(t *testing.T) {
	runner := &scriptedRunner{}
	manager := NewManager(runner, true)
	err := manager.add(context.Background(), config.Source{Kind: "brew-tap", Name: "user/tap"})
	if err == nil {
		t.Fatal("expected dry-run mutation boundary to reject source add")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("underlying runner must not be reached, calls=%v", runner.calls)
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

func TestEnsureTrackedReportsOnlyConfirmedAdds(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{}, {}}}
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools"}
	result, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != source {
		t.Fatalf("missing=%v want [%v]", result.Missing, source)
	}
	if len(result.Added) != 1 || result.Added[0] != source {
		t.Fatalf("added=%v want [%v]", result.Added, source)
	}
}

func TestEnsureTrackedDoesNotClaimFailedAdd(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{}, {Err: context.DeadlineExceeded, ExitCode: 1}}}
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools"}
	result, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err == nil {
		t.Fatal("expected add failure")
	}
	if len(result.Missing) != 1 {
		t.Fatalf("missing=%v want source observed missing", result.Missing)
	}
	if len(result.Added) != 0 {
		t.Fatalf("added=%v; failed add must not be reported as confirmed", result.Added)
	}
	if result.Unconfirmed == nil || *result.Unconfirmed != source {
		t.Fatalf("unconfirmed=%v want %v", result.Unconfirmed, source)
	}
}

func TestManagerRemoveBrewTap(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte("vendor/tools\n")}, {}}}
	manager := NewManager(runner, false)
	if err := manager.Remove(context.Background(), []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%v want probe + removal", runner.calls)
	}
	if got := runner.calls[1]; got.Name != "brew" || len(got.Args) != 2 || got.Args[0] != "untap" || got.Args[1] != "vendor/tools" {
		t.Fatalf("remove call=%v", got)
	}
}

func TestManagerRemovePPARefreshesIndex(t *testing.T) {
	run.OverrideElevation("sudo")
	t.Cleanup(func() { run.OverrideElevation("") })
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("https://ppa.launchpadcontent.net/vendor/stable/ubuntu\n")},
		{},
		{},
	}}
	manager := NewManager(runner, false)
	if err := manager.Remove(context.Background(), []config.Source{{Kind: "apt-ppa", Name: "ppa:vendor/stable"}}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls=%v want probe + remove + refresh", runner.calls)
	}
	remove := runner.calls[1]
	if remove.Name != "sudo" || len(remove.Args) < 5 || remove.Args[0] != "add-apt-repository" || remove.Args[2] != "--remove" {
		t.Fatalf("remove call=%v", remove)
	}
	refresh := runner.calls[2]
	if refresh.Name != "sudo" || len(refresh.Args) != 2 || refresh.Args[0] != "apt-get" || refresh.Args[1] != "update" {
		t.Fatalf("refresh call=%v", refresh)
	}
}
