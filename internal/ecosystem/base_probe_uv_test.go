package ecosystem

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestBaseAdapterUVObserveToolListResponses(t *testing.T) {
	tests := []struct {
		name      string
		stdout    string
		stderr    string
		exitCode  int
		runErr    error
		want      plan.PresenceState
		version   string
		wantError bool
	}{
		{name: "present exact version", stdout: "demo v1.2.3\n- cmd\n", want: plan.PresencePresent, version: "1.2.3"},
		{name: "version drift", stdout: "demo v2.0.0\n- demo\n", want: plan.PresencePresent, version: "2.0.0"},
		{name: "valid complete list without target", stdout: "alpha v1.2.0\n- alpha\n", want: plan.PresenceAbsent},
		{name: "legacy empty marker on stdout", stdout: "No tools installed\n", want: plan.PresenceAbsent},
		{name: "current empty marker on stderr", stderr: "No tools installed\n", want: plan.PresenceAbsent},
		{name: "incomplete target version", stdout: "demo v\n- cmd\n", want: plan.PresenceUnknown},
		{name: "unreadable target version", stdout: "demo unreadable\n- demo\n", want: plan.PresenceUnknown},
		{name: "bare target name", stdout: "demo\n- demo\n", want: plan.PresenceUnknown},
		{name: "target followed by invalid text", stdout: "demo v1.2.3\nunknown text\n", want: plan.PresenceUnknown},
		{name: "prerelease version remains opaque", stdout: "demo v1.2.3rc1\n- demo\n", want: plan.PresencePresent, version: "1.2.3rc1"},
		{name: "unrecognized text", stdout: "tools are managed elsewhere\n", want: plan.PresenceUnknown},
		{name: "spawn error", runErr: errors.New("uv could not start"), want: plan.PresenceBroken, wantError: true},
		{name: "nonzero after readable target version", stdout: "demo v1.2.3\n- demo\n", stderr: "uv command failed\n", exitCode: 2, want: plan.PresenceBroken, wantError: true},
		{name: "nonzero with empty marker is still a failure", stdout: "No tools installed\n", stderr: "uv command failed\n", exitCode: 2, want: plan.PresenceBroken, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs["uv"])
			tool := &config.Tool{Name: "demo"}
			method := &config.MethodCandidate{Kind: "uv", Config: map[string]any{"pkg": "demo", "version": "1.2.3"}}
			runner := &run.FakeRunner{LookPaths: map[string]bool{"uv": true}, Stdout: tt.stdout, Stderr: tt.stderr, ExitCode: tt.exitCode, Err: tt.runErr}
			observation, err := adapter.Observe(context.Background(), runner, tool, method)
			if observation.Presence != tt.want {
				t.Fatalf("Observe() presence = %q, want %q (observation=%+v, error=%v)", observation.Presence, tt.want, observation, err)
			}
			if (err != nil) != tt.wantError {
				t.Fatalf("Observe() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.version != "" && (observation.Identity.Version != tt.version || !slices.Contains(observation.KnownFields, plan.FieldVersion)) {
				t.Fatalf("Observe() identity = %+v fields=%v, want version %q known", observation.Identity, observation.KnownFields, tt.version)
			}
			if tt.name == "version drift" {
				verification := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1.2.3"}, observation)
				if verification.State != plan.StateDrifted {
					t.Fatalf("Reconcile() state = %q, want %q", verification.State, plan.StateDrifted)
				}
			}
		})
	}
}
