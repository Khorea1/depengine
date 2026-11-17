package contracttest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

type verifyStateRunner struct {
	responses       map[string]string
	defaultResponse string
	exitCodes       map[string]int
	stderr          map[string]string
	calls           []string
}

func (r *verifyStateRunner) Run(_ context.Context, name string, args ...string) run.Result {
	key := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, key)
	result := run.Result{ExitCode: r.exitCodes[key], Stderr: []byte(r.stderr[key])}
	if response, ok := r.responses[key]; ok {
		result.Stdout = []byte(response)
		return result
	}
	result.Stdout = []byte(r.defaultResponse)
	return result
}

func (r *verifyStateRunner) LookPath(context.Context, string) bool { return true }

func (r *verifyStateRunner) RunWithEnv(ctx context.Context, _ map[string]string, _ []string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func (r *verifyStateRunner) RunInDir(ctx context.Context, _ string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func probeObservation(t *testing.T, adapter interface {
	Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error)
}, runner run.Runner, method *config.MethodCandidate) plan.Observation {
	t.Helper()
	got, err := adapter.Observe(context.Background(), runner, &config.Tool{Name: "probe"}, method)
	if err != nil {
		t.Fatalf("Observe(%s) error: %v", method.Kind, err)
	}
	return got
}

func requireProbeStates(t *testing.T, adapter interface {
	Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error)
}, runner run.Runner, desired [2]plan.ResolvedIdentity, methods [2]*config.MethodCandidate) {
	t.Helper()
	states := [2]plan.VerificationState{}
	for i := range methods {
		states[i] = plan.Reconcile(desired[i], probeObservation(t, adapter, runner, methods[i])).State
	}
	if states[0] == states[1] || states[0] != plan.StateSatisfied || states[1] != plan.StateAbsent && states[1] != plan.StateDrifted {
		t.Fatalf("A/B verification states = %v; want satisfied then absent/drifted", states)
	}
}

