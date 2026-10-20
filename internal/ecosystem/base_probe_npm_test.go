package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestRegistryAliasesResolveInstalledVersionByAliasName(t *testing.T) {
	const pkg = "omp@npm:@oh-my-pi/pi-coding-agent"
	for _, tc := range []struct {
		kind   string
		stdout string
	}{
		{kind: "bun", stdout: "/home/user/.bun/install/global/node_modules (1 installed)\n└── omp@18.8.9\n"},
		{kind: "pnpm", stdout: "[{\"dependencies\":{\"omp\":{\"version\":\"18.8.9\"}}}]"},
		{kind: "yarn", stdout: "yarn global v1.22.22\ninfo \"omp@18.8.9\" has binaries:\nDone in 0.1s.\n"},
		{kind: "npm", stdout: "{\"dependencies\":{\"omp\":{\"version\":\"18.8.9\"}}}"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs[tc.kind])
			runner := &run.FakeRunner{Stdout: tc.stdout}
			tool := &config.Tool{Name: "omp"}
			method := &config.MethodCandidate{Kind: tc.kind, Config: map[string]any{"pkg": pkg}}
			got, err := adapter.InstalledVersion(context.Background(), runner, tool, method)
			if err != nil || got != "18.8.9" {
				t.Fatalf("InstalledVersion() = %q, %v; want 18.8.9", got, err)
			}
		})
	}
}

func TestNPMExactVersionObserve(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		stdout    string
		exitCode  int
		err       error
		want      plan.PresenceState
		wantVer   string
		wantError bool
	}{
		{name: "installed exact version", pkg: "demo", stdout: `{"dependencies":{"demo":{"version":"1.2.3"}}}`, want: plan.PresencePresent, wantVer: "1.2.3"},
		{name: "version drift is present", pkg: "demo", stdout: `{"dependencies":{"demo":{"version":"2.0.0"}}}`, want: plan.PresencePresent, wantVer: "2.0.0"},
		{name: "version zero is not semver-rejected", pkg: "demo", stdout: `{"dependencies":{"demo":{"version":"0"}}}`, want: plan.PresencePresent, wantVer: "0"},
		{name: "complete empty dependencies is absent", pkg: "demo", stdout: `{"dependencies":{}}`, want: plan.PresenceAbsent},
		{name: "bare name inventory with success is absent", pkg: "demo", stdout: "{\n  \"name\": \"lib\"\n}", want: plan.PresenceAbsent},
		{name: "bare name inventory with failure is broken", pkg: "demo", stdout: "{\n  \"name\": \"lib\"\n}", exitCode: 1, want: plan.PresenceBroken, wantError: true},
		{name: "null other dependency invalidates full document", pkg: "demo", stdout: `{"dependencies":{"other":null}}`, want: plan.PresenceBroken, wantError: true},
		{name: "listed package without version is unknown", pkg: "demo", stdout: `{"dependencies":{"demo":{}}}`, want: plan.PresenceUnknown},
		{name: "nonzero version and unrelated problem is broken", pkg: "demo", stdout: `{"dependencies":{"demo":{"version":"2.0.0"}},"problems":["missing: other@1.2.3 from global"]}`, exitCode: 1, want: plan.PresenceBroken, wantError: true},
		{name: "entry remains non-absent on nonzero missing problem", pkg: "demo", stdout: `{"dependencies":{"demo":{"version":"1.2.3"}},"problems":["missing: demo@1.2.3, required by root"]}`, exitCode: 1, want: plan.PresenceBroken, wantError: true},
		{name: "target missing diagnostic is absent", pkg: "demo", stdout: `{"name":"lib","problems":["missing: demo@1.2.3, required by root"]}`, exitCode: 1, want: plan.PresenceAbsent},
		{name: "incomplete target missing diagnostic is broken", pkg: "demo", stdout: `{"problems":["missing: demo@"]}`, exitCode: 1, want: plan.PresenceBroken, wantError: true},
		{name: "unrelated nonzero problem is broken", pkg: "demo", stdout: `{"name":"lib","problems":["missing: other@1.2.3 from global"]}`, exitCode: 1, want: plan.PresenceBroken, wantError: true},
		{name: "scoped package is present", pkg: "@scope/tool", stdout: `{"dependencies":{"@scope/tool":{"version":"3.0.0"}}}`, want: plan.PresencePresent, wantVer: "3.0.0"},
		{name: "npm alias inventory reports alias version", pkg: "omp@npm:@oh-my-pi/pi-coding-agent", stdout: `{"dependencies":{"omp":{"version":"18.8.9"}}}`, want: plan.PresencePresent, wantVer: "18.8.9"},
		{name: "scoped package missing diagnostic is absent", pkg: "@scope/tool", stdout: `{"problems":["missing: @scope/tool@3.0.0, required by root"]}`, exitCode: 1, want: plan.PresenceAbsent},
		{name: "empty object is broken schema", pkg: "demo", stdout: `{}`, want: plan.PresenceBroken, wantError: true},
		{name: "null document is broken schema", pkg: "demo", stdout: `null`, want: plan.PresenceBroken, wantError: true},
		{name: "null package entry is broken schema", pkg: "demo", stdout: `{"dependencies":{"demo":null}}`, want: plan.PresenceBroken, wantError: true},
		{name: "truncated JSON is broken", pkg: "demo", stdout: `{"dependencies":`, want: plan.PresenceBroken, wantError: true},
		{name: "runner error is broken", pkg: "demo", stdout: `{"dependencies":{}}`, err: errors.New("spawn failed"), want: plan.PresenceBroken, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs["npm"])
			runner := &run.FakeRunner{Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.err}
			tool := &config.Tool{Name: tt.pkg}
			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": tt.pkg, "version": "1.2.3"}}

			got, err := adapter.Observe(context.Background(), runner, tool, method)
			if (err != nil) != tt.wantError {
				t.Fatalf("Observe error = %v, wantError %t", err, tt.wantError)
			}
			if got.Presence != tt.want {
				t.Fatalf("Observe presence = %q, want %q (detail %q)", got.Presence, tt.want, got.Detail)
			}
			if got.Identity.Version != tt.wantVer {
				t.Fatalf("Observe version = %q, want %q", got.Identity.Version, tt.wantVer)
			}
		})
	}
}
