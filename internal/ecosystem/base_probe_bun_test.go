package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestBaseAdapterBunObserveInventory(t *testing.T) {
	tests := []struct {
		name     string
		pkg      string
		stdout   string
		exitCode int
		runErr   error
		presence plan.PresenceState
		version  string
		wantErr  bool
	}{
		{name: "real two-package document recognizes local source target", pkg: "@fixture/cli-one", stdout: "/tmp/depengine-pr147-fixture-run-05/bun/global node_modules (2 installed)\n├── @fixture/cli-one@../../js/@fixture/cli-one\n└── @fixture/cli-two@../../js/@fixture/cli-two\n", presence: plan.PresenceUnknown},
		{name: "real two-package document proves absent target", pkg: "@fixture/missing", stdout: "/tmp/depengine-pr147-fixture-run-05/bun/global node_modules (2 installed)\n├── @fixture/cli-one@../../js/@fixture/cli-one\n└── @fixture/cli-two@../../js/@fixture/cli-two\n", presence: plan.PresenceAbsent},
		{name: "real registry package version is present", pkg: "luna-bun-fixture", stdout: "/tmp/depengine-pr147-bun-sandbox/global-registry node_modules (1 installed)\n└── luna-bun-fixture@0.1.0\n", presence: plan.PresencePresent, version: "0.1.0"},
		{name: "scoped target preserves reported version", pkg: "@scope/demo", stdout: "/home/user/.bun/install/global/node_modules (2 installed)\n├── other@1.0.0\n└── @scope/demo@2.0.0\n", presence: plan.PresencePresent, version: "2.0.0"},
		{name: "zero count header schema fixture proves absence", stdout: "/tmp/bun/global node_modules (0 installed)\n", presence: plan.PresenceAbsent},
		{name: "count mismatch is not complete", stdout: "/tmp/bun/global/node_modules (2 installed)\n└── other@1.0.0\n", presence: plan.PresenceUnknown},
		{name: "legacy tree target", stdout: "├── other@1.0.0\n└── demo@1.2.3\n", presence: plan.PresencePresent, version: "1.2.3"},
		{name: "legacy tree complete absence", stdout: "├── other@1.0.0\n└── another@2.0.0\n", presence: plan.PresenceAbsent},
		{name: "windows legacy backslash header fixture", stdout: "C:\\Users\\test\\.bun\\install\\global\\node_modules (0 installed)\n", presence: plan.PresenceAbsent},
		{name: "package without version", stdout: "/tmp/bun/global/node_modules (1 installed)\n└── demo@\n", presence: plan.PresenceUnknown},
		{name: "empty output", stdout: "", presence: plan.PresenceUnknown},
		{name: "bare unrelated package-like text is not a tree", stdout: "unrelated@text\n", presence: plan.PresenceUnknown},
		{name: "notification-like text is not a tree", stdout: "notification user@host\n", presence: plan.PresenceUnknown},
		{name: "unfamiliar text", stdout: "bun says maybe installed?\n", presence: plan.PresenceUnknown},
		{name: "drift preserves actual version", stdout: "/tmp/bun/global/node_modules (1 installed)\n└── demo@2.0.0\n", presence: plan.PresencePresent, version: "2.0.0"},
		{name: "truncated tree is unknown", stdout: "/tmp/bun/global/node_modules (2 installed)\n└── demo@1.2.3\n", presence: plan.PresenceUnknown},
		{name: "malformed later line invalidates earlier target", stdout: "/tmp/bun/global/node_modules (2 installed)\n├── demo@1.2.3\n└── damaged line\n", presence: plan.PresenceUnknown},
		{name: "process error", runErr: errors.New("spawn failed"), presence: plan.PresenceBroken, wantErr: true},
		{name: "nonzero exit", exitCode: 1, presence: plan.PresenceBroken, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := tt.pkg
			if pkg == "" {
				pkg = "demo"
			}
			runner := &run.FakeRunner{LookPaths: map[string]bool{"bun": true}, Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.runErr}
			method := &config.MethodCandidate{Kind: "bun", Config: map[string]any{"pkg": pkg, "version": "1.2.3"}}
			got, err := NewBaseAdapter(Configs["bun"]).Observe(context.Background(), runner, &config.Tool{Name: pkg}, method)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Observe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Presence != tt.presence || (!tt.wantErr && got.Identity.Version != tt.version) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q wantErr=%v", got, tt.presence, tt.version, tt.wantErr)
			}
		})
	}
}
