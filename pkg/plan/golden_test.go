package plan_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestResolvedInstallPlanGolden(t *testing.T) {
	tests := []struct {
		name string
		plan plan.ResolvedInstallPlan
	}{
		{name: "github_artifact", plan: githubArtifactPlan()},
		{name: "native_package", plan: nativePackagePlan()},
		{name: "git_revision", plan: gitRevisionPlan()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.plan.Validate(); err != nil {
				t.Fatalf("fixture plan invalid: %v", err)
			}
			got, err := json.MarshalIndent(tt.plan, "", "  ")
			if err != nil {
				t.Fatalf("MarshalIndent() error: %v", err)
			}
			got = append(got, '\n')

			path := filepath.Join("testdata", tt.name+".golden.json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("update golden: %v", err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("resolved plan differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}

func githubArtifactPlan() plan.ResolvedInstallPlan {
	p := plan.New("ripgrep", "github", true)
	p.Identity = plan.ResolvedIdentity{
		RequestedVersion: &plan.VersionIntent{Mode: plan.VersionExact, Value: "14.1.1"},
		Version:          "14.1.1",
		Revision:         "14.1.1",
		Source:           "https://github.com/BurntSushi/ripgrep",
		Architecture:     "x86_64",
		Platform:         "linux",
	}
	p.Artifacts = []plan.Artifact{{
		URL:      "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep.tar.gz",
		Checksum: "sha256:0123456789abcdef",
	}}
	p.Entrypoints = map[string]string{"rg": "/opt/depengine/ripgrep/bin/rg"}
	p.Operations = []plan.Operation{
		{Kind: "resolve-release", Description: "resolve GitHub release 14.1.1", Effect: plan.EffectReadOnly},
		{Kind: "download-artifact", Description: "download authenticated release asset", Effect: plan.EffectReadOnly},
		{Kind: "install-artifact", Description: "install resolved archive", Effect: plan.EffectMutation},
	}
	p.OwnedPaths = []string{"/opt/depengine/ripgrep"}
	p.Removal = plan.RemovalMetadata{Supported: true, OwnedPaths: []string{"/opt/depengine/ripgrep"}, Identity: "ripgrep@14.1.1"}
	p.Secrets = []plan.SecretReference{{Provider: "env", Name: "GITHUB_TOKEN"}}
	return p
}

func nativePackagePlan() plan.ResolvedInstallPlan {
	p := plan.New("jq", "native", false)
	p.Identity = plan.ResolvedIdentity{
		RequestedVersion: &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.7.1"},
		Version:          "1.7.1",
		Source:           "debian",
		Architecture:     "amd64",
		Platform:         "linux",
	}
	p.Prerequisites = []plan.Prerequisite{{Name: "apt-transport-https", Method: "native"}}
	p.SourceMutations = []plan.Operation{{Kind: "add-source", Description: "configure package source", Effect: plan.EffectMutation}}
	p.Operations = []plan.Operation{
		{Kind: "sync-index", Description: "refresh package metadata", Effect: plan.EffectMutation},
		{Kind: "install-package", Description: "install jq=1.7.1", Effect: plan.EffectMutation},
	}
	p.Entrypoints = map[string]string{"jq": "/usr/bin/jq"}
	p.Removal = plan.RemovalMetadata{Supported: true, Identity: "jq"}
	return p
}

func gitRevisionPlan() plan.ResolvedInstallPlan {
	p := plan.New("example-tool", "git", true)
	p.Identity = plan.ResolvedIdentity{
		RequestedVersion: &plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "abc123def456"},
		Revision:         "abc123def4567890",
		Source:           "https://example.test/org/tool.git",
		Platform:         "linux",
	}
	p.Operations = []plan.Operation{
		{Kind: "resolve-revision", Effect: plan.EffectReadOnly},
		{Kind: "clone", Description: "clone source", Effect: plan.EffectMutation},
		{Kind: "build", Description: "run declared build", Effect: plan.EffectMutation, Command: []string{"make", "install", "--token", "build-secret"}, ArbitraryCode: true},
	}
	p.OwnedPaths = []string{"/opt/depengine/example-tool"}
	p.Removal = plan.RemovalMetadata{Supported: true, OwnedPaths: []string{"/opt/depengine/example-tool"}, Identity: "git:abc123def4567890"}
	return p
}
