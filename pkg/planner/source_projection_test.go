package planner_test

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/planner"
)

func TestBuildCandidateIntentProjectsHostSources(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{
		Kind:   "native",
		Config: map[string]any{"pkg": "demo"},
		Sources: []config.Source{
			{Kind: "apt-ppa", Name: "ppa:vendor/stable"},
			{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"},
		},
	}

	got, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("sources = %+v, want 2 host sources", got.Sources)
	}
	for i, source := range got.Sources {
		if source.Role != plan.SourceHostConfiguration {
			t.Fatalf("source %d role = %q, want %q", i, source.Role, plan.SourceHostConfiguration)
		}
	}
	if got.Sources[0].Kind != "apt-ppa" || got.Sources[1].Kind != "brew-tap" {
		t.Fatalf("source kinds not preserved: %+v", got.Sources)
	}
	contract, ok := methodkind.Lookup("native")
	if !ok {
		t.Fatal("native contract missing")
	}
	missing, err := contract.MissingPlanCapabilities(got)
	if err != nil {
		t.Fatalf("MissingPlanCapabilities() error: %v", err)
	}
	if missing != 0 {
		t.Fatalf("host-source plan unexpectedly rejected by method capability boundary: %v", methodkind.CapabilityNames(missing))
	}
}

func TestBuildCandidateIntentProjectsGitCloneURLAsSourceIdentity(t *testing.T) {
	tool, method := candidate("demo", "git", map[string]any{
		"url": "https://github.com/example/demo.git",
		"tag": "v1.2.3",
	})
	got, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity.Source != "https://github.com/example/demo.git" {
		t.Fatalf("source = %q", got.Identity.Source)
	}
	if got.Identity.RequestedVersion == nil || got.Identity.RequestedVersion.Mode != plan.VersionGitTag || got.Identity.RequestedVersion.Value != "v1.2.3" {
		t.Fatalf("requested version = %+v", got.Identity.RequestedVersion)
	}
}
