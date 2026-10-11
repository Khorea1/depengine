package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestBaseAdapterYarnObserveText(t *testing.T) {
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
		{name: "captured empty inventory is complete", stdout: "yarn global v1.22.22\nDone in 0.02s.\n", presence: plan.PresenceAbsent},
		{name: "banner without completion is unknown", stdout: "yarn global v1.22.22\n", presence: plan.PresenceUnknown},
		{name: "invalid banner is unknown", stdout: "yarn global v1.22.22\nnot an inventory\nDone in 0.02s.\n", presence: plan.PresenceUnknown},
		{name: "exact scoped package is present", pkg: "@fixture/cli-one", stdout: "yarn global v1.22.22\ninfo \"@fixture/cli-one@1.2.3\" has binaries:\n   - fixture-one\nDone in 0.07s.\n", presence: plan.PresencePresent, version: "1.2.3"},
		{name: "name substring does not match", pkg: "cli-one", stdout: "yarn global v1.22.22\ninfo \"other-cli-one@1.2.3\" has binaries:\n   - fixture-one\nDone in 0.07s.\n", presence: plan.PresenceAbsent},
		{name: "scoped drift preserves actual version", pkg: "@fixture/cli-one", stdout: "yarn global v1.22.22\ninfo \"@fixture/cli-one@2.3.4\" has binaries:\n   - fixture-one\nDone in 0.07s.\n", presence: plan.PresencePresent, version: "2.3.4"},
		{name: "invalid trailing line prevents early present", pkg: "@fixture/cli-one", stdout: "yarn global v1.22.22\ninfo \"@fixture/cli-one@1.2.3\" has binaries:\n   - fixture-one\ntruncated\nDone in 0.07s.\n", presence: plan.PresenceUnknown},
		{name: "unfamiliar text is unknown", stdout: "some other command text\n", presence: plan.PresenceUnknown},
		{name: "process error is broken", runErr: errors.New("spawn failed"), presence: plan.PresenceBroken, wantErr: true},
		{name: "nonzero exit is broken", exitCode: 1, presence: plan.PresenceBroken, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := tt.pkg
			if pkg == "" {
				pkg = "demo"
			}
			runner := &run.FakeRunner{LookPaths: map[string]bool{"yarn": true}, Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.runErr}
			method := &config.MethodCandidate{Kind: "yarn", Config: map[string]any{"pkg": pkg, "version": "1.2.3", "scope": "global"}}
			got, err := NewBaseAdapter(Configs["yarn"]).Observe(context.Background(), runner, &config.Tool{Name: pkg}, method)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Observe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Presence != tt.presence || (!tt.wantErr && got.Identity.Version != tt.version) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q", got, tt.presence, tt.version)
			}
			if len(runner.Calls) != 1 || runner.Calls[0].Name != "yarn" || len(runner.Calls[0].Args) != 3 || runner.Calls[0].Args[0] != "global" || runner.Calls[0].Args[1] != "list" || runner.Calls[0].Args[2] != "--depth=0" {
				t.Fatalf("global Observe argv = %#v", runner.Calls)
			}
		})
	}
}

func TestBaseAdapterComposerObserveText(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		stderr   string
		exitCode int
		runErr   error
		presence plan.PresenceState
		version  string
		wantErr  bool
	}{
		{name: "exact missing package diagnostic", stderr: "Package \"demo\" not found.", exitCode: 1, presence: plan.PresenceAbsent},
		{name: "different package name is not absent", stderr: "Package \"demo-extra\" not found.", exitCode: 1, presence: plan.PresenceBroken, wantErr: true},
		{name: "independent missing text is not absent", stderr: "Package \"other\" not found.\nThe requested demo was not found.", exitCode: 1, presence: plan.PresenceBroken, wantErr: true},
		{name: "successful show preserves installed version", stdout: "name : demo\nversions : * 2.0.0\n", presence: plan.PresencePresent, version: "2.0.0"},
		{name: "process error is broken", runErr: errors.New("spawn failed"), presence: plan.PresenceBroken, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{LookPaths: map[string]bool{"composer": true}, Stdout: tt.stdout, Stderr: tt.stderr, ExitCode: tt.exitCode, Err: tt.runErr}
			method := &config.MethodCandidate{Kind: "composer", Config: map[string]any{"pkg": "demo", "version": "1.0.0", "scope": "global"}}
			got, err := NewBaseAdapter(Configs["composer"]).Observe(context.Background(), runner, &config.Tool{Name: "demo"}, method)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Observe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Presence != tt.presence || (!tt.wantErr && got.Identity.Version != tt.version) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q", got, tt.presence, tt.version)
			}

			if len(runner.Calls) != 1 || runner.Calls[0].Name != "composer" || len(runner.Calls[0].Args) != 4 || runner.Calls[0].Args[0] != "global" || runner.Calls[0].Args[1] != "show" || runner.Calls[0].Args[2] != "--locked" || runner.Calls[0].Args[3] != "demo" {
				t.Fatalf("global Observe argv = %#v", runner.Calls)
			}
		})
	}
}
