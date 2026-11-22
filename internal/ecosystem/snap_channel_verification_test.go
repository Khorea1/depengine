package ecosystem

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

func TestSnapChannelEndToEndVerification(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{Kind: "snap", Config: map[string]any{"pkg": "demo", "track": "22", "risk": "candidate", "branch": "hotfix"}}
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewBaseAdapter(Configs["snap"])
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, method, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.ValidateResolution(intent, *resolved); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, tracking string
		state          plan.VerificationState
	}{
		{"matching", "22/candidate/hotfix", plan.StateSatisfied},
		{"different risk", "22/stable", plan.StateDrifted},
		{"different branch", "22/candidate/other", plan.StateDrifted},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{Stdout: "Name Version Rev Tracking Publisher Notes\ndemo 1.0 33 " + tt.tracking + " publisher -\n"}
			obs, err := adapter.Observe(context.Background(), runner, tool, method)
			if err != nil {
				t.Fatal(err)
			}
			if obs.Presence != plan.PresencePresent || obs.Identity.Channel != tt.tracking {
				t.Fatalf("observation=%+v", obs)
			}
			result := plan.Reconcile(resolved.Identity, obs)
			if result.State != tt.state {
				t.Fatalf("verification=%+v want %s", result, tt.state)
			}
			if err := result.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	runner := &run.FakeRunner{}
	if err = adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatal(err)
	}
	last := runner.Calls[len(runner.Calls)-1]
	if !reflect.DeepEqual(last.Args, []string{"install", "demo", "--channel=22/candidate/hotfix"}) {
		t.Fatalf("install argv = %#v", last.Args)
	}
}

// A channel selector is mutable, so a drifted tracking channel cannot enter
// the remove/install replacement WAL. This must be rejected before a candidate
// pre-hook or lazy prerequisite is allowed to modify the host.
func TestSnapChannelDriftStopsBeforeAnyHostMutation(t *testing.T) {
	for _, tc := range []struct {
		name, tracking string
		wantStatus     exec.StatusEnum
	}{
		{"different channel", "latest/stable", exec.StatusFailed},
		{"matching channel", "latest/beta", exec.StatusAlready},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			runner := &run.FakeRunner{Stdout: "Name Version Rev Tracking Publisher Notes\ndemo 1.0 33 " + tc.tracking + " publisher -\n"}
			method := &config.MethodCandidate{Kind: "snap", Config: map[string]any{"pkg": "demo", "channel": "beta"},
				PreInstall: []config.Hook{{Run: []string{"should-not-run"}}}, Requires: []string{"helper"}}
			schema := &config.Schema{Tools: map[string]*config.Tool{
				"demo":   {Name: "demo", Methods: []*config.MethodCandidate{method}},
				"helper": {Name: "helper", DependencyOnly: true, Methods: []*config.MethodCandidate{{Kind: "snap", Config: map[string]any{"pkg": "helper"}}}},
			}}
			executor := exec.New()
			exec.WithRunner(runner)(executor)
			exec.WithAdapters(NewBaseAdapter(Configs["snap"]))(executor)
			exec.WithAllowArbitraryCode()(executor)
			report, err := executor.Execute(context.Background(), schema, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Tools) != 1 || report.Tools[0].Tool != "demo" || report.Tools[0].Status != tc.wantStatus {
				t.Fatalf("report=%+v, want status %v without helper", report, tc.wantStatus)
			}
			if tc.wantStatus == exec.StatusFailed && !strings.Contains(report.Tools[0].Error, "cannot be journaled safely") {
				t.Fatalf("error = %q, want actionable replacement preflight failure", report.Tools[0].Error)
			}
			for _, call := range runner.Calls {
				if (call.Name == "which" && slices.Equal(call.Args, []string{"snap"})) ||
					(call.Name == "snap" && slices.Equal(call.Args, []string{"list", "--color=never", "--unicode=never"})) {
					continue
				}
				t.Fatalf("unexpected host mutation: %s %v", call.Name, call.Args)
			}
			stored, err := state.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(stored.ReplacementTransactions) != 0 || len(stored.Tools) != 0 {
				t.Fatalf("unexpected state mutation: %+v", stored)
			}
		})
	}
}
