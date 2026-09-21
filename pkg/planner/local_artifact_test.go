package planner_test

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/methodkind"
	"github.com/Khorea1/depengine/pkg/planner"
)

func TestBuildCandidateIntentProjectsLocalArtifact(t *testing.T) {
	checksum := "sha256:" + strings.Repeat("a", 64)
	tool, method := candidate("demo", "local", map[string]any{
		"local_path": "vendor/demo.tar.gz",
		"checksum":   checksum,
	})
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Artifacts) != 1 {
		t.Fatalf("artifacts = %#v", p.Artifacts)
	}
	artifact := p.Artifacts[0]
	if artifact.Kind != "archive" || artifact.LocalPath != "vendor/demo.tar.gz" || artifact.Checksum != checksum || artifact.URL != "" {
		t.Fatalf("artifact = %#v", artifact)
	}
	contract, _ := methodkind.Lookup("local")
	missing, err := contract.MissingPlanCapabilities(p)
	if err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("missing capabilities = %v", methodkind.CapabilityNames(missing))
	}
}

func TestBuildCandidateIntentRejectsUnsafeLocalArtifactPaths(t *testing.T) {
	for _, localPath := range []string{"../tool", "/tmp/tool", `C:/vendor/tool`, `vendor\\tool`} {
		t.Run(localPath, func(t *testing.T) {
			tool, method := candidate("demo", "local", map[string]any{"local_path": localPath})
			if _, err := planner.BuildCandidateIntent(tool, method); err == nil {
				t.Fatalf("local_path %q unexpectedly planned", localPath)
			}
		})
	}
}

func TestBuildCandidateIntentRejectsUnsupportedLocalChecksumSemantics(t *testing.T) {
	for _, checksum := range []string{
		"sha256:auto",
		"md5:" + strings.Repeat("a", 32),
		"sha256:abc",
	} {
		t.Run(checksum, func(t *testing.T) {
			tool, method := candidate("demo", "local", map[string]any{
				"local_path": "vendor/demo",
				"checksum":   checksum,
			})
			if _, err := planner.BuildCandidateIntent(tool, method); err == nil {
				t.Fatalf("checksum %q unexpectedly planned", checksum)
			}
		})
	}
}

func TestBuildCandidateIntentRejectsUnresolvedLocalArtifactPlaceholders(t *testing.T) {
	for _, localPath := range []string{
		"vendor/bin/tool-{os_any}-{arch_any}",
		"vendor/{arch}/tool",
		"vendor/{unknown_placeholder}/tool",
	} {
		t.Run(localPath, func(t *testing.T) {
			tool, method := candidate("demo", "local", map[string]any{"local_path": localPath})
			_, err := planner.BuildCandidateIntent(tool, method)
			if err == nil || !strings.Contains(err.Error(), "must be fully resolved before planning") {
				t.Fatalf("error = %v, want unresolved-placeholder rejection", err)
			}
		})
	}
}
