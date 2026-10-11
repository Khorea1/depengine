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

func TestBaseAdapterPipxObserveJSON(t *testing.T) {
	tests := []struct {
		name, pkg, stdout, scope string
		exitCode                 int
		runErr                   error
		presence                 plan.PresenceState
		version                  string
		wantScope                string
		wantError                bool
	}{
		{name: "captured metadata shape finds package under different venv key", pkg: "fixture-cli-one", stdout: `{"pipx_spec_version":"0.1","venvs":{"venv-key":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"1.2.3"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
		{name: "drift preserves real metadata version", pkg: "fixture-cli-one", stdout: `{"venvs":{"fixture-cli-one":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"2.0.0"}}}}}`, presence: plan.PresencePresent, version: "2.0.0"},
		{name: "existing entry without version is unknown", pkg: "fixture-cli-one", stdout: `{"venvs":{"fixture-cli-one":{"metadata":{"main_package":{"package":"fixture-cli-one"}}}}}`, presence: plan.PresenceUnknown},
		{name: "complete empty inventory proves absence", pkg: "demo", stdout: `{"pipx_spec_version":"0.1","venvs":{}}`, presence: plan.PresenceAbsent},
		{name: "target key without version is not absent", pkg: "demo", stdout: `{"venvs":{"demo":{"metadata":{"main_package":{"package":"demo"}}}}}`, presence: plan.PresenceUnknown},
		{name: "null document is broken", pkg: "demo", stdout: `null`, presence: plan.PresenceBroken, wantError: true},
		{name: "truncated JSON is broken", pkg: "demo", stdout: `{"venvs":`, presence: plan.PresenceBroken, wantError: true},
		{name: "wrong venvs type is broken", pkg: "demo", stdout: `{"venvs":[]}`, presence: plan.PresenceBroken, wantError: true},
		{name: "null venv invalidates inventory", pkg: "demo", stdout: `{"venvs":{"other":null}}`, presence: plan.PresenceBroken, wantError: true},
		{name: "null metadata invalidates inventory", pkg: "demo", stdout: `{"venvs":{"other":{"metadata":null}}}`, presence: plan.PresenceBroken, wantError: true},
		{name: "wrong package version type invalidates inventory", pkg: "demo", stdout: `{"venvs":{"other":{"metadata":{"main_package":{"package":"other","package_version":3}}}}}`, presence: plan.PresenceBroken, wantError: true},
		{name: "spawn error is broken", pkg: "demo", runErr: errors.New("spawn failed"), presence: plan.PresenceBroken, wantError: true},
		{name: "nonzero exit is broken", pkg: "demo", exitCode: 1, presence: plan.PresenceBroken, wantError: true},
		{name: "user scope identity and argv", pkg: "fixture-cli-one", scope: "user", wantScope: string(plan.ScopeUser), stdout: `{"venvs":{"fixture-cli-one":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"1.2.3"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
		{name: "global scope identity and argv", pkg: "fixture-cli-one", scope: "global", wantScope: string(plan.ScopeSystem), stdout: `{"venvs":{"fixture-cli-one":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"1.2.3"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{LookPaths: map[string]bool{"pipx": true}, Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.runErr}
			method := &config.MethodCandidate{Kind: "pipx", Config: map[string]any{"pkg": tt.pkg, "version": "1.0.0", "scope": tt.scope}}
			got, err := NewBaseAdapter(Configs["pipx"]).Observe(context.Background(), runner, &config.Tool{Name: tt.pkg}, method)
			if (err != nil) != tt.wantError {
				t.Fatalf("Observe() error = %v, wantError %v", err, tt.wantError)
			}
			if got.Presence != tt.presence || (!tt.wantError && (got.Identity.Version != tt.version || (tt.wantScope != "" && got.Identity.Scope != tt.wantScope))) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q", got, tt.presence, tt.version)
			}
			wantArgs := []string{"list", "--json"}
			if tt.scope == "global" {
				wantArgs = append(wantArgs, "--global")
			}
			if len(runner.Calls) != 1 || runner.Calls[0].Name != "pipx" || !reflect.DeepEqual(runner.Calls[0].Args, wantArgs) {
				t.Fatalf("pipx Observe argv = %#v, want %q", runner.Calls, wantArgs)
			}
		})
	}
}
