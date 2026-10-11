package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestNPMExactVersionObserve(t *testing.T) {
	const pkg = "@scope/tool"
	tests := []struct {
		name      string
		stdout    string
		exitCode  int
		err       error
		want      plan.PresenceState
		wantVer   string
		wantError bool
	}{
		{
			name:     "target-specific missing problem is absent",
			stdout:   "{\"name\":\"lib\",\"problems\":[\"missing: @scope/tool@1.2.3 from global packages\"]}",
			exitCode: 1,
			want:     plan.PresenceAbsent,
		},
		{
			name:      "runner error is broken",
			stdout:    "{\"name\":\"lib\"}",
			err:       errors.New("spawn failed"),
			want:      plan.PresenceBroken,
			wantError: true,
		},
		{
			name:      "missing problem for another package is not target absence",
			stdout:    "{\"name\":\"lib\",\"problems\":[\"missing: @scope/other@1.2.3 from global packages\"]}",
			exitCode:  1,
			want:      plan.PresenceBroken,
			wantError: true,
		},
		{
			name:     "version remains present when npm exits nonzero",
			stdout:   "{\"name\":\"lib\",\"dependencies\":{\"@scope/tool\":{\"version\":\"2.4.1\"}}}",
			exitCode: 1,
			want:     plan.PresencePresent,
			wantVer:  "2.4.1",
		},
		{
			name:   "empty object is valid but unrecognized",
			stdout: "{}",
			want:   plan.PresenceUnknown,
		},
		{
			name:      "truncated JSON is broken",
			stdout:    "{\"dependencies\":",
			want:      plan.PresenceBroken,
			wantError: true,
		},
		{
			name:    "scoped package version is present",
			stdout:  "{\"dependencies\":{\"@scope/tool\":{\"version\":\"3.0.0\"}}}",
			want:    plan.PresencePresent,
			wantVer: "3.0.0",
		},
		{
			name:   "listed package without version is unknown",
			stdout: "{\"dependencies\":{\"@scope/tool\":{}}}",
			want:   plan.PresenceUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewBaseAdapter(Configs["npm"])
			runner := &run.FakeRunner{Stdout: tt.stdout, ExitCode: tt.exitCode, Err: tt.err}
			tool := &config.Tool{Name: pkg}
			method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": pkg, "version": "1.0.0"}}

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
			if got.Presence == plan.PresenceUnknown && err != nil {
				t.Fatalf("unknown observation returned an error: %v", err)
			}
			if len(runner.Calls) != 1 {
				t.Fatalf("runner calls = %d, want 1", len(runner.Calls))
			}
			call := runner.Calls[0]
			if call.Name != "npm" || len(call.Args) != 5 || call.Args[0] != "ls" || call.Args[1] != "-g" || call.Args[2] != "--depth=0" || call.Args[3] != "--json" || call.Args[4] != pkg {
				t.Fatalf("npm argv = %q %q, want npm ls -g --depth=0 --json %q", call.Name, call.Args, pkg)
			}
		})
	}
}
