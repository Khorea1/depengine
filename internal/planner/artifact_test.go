package planner_test

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/planner"
)

func TestBuildCandidateIntentProjectsArtifact(t *testing.T) {
	tool, method := candidate("rg", "http", map[string]any{
		"url": "https://example.test/rg.tar.gz", "checksum": "sha256:" + strings.Repeat("a", 64),
		"checksum_url": "https://example.test/rg.tar.gz.sha256", "checksum_file_format": "sha256sum",
		"signature_url": "https://example.test/rg.tar.gz.sig", "signing_key": "release-key-2026",
	})
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	if len(p.Artifacts) != 1 || p.Artifacts[0].URL != "https://example.test/rg.tar.gz" {
		t.Fatalf("artifacts = %+v", p.Artifacts)
	}
	artifact := p.Artifacts[0]
	if artifact.ChecksumURL != "https://example.test/rg.tar.gz.sha256" || artifact.ChecksumFileFormat != "sha256sum" || artifact.SigningKey != "release-key-2026" {
		t.Fatalf("artifact integrity metadata = %+v", artifact)
	}
	if !p.Removal.Supported || p.Removal.Identity != "rg" {
		t.Fatalf("removal = %+v", p.Removal)
	}
}

func TestBuildCandidateIntentRejectsCredentialBearingArtifact(t *testing.T) {
	tool, method := candidate("demo", "http", map[string]any{"url": "https://user:secret@example.test/demo.tar.gz"})
	_, err := planner.BuildCandidateIntent(tool, method)
	if err == nil || !strings.Contains(err.Error(), "literal URL credentials") {
		t.Fatalf("error = %v, want credential rejection", err)
	}
}
