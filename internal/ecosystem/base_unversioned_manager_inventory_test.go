package ecosystem

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type snapOutputRunner struct {
	results []run.Result
	calls   [][]string
}

func (r *snapOutputRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(r.calls) > len(r.results) {
		return run.Result{ExitCode: 1}
	}
	return r.results[len(r.calls)-1]
}

func TestBaseAdapterSnapObserveInventorySafety(t *testing.T) {
	const full = "Name Version Rev Tracking Publisher Notes\n" +
		"demo 2.4.0 55 latest/beta publisher -\n" +
		"other 3.0 8 latest/stable publisher classic\n"
	tests := []struct {
		name    string
		stdout  string
		runErr  error
		waitErr error
		exit    int
		channel string
		want    plan.PresenceState
		version string
	}{
		{name: "installed", stdout: full, want: plan.PresencePresent, version: "2.4.0"},
		{name: "matching channel", stdout: full, channel: "beta", want: plan.PresencePresent, version: "2.4.0"},
		{name: "different channel reports presence for drift comparison", stdout: full, channel: "stable", want: plan.PresencePresent, version: "2.4.0"},
		{name: "absent from complete inventory", stdout: "Name Version Rev Tracking Publisher Notes\nother 1.0 4 latest/stable store -\n", want: plan.PresenceAbsent},
		{name: "empty inventory with header", stdout: "Name Version Rev Tracking Publisher Notes\n", want: plan.PresenceAbsent},
		{name: "missing header", stdout: "", want: plan.PresenceBroken},
		{name: "missing tracking column", stdout: "Name Version Rev Tracking Publisher Notes\ndemo 2.4.0 55 publisher -\n", want: plan.PresenceBroken},
		{name: "invalid row after target", stdout: full + "invalid row\n", want: plan.PresenceBroken},
		{name: "duplicate package", stdout: full + "demo 2.4.0 55 latest/beta publisher -\n", want: plan.PresenceBroken},
		{name: "invalid tracking syntax", stdout: "Name Version Rev Tracking Publisher Notes\ndemo 2.4.0 55 unknown publisher -\n", want: plan.PresenceBroken},
		{name: "invalid risk after target", stdout: full + "another 2.4.0 55 latest/unknown publisher -\n", want: plan.PresenceBroken},
		{name: "failed process", stdout: full, runErr: errors.New("spawn failed"), want: plan.PresenceUnknown},
		{name: "failed exit", stdout: full, exit: 2, want: plan.PresenceUnknown},
		{name: "incomplete capture", stdout: full, waitErr: errors.New("incomplete"), want: plan.PresenceUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &config.MethodCandidate{Kind: "snap", Config: map[string]any{"pkg": "demo"}}
			if tt.channel != "" {
				mc.Config["channel"] = tt.channel
			}
			runner := &run.FakeRunner{Stdout: tt.stdout, ExitCode: tt.exit, Err: tt.runErr, WaitErr: tt.waitErr}
			obs, err := NewBaseAdapter(Configs["snap"]).Observe(context.Background(), runner, &config.Tool{Name: "demo"}, mc)
			if err != nil || obs.Presence != tt.want || obs.Identity.Version != tt.version {
				t.Fatalf("Observe() = %+v, err=%v; want %s with version %q", obs, err, tt.want, tt.version)
			}
			calls := runner.Calls
			if !reflect.DeepEqual(calls[0].Args, []string{"list", "--color=never", "--unicode=never"}) {
				t.Fatalf("inventory query = %#v", calls)
			}
			if tt.want == plan.PresencePresent {
				if obs.Identity.Revision != "55" || obs.Identity.Channel != "latest/beta" || !reflect.DeepEqual(obs.KnownFields, []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldRevision, plan.FieldChannel}) {
					t.Fatalf("incomplete identity: %+v", obs)
				}
			} else if tt.want != plan.PresenceAbsent {
				if decision, decisionErr := plan.TransitionForVerification(plan.Reconcile(plan.ResolvedIdentity{Package: "demo"}, obs)); decisionErr == nil || decision.Required {
					t.Fatalf("untrusted inventory authorized mutation: %+v, %v", decision, decisionErr)
				}
			}
		})
	}
}

