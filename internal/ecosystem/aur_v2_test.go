package ecosystem

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

func TestAURAdapterV2AliasesConform(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter *AURAdapter
		kind    string
	}{
		{"aur", NewAURAdapter("paru"), "aur"},
		{"paru", NewAURAdapter("paru"), "paru"},
		{"yay", NewAURAdapter("yay"), "yay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := &config.Tool{Name: "tool"}
			mc := &config.MethodCandidate{Kind: tc.kind, Config: map[string]any{"pkg": "aur-tool"}}
			intent, err := planner.BuildCandidateIntent(tool, mc)
			if err != nil {
				t.Fatal(err)
			}
			intent.Identity.Package = "planner-package"
			resolved, err := tc.adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
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
			observation, err := tc.adapter.Observe(context.Background(), runner, tool, mc)
			if err != nil || observation.Presence != plan.PresencePresent {
				t.Fatalf("Observe() = %#v, error = %v", observation, err)
			}
			if want := (run.FakeCall{Name: tc.adapter.helper, Args: []string{"-Qi", "aur-tool"}}); !reflect.DeepEqual(runner.Calls[0], want) {
				t.Fatalf("observe call = %#v, want %#v", runner.Calls[0], want)
			}

			runner = &run.FakeRunner{}
			if err := tc.adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
				t.Fatal(err)
			}
			want := run.FakeCall{Name: tc.adapter.helper, Args: []string{"-S", "--noconfirm", "planner-package"}}
			if !reflect.DeepEqual(runner.Calls, []run.FakeCall{want}) {
				t.Fatalf("install calls = %#v, want %#v", runner.Calls, []run.FakeCall{want})
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
