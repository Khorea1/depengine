package ecosystem

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

var registerAURAliasesForTest sync.Once

func TestAURAdapterV2AliasesConform(t *testing.T) {
	registerAURAliasesForTest.Do(RegisterAURAliases)

	contract, ok := methodkind.Lookup("aur")
	if !ok || contract == nil {
		t.Fatal("methodkind.Lookup(aur) did not return a contract")
	}
	if len(contract.Aliases) == 0 {
		t.Fatal("aur method contract has no aliases")
	}

	for _, alias := range contract.Aliases {
		t.Run(alias, func(t *testing.T) {
			adapter, ok := exec.Lookup(alias).(*AURByNameAdapter)
			if !ok {
				t.Fatalf("registered adapter for %q = %T, want *AURByNameAdapter", alias, exec.Lookup(alias))
			}
			if adapter.Kind() != alias {
				t.Fatalf("adapter kind = %q, want %q", adapter.Kind(), alias)
			}
			if adapter.helper != alias {
				t.Fatalf("adapter helper = %q, want %q", adapter.helper, alias)
			}

			tool := &config.Tool{Name: "tool"}
			mc := &config.MethodCandidate{Kind: alias, Config: map[string]any{"pkg": "aur-tool"}}
			intent, err := planner.BuildCandidateIntent(tool, mc)
			if err != nil {
				t.Fatal(err)
			}
			intent.Identity.Package = "planner-package"
			resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
			if err != nil {
				t.Fatal(err)
			}
			if err := plan.ValidateResolution(intent, *resolved); err != nil {
				t.Fatalf("ValidateResolution() error = %v", err)
			}
			if resolved.Identity.Package != "planner-package" {
				t.Fatalf("package = %q, want planner-package", resolved.Identity.Package)
			}

			runner := &run.FakeRunner{ExitCode: 0}
			observation, err := adapter.Observe(context.Background(), runner, tool, mc)
			if err != nil || observation.Presence != plan.PresencePresent {
				t.Fatalf("Observe() = %#v, error = %v", observation, err)
			}
			wantObserve := run.FakeCall{Name: alias, Args: []string{"-Qi", "aur-tool"}}
			if !reflect.DeepEqual(runner.Calls, []run.FakeCall{wantObserve}) {
				t.Fatalf("observe calls = %#v, want %#v", runner.Calls, []run.FakeCall{wantObserve})
			}

			runner = &run.FakeRunner{}
			if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
				t.Fatal(err)
			}
			wantInstall := run.FakeCall{Name: alias, Args: []string{"-S", "--noconfirm", "planner-package"}}
			if !reflect.DeepEqual(runner.Calls, []run.FakeCall{wantInstall}) {
				t.Fatalf("install calls = %#v, want %#v", runner.Calls, []run.FakeCall{wantInstall})
			}
		})
	}
}

func TestAURAdapterV2ObserveDistinguishesBackendFailure(t *testing.T) {
	wantErr := context.DeadlineExceeded
	observation, err := NewAURAdapter("paru").Observe(context.Background(), &run.FakeRunner{Err: wantErr}, &config.Tool{Name: "foo"}, &config.MethodCandidate{Config: map[string]any{"pkg": "foo"}})
	if observation.Presence != plan.PresenceUnknown || !errors.Is(err, wantErr) {
		t.Fatalf("Observe() = %#v, error = %v, want unknown and backend error", observation, err)
	}
}

func TestAURAdapterV2RejectsExplicitOperations(t *testing.T) {
	resolved := plan.New("foo", "aur", true)
	resolved.Identity.Package = "foo"
	resolved.Operations = []plan.Operation{{Kind: "arbitrary", Effect: plan.EffectMutation, Command: []string{"sh", "-c", "unsafe"}, ArbitraryCode: true}}
	err := NewAURAdapter("paru").InstallResolved(context.Background(), &run.FakeRunner{}, nil, nil, &resolved)
	if err == nil || !strings.Contains(err.Error(), "operations are unsupported") {
		t.Fatalf("InstallResolved() error = %v, want unsupported operations", err)
	}
}