func TestVerificationPhaseAdapterFieldProbes(t *testing.T) {
	t.Run("conda observed identity dimensions", func(t *testing.T) {
		adapter := ecosystem.NewCondaAdapter()
		cases := []struct {
			name                          string
			first, second                 map[string]any
			firstIdentity, secondIdentity plan.ResolvedIdentity
			response                      string
		}{
			{"version", map[string]any{"pkg": "demo", "version": "1"}, map[string]any{"pkg": "demo", "version": "2"}, plan.ResolvedIdentity{Package: "demo", Version: "1"}, plan.ResolvedIdentity{Package: "demo", Version: "2"}, `[{"name":"demo","version":"1","build":"py_0","channel":"stable"}]`},
			{"build", map[string]any{"pkg": "demo", "version": "1", "build": "py_0"}, map[string]any{"pkg": "demo", "version": "1", "build": "py_1"}, plan.ResolvedIdentity{Package: "demo", Version: "1", Revision: "py_0"}, plan.ResolvedIdentity{Package: "demo", Version: "1", Revision: "py_1"}, `[{"name":"demo","version":"1","build":"py_0","channel":"stable"}]`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				runner := &verifyStateRunner{defaultResponse: tc.response}
				methods := [2]*config.MethodCandidate{{Kind: "conda", Config: tc.first}, {Kind: "conda", Config: tc.second}}
				requireProbeStates(t, adapter, runner, [2]plan.ResolvedIdentity{tc.firstIdentity, tc.secondIdentity}, methods)
			})
		}
	})

	t.Run("package selectors", func(t *testing.T) {
		t.Run("conda", func(t *testing.T) {
			runner := &verifyStateRunner{responses: map[string]string{
				"conda list --json -n base demo-a": `[{"name":"demo-a","version":"1","build":"b0","channel":"stable"}]`,
				"conda list --json -n base demo-b": `[]`,
			}}
			methods := [2]*config.MethodCandidate{{Kind: "conda", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "conda", Config: map[string]any{"pkg": "demo-b"}}}
			for i, method := range methods {
				obs := probeObservation(t, ecosystem.NewCondaAdapter(), runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateAbsent
				}
				if got := plan.Reconcile(plan.ResolvedIdentity{Package: method.Config["pkg"].(string)}, obs).State; got != want {
					t.Fatalf("state=%s want=%s", got, want)
				}
			}
		})
		t.Run("cargo", func(t *testing.T) {
			runner := &verifyStateRunner{responses: map[string]string{
				"cargo install --list": "demo-a v1.0.0:\n    demo-a-bin\n",
			}}
			methods := [2]*config.MethodCandidate{{Kind: "cargo", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "cargo", Config: map[string]any{"pkg": "demo-b"}}}
			for i, method := range methods {
				obs := probeObservation(t, ecosystem.NewCargoAdapter(), runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateAbsent
				}
				if got := plan.Reconcile(plan.ResolvedIdentity{Package: method.Config["pkg"].(string), Version: "1.0.0"}, obs).State; got != want {
					t.Fatalf("state=%s want=%s", got, want)
				}
			}
		})
	})

	t.Run("generic pip fields", func(t *testing.T) {
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["pip"])
		t.Run("pkg", func(t *testing.T) {
			runner := &verifyStateRunner{
				responses: map[string]string{"pip show demo-a": "Name: demo-a\nVersion: 1.0\n"},
				exitCodes: map[string]int{"pip show demo-b": 1},
				stderr:    map[string]string{"pip show demo-b": "WARNING: Package(s) not found: demo-b"},
			}
			methods := [2]*config.MethodCandidate{{Kind: "pip", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "pip", Config: map[string]any{"pkg": "demo-b"}}}
			for i, method := range methods {
				obs := probeObservation(t, adapter, runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateAbsent
				}
				if got := plan.Reconcile(plan.ResolvedIdentity{Package: method.Config["pkg"].(string)}, obs).State; got != want {
					t.Fatalf("state=%s want=%s", got, want)
				}
			}
		})
		t.Run("version", func(t *testing.T) {
			runner := &verifyStateRunner{defaultResponse: "Name: demo\nVersion: 1.0\n"}
			methods := [2]*config.MethodCandidate{{Kind: "pip", Config: map[string]any{"pkg": "demo", "version": "1.0"}}, {Kind: "pip", Config: map[string]any{"pkg": "demo", "version": "2.0"}}}
			for i, method := range methods {
				obs := probeObservation(t, adapter, runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateDrifted
				}
				if got := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: method.Config["version"].(string)}, obs).State; got != want {
					t.Fatalf("state=%s want=%s", got, want)
				}
			}
		})
	})

	t.Run("pipx scope", func(t *testing.T) {
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["pipx"])
		runner := &verifyStateRunner{responses: map[string]string{
			`pipx list --json`:          `{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":"1.0"}}}}}`,
			`pipx list --json --global`: `{"pipx_spec_version":"0.1","venvs":{}}`,
		}}
		methods := [2]*config.MethodCandidate{{Kind: "pipx", Config: map[string]any{"pkg": "demo", "scope": "user"}}, {Kind: "pipx", Config: map[string]any{"pkg": "demo", "scope": "global"}}}
		for i, method := range methods {
			obs := probeObservation(t, adapter, runner, method)
			want := plan.StateSatisfied
			if i == 1 {
				want = plan.StateAbsent
			}
			if got := plan.Reconcile(plan.ResolvedIdentity{Package: "demo"}, obs).State; got != want {
				t.Fatalf("state=%s want=%s calls=%v", got, want, runner.calls)
			}
		}
	})

	t.Run("conda environment and prefix selectors", func(t *testing.T) {
		adapter := ecosystem.NewCondaAdapter()
		for _, tc := range []struct{ name, key, valueA, valueB string }{{"environment", "environment", "dev", "prod"}, {"prefix", "prefix", "/env/a", "/env/b"}} {
			t.Run(tc.name, func(t *testing.T) {
				responseA, _ := json.Marshal([]map[string]string{{"name": "demo", "version": "1", "build": "b0", "channel": "stable"}})
				responseB, _ := json.Marshal([]map[string]string{})
				var prefixA, prefixB string
				if tc.key == "prefix" {
					prefixA = "-p " + tc.valueA
					prefixB = "-p " + tc.valueB
				} else {
					prefixA = "-n " + tc.valueA
					prefixB = "-n " + tc.valueB
				}
				runner := &verifyStateRunner{responses: map[string]string{
					"conda list --json " + prefixA + " demo": string(responseA),
					"conda list --json " + prefixB + " demo": string(responseB),
				}}
				methods := [2]*config.MethodCandidate{{Kind: "conda", Config: map[string]any{"pkg": "demo", tc.key: tc.valueA}}, {Kind: "conda", Config: map[string]any{"pkg": "demo", tc.key: tc.valueB}}}
				for i, method := range methods {
					obs := probeObservation(t, adapter, runner, method)
					want := plan.StateSatisfied
					if i == 1 {
						want = plan.StateAbsent
					}
					if got := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1", Revision: "b0"}, obs).State; got != want {
						t.Fatalf("state = %s, want %s; observation=%#v calls=%v", got, want, obs, runner.calls)
					}
				}
			})
		}
	})

	t.Run("cargo root and bins", func(t *testing.T) {
		adapter := ecosystem.NewCargoAdapter()
		t.Run("root", func(t *testing.T) {
			runner := &verifyStateRunner{responses: map[string]string{
				"cargo install --list --root /root/a": "demo v1.0.0:\n    demo-bin\n",
				"cargo install --list --root /root/b": "",
			}}
			methods := [2]*config.MethodCandidate{{Kind: "cargo", Config: map[string]any{"pkg": "demo", "root": "/root/a"}}, {Kind: "cargo", Config: map[string]any{"pkg": "demo", "root": "/root/b"}}}
			for i, method := range methods {
				obs := probeObservation(t, adapter, runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateAbsent
				}
				if got := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1.0.0"}, obs).State; got != want {
					t.Fatalf("state=%s want=%s calls=%v", got, want, runner.calls)
				}
			}
		})
		t.Run("bins", func(t *testing.T) {
			runner := &verifyStateRunner{defaultResponse: "demo v1.0.0:\n    actual-bin\n"}
			methods := [2]*config.MethodCandidate{{Kind: "cargo", Config: map[string]any{"pkg": "demo", "bins": []string{"actual-bin"}}}, {Kind: "cargo", Config: map[string]any{"pkg": "demo", "bins": []string{"other-bin"}}}}
			for i, method := range methods {
				obs := probeObservation(t, adapter, runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateAbsent
				}
				if got := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1.0.0"}, obs).State; got != want {
					t.Fatalf("state=%s want=%s", got, want)
				}
			}
		})
	})

	t.Run("flatpak selectors use observed inventory identity", func(t *testing.T) {
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["flatpak"])
		const inventory = "Application ID Origin Installation Arch Branch\norg.example.App testing user x86_64 beta\norg.example.App flathub user x86_64 stable\n"
		runner := &verifyStateRunner{defaultResponse: inventory}
		stable := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{"pkg": "org.example.App", "remote": "flathub", "branch": "stable", "scope": "user"}}
		beta := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{"pkg": "org.example.App", "remote": "testing", "branch": "beta", "scope": "user"}}
		for _, method := range []*config.MethodCandidate{stable, beta} {
			obs := probeObservation(t, adapter, runner, method)
			identity := plan.ResolvedIdentity{Package: "org.example.App", Source: method.Config["remote"].(string), Revision: method.Config["branch"].(string), Scope: string(plan.ScopeUser)}
			if state := plan.Reconcile(identity, obs).State; state != plan.StateSatisfied {
				t.Fatalf("matching observed identity state=%s observation=%+v", state, obs)
			}
		}

		mismatched := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{"pkg": "org.example.App", "remote": "other", "branch": "stable", "scope": "user"}}
		mismatchRunner := &verifyStateRunner{defaultResponse: "Application ID Origin Installation Arch Branch\norg.example.App flathub user x86_64 stable\n"}
		obs := probeObservation(t, adapter, mismatchRunner, mismatched)
		want := plan.ResolvedIdentity{Package: "org.example.App", Source: "other", Revision: "stable", Scope: string(plan.ScopeUser)}
		if state := plan.Reconcile(want, obs).State; state != plan.StateDrifted {
			t.Fatalf("requested remote mismatch state=%s observation=%+v, want drifted from actual inventory identity", state, obs)
		}
	})

	t.Run("flatpak scope is observed", func(t *testing.T) {
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["flatpak"])
		runner := &verifyStateRunner{defaultResponse: "Application ID Origin Installation Arch Branch\norg.example.App flathub user x86_64 stable\n"}
		method := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{"pkg": "org.example.App", "scope": "user"}}
		obs := probeObservation(t, adapter, runner, method)
		identity := plan.ResolvedIdentity{Package: "org.example.App", Source: "flathub", Revision: "stable", Scope: string(plan.ScopeUser)}
		if state := plan.Reconcile(identity, obs).State; state != plan.StateSatisfied {
			t.Fatalf("state=%s observation=%+v", state, obs)
		}
		system := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{"pkg": "org.example.App", "scope": "system"}}
		if got := probeObservation(t, adapter, runner, system).Presence; got != plan.PresenceAbsent {
			t.Fatalf("system-only complete inventory presence=%s, want absent", got)
		}
	})

	t.Run("flatpak complete inventory proves package absence", func(t *testing.T) {
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["flatpak"])
		runner := &verifyStateRunner{defaultResponse: "Application ID Origin Installation Arch Branch\norg.example.Other flathub user x86_64 stable\n"}
		method := &config.MethodCandidate{Kind: "flatpak", Config: map[string]any{"pkg": "org.example.App"}}
		if got := probeObservation(t, adapter, runner, method).Presence; got != plan.PresenceAbsent {
			t.Fatalf("complete inventory presence=%s, want absent", got)
		}
	})

	t.Run("snap selectors", func(t *testing.T) {
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["snap"])
		runPair := func(t *testing.T, a, b map[string]any, packageA, packageB string) {
			t.Helper()
			response := "Name Version Rev Tracking Publisher Notes\ndemo 1 1 latest/stable publisher -\n"
			if requested, ok := a["branch"].(string); ok {
				response = "Name Version Rev Tracking Publisher Notes\ndemo 1 1 latest/stable/" + requested + " publisher -\n"
			}
			runner := &verifyStateRunner{defaultResponse: response}
			methods := [2]*config.MethodCandidate{{Kind: "snap", Config: a}, {Kind: "snap", Config: b}}
			for i, method := range methods {
				obs := probeObservation(t, adapter, runner, method)
				want := plan.StateSatisfied
				if i == 1 {
					want = plan.StateDrifted
				}
				intent, err := planner.BuildCandidateIntent(&config.Tool{Name: "probe"}, method)
				if err != nil {
					t.Fatalf("BuildCandidateIntent: %v", err)
				}
				pkg := packageA
				if i == 1 {
					pkg = packageB
				}
				if intent.Identity.Package != pkg {
					t.Fatalf("projected package=%q want=%q", intent.Identity.Package, pkg)
				}
				result := plan.Reconcile(intent.Identity, obs)
				if result.State != want {
					t.Fatalf("state=%s want=%s channel=%q observed=%q calls=%v", result.State, want, intent.Identity.Channel, obs.Identity.Channel, runner.calls)
				}
				if i == 1 && (len(result.Drift) != 1 || result.Drift[0].Field != plan.FieldChannel) {
					t.Fatalf("channel mismatch drift=%+v", result.Drift)
				}
			}
		}
		for _, tc := range []struct {
			name string
			a, b map[string]any
		}{
			{name: "channel", a: map[string]any{"pkg": "demo", "channel": "stable"}, b: map[string]any{"pkg": "demo", "channel": "candidate"}},
			{name: "track", a: map[string]any{"pkg": "demo", "track": "latest"}, b: map[string]any{"pkg": "demo", "track": "2.0"}},
			{name: "risk", a: map[string]any{"pkg": "demo", "track": "latest", "risk": "stable"}, b: map[string]any{"pkg": "demo", "track": "latest", "risk": "candidate"}},
			{name: "branch", a: map[string]any{"pkg": "demo", "branch": "one"}, b: map[string]any{"pkg": "demo", "branch": "two"}},
		} {
			t.Run(tc.name, func(t *testing.T) { runPair(t, tc.a, tc.b, "demo", "demo") })
		}
	})
}
