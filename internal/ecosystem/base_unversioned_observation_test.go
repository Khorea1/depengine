package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// A versionless declaration still needs a trustworthy inventory: a failed
// query is not evidence that the requested package is missing.
func TestBaseAdapterUnversionedObserveUsesInventory(t *testing.T) {
	fixtures := map[string]string{
		"pip":      "Name: demo\nVersion: 2.0.0\n",
		"pipx":     `{"venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":"2.0.0"}}}}}`,
		"uv":       "demo v2.0.0\n- demo\n",
		"npm":      `{"dependencies":{"demo":{"version":"2.0.0"}}}`,
		"pnpm":     `[{"dependencies":{"demo":{"version":"2.0.0"}}}]`,
		"bun":      "└── demo@2.0.0\n",
		"gem":      "demo (2.0.0)\n",
		"yarn":     "yarn global v1.22.22\ninfo \"demo@2.0.0\" has binaries:\nDone in 0.02s.\n",
		"composer": "name : demo\nversions : * 2.0.0\n",
	}
	for kind, stdout := range fixtures {
		t.Run(kind, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs[kind])
			tool := &config.Tool{Name: "demo"}
			method := &config.MethodCandidate{Kind: kind, Config: map[string]any{"pkg": "demo"}}
			for _, tc := range []struct {
				name     string
				runner   *run.FakeRunner
				presence plan.PresenceState
				version  string
				wantErr  bool
			}{
				{name: "present", runner: &run.FakeRunner{Stdout: stdout}, presence: plan.PresencePresent, version: "2.0.0"},
				{name: "process error", runner: &run.FakeRunner{Err: errors.New("process failed")}, presence: plan.PresenceBroken, wantErr: true},
				{name: "nonzero exit", runner: &run.FakeRunner{ExitCode: 2, Stderr: "unexpected failure"}, presence: plan.PresenceBroken, wantErr: true},
				{name: "truncated inventory", runner: &run.FakeRunner{Stdout: stdout, WaitErr: errors.New("stdout capture incomplete")}, presence: plan.PresenceBroken, wantErr: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// Legacy Check's available-binary guard cannot turn an
					// failed inventory query into a confirmed absence.
					tc.runner.LookPaths = map[string]bool{Configs[kind].Binary: true}
					obs, err := adapter.Observe(context.Background(), tc.runner, tool, method)
					if obs.Presence != tc.presence || obs.Identity.Version != tc.version || (err != nil) != tc.wantErr {
						t.Fatalf("Observe() = %+v, err=%v; want presence=%s version=%q err=%v", obs, err, tc.presence, tc.version, tc.wantErr)
					}
					if tc.wantErr {
						verification := plan.Reconcile(plan.ResolvedIdentity{Package: "demo"}, obs)
						if decision, decisionErr := plan.TransitionForVerification(verification); decisionErr == nil || decision.Required {
							t.Fatalf("uncertain inventory authorized mutation: decision=%+v, err=%v", decision, decisionErr)
						}
					}
				})
			}
		})
	}
}

func TestBaseAdapterUnversionedObserveProvesAbsenceOnlyFromRecognizedInventory(t *testing.T) {
	tests := []struct {
		kind   string
		stdout string
		stderr string
		exit   int
	}{
		{kind: "pip", exit: 1, stderr: "WARNING: Package(s) not found: demo"},
		{kind: "pipx", stdout: `{"venvs":{}}`},
		{kind: "uv", stdout: "alpha v1.2.0\n- alpha\n"},
		{kind: "npm", stdout: `{"dependencies":{}}`},
		{kind: "pnpm", stdout: "[]"},
		{kind: "bun", stdout: "├── alpha@1.2.0\n"},
		{kind: "gem", stdout: "*** LOCAL GEMS ***\n"},
		{kind: "yarn", stdout: "yarn global v1.22.22\nDone in 0.02s.\n"},
		{kind: "composer", exit: 1, stderr: `Package "demo" not found.`},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs[tt.kind])
			tool := &config.Tool{Name: "demo"}
			method := &config.MethodCandidate{Kind: tt.kind, Config: map[string]any{"pkg": "demo"}}
			runner := &run.FakeRunner{Stdout: tt.stdout, Stderr: tt.stderr, ExitCode: tt.exit}
			obs, err := adapter.Observe(context.Background(), runner, tool, method)
			if err != nil || obs.Presence != plan.PresenceAbsent {
				t.Fatalf("Observe() = %+v, %v; want confirmed absence", obs, err)
			}
			// A proven-absent query must not be trusted if a concurrent
			// capture error tells us the response might be incomplete.
			runner.WaitErr = errors.New("incomplete capture")
			obs, err = adapter.Observe(context.Background(), runner, tool, method)
			if err == nil || obs.Presence != plan.PresenceBroken {
				t.Fatalf("incomplete missing inventory = %+v, %v; want broken/error", obs, err)
			}
		})
	}
}

func TestBaseAdapterVersionedObserveRejectsIncompleteCapture(t *testing.T) {
	for _, kind := range []string{"pip", "pipx", "uv", "gem", "composer", "bun", "pnpm", "yarn", "npm"} {
		t.Run(kind, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs[kind])
			method := &config.MethodCandidate{Kind: kind, Config: map[string]any{"pkg": "demo", "version": "1.0.0"}}
			runner := &run.FakeRunner{WaitErr: errors.New("partial output"), Stdout: "{}"}
			obs, err := adapter.Observe(context.Background(), runner, &config.Tool{Name: "demo"}, method)
			if obs.Presence != plan.PresenceBroken || err == nil {
				t.Fatalf("Observe() = %+v, %v; incomplete capture must fail closed", obs, err)
			}
		})
	}
}
