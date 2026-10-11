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

func TestBaseAdapterPNPMObserveJSON(t *testing.T) {
	tests := []struct {
		name, pkg, stdout string
		exitCode          int
		runErr            error
		presence          plan.PresenceState
		version           string
		wantError         bool
	}{
		{name: "captured registry package is present", pkg: "@fixture/cli-one", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"@fixture/cli-one\":{\"from\":\"@fixture/cli-one\",\"version\":\"1.2.3\",\"path\":\"/tmp/pnpm/global/v11/pkg/node_modules/@fixture/cli-one\"}}}]", presence: plan.PresencePresent, version: "1.2.3", wantError: false},
		{name: "drift preserves real installed version", pkg: "@fixture/cli-one", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"@fixture/cli-one\":{\"from\":\"@fixture/cli-one\",\"version\":\"2.0.0\",\"path\":\"/tmp/pnpm/global/v11/pkg/node_modules/@fixture/cli-one\"}}}]", presence: plan.PresencePresent, version: "2.0.0", wantError: false},
		{name: "multiple roots locate target in later root", pkg: "@fixture/cli-two", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"@fixture/cli-one\":{\"version\":\"1.2.3\"}}},{\"path\":\"/tmp/pnpm/global/v11/other\",\"private\":true,\"dependencies\":{\"@fixture/cli-two\":{\"version\":\"2.3.4\"}}}]", presence: plan.PresencePresent, version: "2.3.4", wantError: false},
		{name: "valid empty array proves absence", pkg: "demo", stdout: "[]", presence: plan.PresenceAbsent, version: "", wantError: false},
		{name: "real empty root proves absence", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{}}]", presence: plan.PresenceAbsent, version: "", wantError: false},
		{name: "valid root missing target proves absence", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"other\":{\"version\":\"1.0.0\"}}}]", presence: plan.PresenceAbsent, version: "", wantError: false},
		{name: "existing entry without version is unknown", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"demo\":{}}}]", presence: plan.PresenceUnknown, version: "", wantError: false},
		{name: "existing entry with null version is unknown", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"demo\":{\"version\":null}}}]", presence: plan.PresenceUnknown, version: "", wantError: false},
		{name: "null top-level document is broken", pkg: "demo", stdout: "null", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "null root is broken", pkg: "demo", stdout: "[null]", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "root missing dependencies is broken", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true}]", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "null dependencies is broken", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":null}]", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "wrong dependencies type is broken", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":[]}]", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "null dependency entry is broken", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"other\":null}}]", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "wrong version type is broken", pkg: "demo", stdout: "[{\"path\":\"/tmp/pnpm/global/v11\",\"private\":true,\"dependencies\":{\"other\":{\"version\":3}}}]", presence: plan.PresenceBroken, version: "", wantError: true},
		{name: "malformed JSON is broken", stdout: "[{\"dependencies\":{\"demo\":{\"version\":\"1.2.3\"}}", presence: plan.PresenceBroken, wantError: true},
		{name: "process error is broken", runErr: errors.New("spawn failed"), presence: plan.PresenceBroken, wantError: true},
		{name: "nonzero exit is broken", exitCode: 1, presence: plan.PresenceBroken, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := tt.pkg
			if pkg == "" {
				pkg = "demo"
			}
			runner := &run.FakeRunner{LookPaths: map[string]bool{"pnpm": true}, Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.runErr}
			method := &config.MethodCandidate{Kind: "pnpm", Config: map[string]any{"pkg": pkg, "version": "1.2.3", "scope": "global"}}
			got, err := NewBaseAdapter(Configs["pnpm"]).Observe(context.Background(), runner, &config.Tool{Name: pkg}, method)
			if (err != nil) != tt.wantError {
				t.Fatalf("Observe() error = %v, wantError %v", err, tt.wantError)
			}
			if got.Presence != tt.presence || (!tt.wantError && got.Identity.Version != tt.version) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q wantError=%v", got, tt.presence, tt.version, tt.wantError)
			}
		})
	}
}

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
		{name: "exact venv key wins over conflicting suffix metadata", pkg: "fixture-cli-one", stdout: `{"venvs":{"fixture-cli-one":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"1.2.3"}}},"suffix":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"2.0.0"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
		{name: "ambiguous package fallback is unknown", pkg: "fixture-cli-one", stdout: `{"venvs":{"suffix-a":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"1.2.3"}}},"suffix-b":{"metadata":{"main_package":{"package":"fixture-cli-one","package_version":"2.0.0"}}}}}`, presence: plan.PresenceUnknown},
		{name: "existing entry without version is unknown", pkg: "fixture-cli-one", stdout: `{"venvs":{"fixture-cli-one":{"metadata":{"main_package":{"package":"fixture-cli-one"}}}}}`, presence: plan.PresenceUnknown},
		{name: "venv key with divergent package identity is unknown", pkg: "demo", stdout: `{"venvs":{"demo":{"metadata":{"main_package":{"package":"other","package_version":"1.2.3"}}}}}`, presence: plan.PresenceUnknown},
		{name: "venv key without package identity is unknown", pkg: "demo", stdout: `{"venvs":{"demo":{"metadata":{"main_package":{"package":"","package_version":"1.2.3"}}}}}`, presence: plan.PresenceUnknown},
		{name: "underscored request matches dashed venv key", pkg: "demo_pkg", stdout: `{"venvs":{"demo-pkg":{"metadata":{"main_package":{"package":"demo-pkg","package_version":"1.2.3"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
		{name: "dotted metadata matches dashed request", pkg: "demo-pkg", stdout: `{"venvs":{"other-key":{"metadata":{"main_package":{"package":"demo.pkg","package_version":"1.2.3"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
		{name: "mixed separators and case match canonically", pkg: "Demo__pkg", stdout: `{"venvs":{"other-key":{"metadata":{"main_package":{"package":"demo.-pkg","package_version":"1.2.3"}}}}}`, presence: plan.PresencePresent, version: "1.2.3"},
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
			method := &config.MethodCandidate{Kind: "pipx", Config: map[string]any{"pkg": tt.pkg, "version": "1.2.3", "scope": tt.scope}}
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
