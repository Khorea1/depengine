package planner_test

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/planner"
)

func TestBuildCandidateIntentVersionSemantics(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		config map[string]any
		mode   plan.VersionMode
	}{
		{name: "git branch", kind: "git", config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"}, mode: plan.VersionGitBranch},
		{name: "container tag", kind: "container", config: map[string]any{"manager": "podman", "source": "example/tool", "tag": "edge"}, mode: plan.VersionContainerTag},
		{name: "snap branch", kind: "snap", config: map[string]any{"pkg": "demo", "branch": "edge-fix"}, mode: plan.VersionChannel},
		{name: "github branch selector", kind: "github", config: map[string]any{"repo": "org/tool", "asset": "tool.tar.gz", "branch": "edge"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertVersionMode(t, tt.kind, tt.config, tt.mode)
		})
	}
}

func assertVersionMode(t *testing.T, kind string, cfg map[string]any, mode plan.VersionMode) {
	t.Helper()
	tool, method := candidate("demo", kind, cfg)
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	if mode == "" && p.Identity.RequestedVersion != nil {
		t.Fatalf("requested version = %+v, want nil", p.Identity.RequestedVersion)
	}
	if mode != "" && (p.Identity.RequestedVersion == nil || p.Identity.RequestedVersion.Mode != mode) {
		t.Fatalf("requested version = %+v, want mode %q", p.Identity.RequestedVersion, mode)
	}
}
