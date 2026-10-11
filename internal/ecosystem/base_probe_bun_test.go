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
		{name: "local source path is not a version", pkg: "@fixture/cli-one", stdout: "/tmp/bun/global node_modules (2 installed)\n├── @fixture/cli-one@../../js/@fixture/cli-one\n└── @fixture/cli-two@../../js/@fixture/cli-two\n", presence: plan.PresenceUnknown},
		{name: "scoped target preserves reported version", pkg: "@scope/demo", stdout: "/home/user/.bun/install/global/node_modules (2 installed)\n├── other@1.0.0\n└── @scope/demo@2.0.0\n", presence: plan.PresencePresent, version: "2.0.0"},
		{name: "header and zero entries proves absence", stdout: "/tmp/bun/global/node_modules (0 installed)\n", presence: plan.PresenceAbsent},
		{name: "count mismatch is not complete", stdout: "/tmp/bun/global/node_modules (2 installed)\n└── other@1.0.0\n", presence: plan.PresenceUnknown},
		{name: "legacy tree target", stdout: "├── other@1.0.0\n└── demo@1.2.3\n", presence: plan.PresencePresent, version: "1.2.3"},
		{name: "legacy tree complete absence", stdout: "├── other@1.0.0\n└── another@2.0.0\n", presence: plan.PresenceAbsent},
		{name: "windows node modules header", stdout: "C:\\Users\\test\\.bun\\install\\global\\node_modules (0 installed)\n", presence: plan.PresenceAbsent},
		{name: "package without version", stdout: "/tmp/bun/global/node_modules (1 installed)\n└── demo@\n", presence: plan.PresenceUnknown},
		{name: "unfamiliar text", stdout: "bun says maybe installed?\n", presence: plan.PresenceUnknown},
		{name: "drift preserves actual version", stdout: "/tmp/bun/global/node_modules (1 installed)\n└── demo@2.0.0\n", presence: plan.PresencePresent, version: "2.0.0"},
		{name: "truncated tree is unknown", stdout: "/tmp/bun/global/node_modules (2 installed)\n└── demo@1.2.3\n", presence: plan.PresenceUnknown},
		{name: "malformed later line invalidates earlier target", stdout: "/tmp/bun/global/node_modules (2 installed)\n├── demo@1.2.3\n└── damaged line\n", presence: plan.PresenceUnknown},
		{name: "process error", runErr: errors.New("spawn failed"), wantErr: true},
		{name: "nonzero exit", exitCode: 1, wantErr: true},
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
			if tt.wantErr {
				return
			}
			if got.Presence != tt.presence || got.Identity.Version != tt.version {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q", got, tt.presence, tt.version)
			}
		})
	}
}
