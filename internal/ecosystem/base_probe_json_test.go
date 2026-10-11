package ecosystem

import (
	"context"
	"errors"
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"testing"
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
			method := &config.MethodCandidate{Kind: "pnpm", Config: map[string]any{"pkg": pkg, "version": "1.0.0", "scope": "global"}}
			got, err := NewBaseAdapter(Configs["pnpm"]).Observe(context.Background(), runner, &config.Tool{Name: pkg}, method)
			if (err != nil) != tt.wantError {
				t.Fatalf("Observe() error = %v, wantError %v", err, tt.wantError)
			}
			if got.Presence != tt.presence || (!tt.wantError && got.Identity.Version != tt.version) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q wantError=%v", got, tt.presence, tt.version, tt.wantError)
			}
			if len(runner.Calls) != 1 || runner.Calls[0].Name != "pnpm" || len(runner.Calls[0].Args) != 4 || runner.Calls[0].Args[0] != "list" || runner.Calls[0].Args[1] != "-g" || runner.Calls[0].Args[2] != "--depth=0" || runner.Calls[0].Args[3] != "--json" {
				t.Fatalf("global Observe argv = %#v", runner.Calls)
			}
		})
	}
}
