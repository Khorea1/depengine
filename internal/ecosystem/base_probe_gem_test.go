package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestBaseAdapterGemObserveVersions(t *testing.T) {
	tests := []struct {
		name, stdout, requested, version string
		exitCode                         int
		runErr                           error
		presence                         plan.PresenceState
		wantError                        bool
	}{
		{name: "existing target with whitespace-only versions is unknown", stdout: "demo ( )\n", requested: "1.2.3", presence: plan.PresenceUnknown},
		{name: "existing target with empty versions is unknown", stdout: "demo ()\n", requested: "1.2.3", presence: plan.PresenceUnknown},
		{name: "exact real version is present", stdout: "demo (1.2.3)\n", requested: "1.2.3", version: "1.2.3", presence: plan.PresencePresent},
		{name: "reported drift preserves actual version", stdout: "demo (2.0.0)\n", requested: "1.2.3", version: "2.0.0", presence: plan.PresencePresent},
		{name: "requested version among installed versions is selected", stdout: "demo (1.2.3, 2.0.0)\n", requested: "2.0.0", version: "2.0.0", presence: plan.PresencePresent},
		{name: "first installed version remains default when request is absent", stdout: "demo (1.2.3, 2.0.0)\n", requested: "9.0.0", version: "1.2.3", presence: plan.PresencePresent},
		{name: "prerelease-like version remains opaque", stdout: "demo (1.2.3.pre)\n", requested: "1.2.3", version: "1.2.3.pre", presence: plan.PresencePresent},
		{name: "empty output proves absence", requested: "1.2.3", presence: plan.PresenceAbsent},
		{name: "bare local gems marker proves absence", stdout: "*** LOCAL GEMS ***\n", requested: "1.2.3", presence: plan.PresenceAbsent},
		{name: "complete unrelated listing proves absence", stdout: "other (1.0.0)\n", requested: "1.2.3", presence: plan.PresenceAbsent},
		{name: "unrelated empty version row is unknown", stdout: "other ( )\n", requested: "1.2.3", presence: plan.PresenceUnknown},
		{name: "lone newline proves absence", stdout: "\n", requested: "1.2.3", presence: plan.PresenceAbsent},
		{name: "whitespace-only output proves absence", stdout: " \t\n ", requested: "1.2.3", presence: plan.PresenceAbsent},
		{name: "incomplete target row is unknown", stdout: "demo (1.2.3\n", requested: "1.2.3", presence: plan.PresenceUnknown},
		{name: "process error is broken", stdout: "demo (1.2.3)\n", requested: "1.2.3", runErr: errors.New("spawn failed"), presence: plan.PresenceBroken, wantError: true},
		{name: "nonzero exit is broken", stdout: "demo (1.2.3)\n", requested: "1.2.3", exitCode: 1, presence: plan.PresenceBroken, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &run.FakeRunner{LookPaths: map[string]bool{"gem": true}, Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.runErr}
			method := &config.MethodCandidate{Kind: "gem", Config: map[string]any{"pkg": "demo", "version": tt.requested}}
			got, err := NewBaseAdapter(Configs["gem"]).Observe(context.Background(), runner, &config.Tool{Name: "demo"}, method)
			if (err != nil) != tt.wantError {
				t.Fatalf("Observe() error = %v, wantError %v", err, tt.wantError)
			}
			if got.Presence != tt.presence || (!tt.wantError && got.Identity.Version != tt.version) {
				t.Fatalf("Observe() = %+v, want presence=%q version=%q", got, tt.presence, tt.version)
			}
		})
	}
}
