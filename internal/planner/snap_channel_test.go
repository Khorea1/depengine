package planner_test

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
)

func TestSnapIntentPreservesCanonicalChannel(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cfg    map[string]any
		want   string
		branch string
	}{
		{name: "unspecified", cfg: map[string]any{"pkg": "demo"}},
		{name: "risk shorthand", cfg: map[string]any{"pkg": "demo", "channel": "beta"}, want: "latest/beta"},
		{name: "track default risk", cfg: map[string]any{"pkg": "demo", "track": "22"}, want: "22/stable"},
		{name: "risk default track", cfg: map[string]any{"pkg": "demo", "risk": "edge"}, want: "latest/edge"},
		{name: "branch alone", cfg: map[string]any{"pkg": "demo", "branch": "hotfix"}, want: "latest/stable/hotfix", branch: "hotfix"},
		{name: "track risk branch", cfg: map[string]any{"pkg": "demo", "track": "22", "risk": "candidate", "branch": "hotfix"}, want: "22/candidate/hotfix", branch: "hotfix"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := planner.BuildCandidateIntent(&config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: "snap", Config: tt.cfg})
			if err != nil {
				t.Fatal(err)
			}
			if intent.Identity.Channel != tt.want {
				t.Fatalf("identity channel=%q want %q", intent.Identity.Channel, tt.want)
			}
			if tt.want != "" && (intent.Identity.RequestedVersion == nil || intent.Identity.RequestedVersion.Mode != plan.VersionChannel || intent.Identity.RequestedVersion.Channel.Branch != tt.branch) {
				t.Fatalf("version intent did not preserve branch: %+v", intent.Identity.RequestedVersion)
			}
		})
	}
}