func TestSnapEmptyListAndLocalTracking(t *testing.T) {
	runner := &run.FakeRunner{Stderr: "No snaps are installed yet. Try 'snap install hello-world'.\n"}
	obs := NewBaseAdapter(Configs["snap"]).observeSnapInventory(context.Background(), runner, "demo")
	if obs.Presence != plan.PresenceAbsent {
		t.Fatalf("empty inventory = %+v, want absent", obs)
	}
	if !validSnapTracking("-") {
		t.Fatal("local snap tracking '-' is invalid")
	}
}

func TestSnapTrackingInfoUsesAuthoritativeValue(t *testing.T) {
	got, ok := snapTrackingFromInfo("name: demo\ntracking: 22/candidate/hotfix\n", "demo")
	if !ok || got != "22/candidate/hotfix" {
		t.Fatalf("tracking info = %q, %t", got, ok)
	}
}

func TestSnapBranchInfoReplacesAbbreviatedListChannel(t *testing.T) {
	const listed = "Name Version Rev Tracking Publisher Notes\ndemo 1.0 42 22/candidate/… publisher -\n"
	runner := &snapOutputRunner{results: []run.Result{
		{Stdout: []byte(listed)},
		{Stdout: []byte("name: demo\ntracking: 22/candidate/hotfix\n")},
		{Stdout: []byte(listed)},
	}}
	obs := NewBaseAdapter(Configs["snap"]).observeSnapInventory(context.Background(), runner, "demo")
	if obs.Presence != plan.PresencePresent || obs.Identity.Channel != "22/candidate/hotfix" {
		t.Fatalf("inventory observation = %+v", obs)
	}
	wantCalls := [][]string{
		{"snap", "list", "--color=never", "--unicode=never"},
		{"snap", "info", "demo"},
		{"snap", "list", "--color=never", "--unicode=never", "demo"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("snap calls = %#v, want %#v", runner.calls, wantCalls)
	}
	wantFields := []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldRevision, plan.FieldChannel}
	if !reflect.DeepEqual(obs.KnownFields, wantFields) {
		t.Fatalf("known fields = %#v, want %#v", obs.KnownFields, wantFields)
	}

	runner.results[1].Stdout = []byte("name: demo\ntracking: 23/stable/foo\n")
	runner.calls = nil
	obs = NewBaseAdapter(Configs["snap"]).observeSnapInventory(context.Background(), runner, "demo")
	if obs.Presence != plan.PresenceUnknown {
		t.Fatalf("inconsistent info channel = %+v, want unknown", obs)
	}

	runner.results[1].Stdout = []byte("name: demo\ntracking: 22/candidate/hotfix\n")
	runner.results[2].Stdout = []byte("Name Version Rev Tracking Publisher Notes\ndemo 1.0 43 22/candidate/… publisher -\n")
	runner.calls = nil
	obs = NewBaseAdapter(Configs["snap"]).observeSnapInventory(context.Background(), runner, "demo")
	if obs.Presence != plan.PresenceUnknown {
		t.Fatalf("changed revision during info query = %+v, want unknown", obs)
	}
}

func TestSnapCheckRejectsInconsistentBranchInfo(t *testing.T) {
	runner := &snapOutputRunner{results: []run.Result{
		{Stdout: []byte("Name Version Rev Tracking Publisher Notes\ndemo 1.0 42 22/candidate/… publisher -\n")},
		{Stdout: []byte("name: demo\ntracking: 23/stable/foo\n")},
	}}
	method := &config.MethodCandidate{Config: map[string]any{
		"pkg": "demo", "track": "22", "risk": "candidate", "branch": "hotfix",
	}}
	if NewBaseAdapter(Configs["snap"]).checkSnap(context.Background(), runner, &config.Tool{Name: "demo"}, method) {
		t.Fatal("inconsistent info tracking must not verify the requested branch")
	}
}

func TestSnapLocalAndUnrelatedRowsDoNotProveChannel(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "Name Version Rev Tracking Publisher Notes\ndemo 1.0 42 - publisher -\nother 2.0 43 - publisher -\n"}
	obs := NewBaseAdapter(Configs["snap"]).observeSnapInventory(context.Background(), runner, "demo")
	if obs.Presence != plan.PresencePresent || obs.Identity.Channel != "-" {
		t.Fatalf("local snap observation = %+v", obs)
	}
	wantFields := []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldRevision}
	if !reflect.DeepEqual(obs.KnownFields, wantFields) {
		t.Fatalf("local snap known fields = %#v, want %#v", obs.KnownFields, wantFields)
	}
	reconciled := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Channel: "latest/stable"}, obs)
	if reconciled.State == plan.StateDrifted {
		t.Fatalf("unproven local tracking produced drift: %+v", reconciled)
	}

	obs = NewBaseAdapter(Configs["snap"]).observeSnapInventory(context.Background(), &run.FakeRunner{Stdout: "Name Version Rev Tracking Publisher Notes\nother 2.0 43 - publisher -\n"}, "demo")
	if obs.Presence != plan.PresenceAbsent {
		t.Fatalf("unrelated local snap observation = %+v, want absent", obs)
	}
}

func TestBaseAdapterEditorExtensionsObserveInventorySafety(t *testing.T) {
	for _, tt := range []struct{ kind, bin string }{{"vscode", "code"}, {"vscodium", "codium"}} {
		t.Run(tt.kind, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				stdout  string
				err     error
				waitErr error
				exit    int
				want    plan.PresenceState
			}{
				{name: "installed", stdout: "ms-python.python\nredhat.vscode-yaml\n", want: plan.PresencePresent},
				{name: "case insensitive identity", stdout: "MS-Python.Python\n", want: plan.PresencePresent},
				{name: "absent", stdout: "redhat.vscode-yaml\n", want: plan.PresenceAbsent},
				{name: "empty inventory", stdout: "", want: plan.PresenceAbsent},
				{name: "invalid row after target", stdout: "ms-python.python\nwarning: partial output\n", want: plan.PresenceBroken},
				{name: "invalid extension name", stdout: "unqualified-extension\n", want: plan.PresenceBroken},
				{name: "duplicate", stdout: "ms-python.python\nMS-Python.Python\n", want: plan.PresenceBroken},
				{name: "execution failure", err: errors.New("cannot run"), want: plan.PresenceUnknown},
				{name: "nonzero exit", exit: 1, want: plan.PresenceUnknown},
				{name: "truncated output", stdout: "ms-python.python\n", waitErr: errors.New("incomplete"), want: plan.PresenceUnknown},
			} {
				t.Run(tc.name, func(t *testing.T) {
					runner := &run.FakeRunner{Stdout: tc.stdout, Err: tc.err, WaitErr: tc.waitErr, ExitCode: tc.exit}
					mc := &config.MethodCandidate{Kind: tt.kind, Config: map[string]any{"pkg": "ms-python.python"}}
					obs, err := NewBaseAdapter(Configs[tt.kind]).Observe(context.Background(), runner, &config.Tool{Name: "python"}, mc)
					if err != nil || obs.Presence != tc.want {
						t.Fatalf("Observe() = %+v, err=%v; want %s", obs, err, tc.want)
					}
					lastCall := runner.Calls[len(runner.Calls)-1]
					if !reflect.DeepEqual(lastCall.Args, []string{"--list-extensions"}) || lastCall.Name != tt.bin {
						t.Fatalf("inventory query = %#v", runner.Calls)
					}
					if tc.want == plan.PresencePresent && !reflect.DeepEqual(obs.KnownFields, []plan.IdentityField{plan.FieldPackage}) {
						t.Fatalf("missing package identity: %+v", obs)
					} else if tc.want == plan.PresenceUnknown || tc.want == plan.PresenceBroken {
						if decision, decisionErr := plan.TransitionForVerification(plan.Reconcile(plan.ResolvedIdentity{Package: "ms-python.python"}, obs)); decisionErr == nil || decision.Required {
							t.Fatalf("untrusted inventory authorized mutation: %+v, %v", decision, decisionErr)
						}
					}
				})
			}
		})
	}
}
