package planner_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
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

func TestBuildCandidateIntentProjectsHTTPSecretReference(t *testing.T) {
	tool, method := candidate("demo", "http", map[string]any{"url": "https://example.test/demo.tar.gz"})
	method.SecretRef = &config.SecretReference{Provider: "env", Name: "ARTIFACT_TOKEN"}
	method.ChecksumSecretRef = &config.SecretReference{Provider: "env", Name: "CHECKSUM_TOKEN"}
	method.SignatureSecretRef = &config.SecretReference{Provider: "env", Name: "SIGNATURE_TOKEN"}
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	want := []plan.SecretReference{
		{Provider: "env", Name: "ARTIFACT_TOKEN"},
		{Provider: "env", Name: "CHECKSUM_TOKEN"},
		{Provider: "env", Name: "SIGNATURE_TOKEN"},
	}
	if len(p.Artifacts) != 1 || p.Artifacts[0].URL != "https://example.test/demo.tar.gz" {
		t.Fatalf("artifact identity = %+v", p.Artifacts)
	}
	if len(p.Secrets) != len(want) {
		t.Fatalf("secret requirements = %+v, want %+v", p.Secrets, want)
	}
	for i := range want {
		if p.Secrets[i] != want[i] {
			t.Fatalf("secret requirements = %+v, want %+v", p.Secrets, want)
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sentinel-token-value") {
		t.Fatal("serialized static plan contains secret material")
	}
}

func TestBuildCandidateIntentRejectsInvalidHTTPSidecarSecretReference(t *testing.T) {
	tool, method := candidate("demo", "http", map[string]any{"url": "https://example.test/demo.tar.gz"})
	method.ChecksumSecretRef = &config.SecretReference{Provider: "env", Name: ""}
	if _, err := planner.BuildCandidateIntent(tool, method); err == nil || !strings.Contains(err.Error(), "checksum_secret_ref") {
		t.Fatalf("BuildCandidateIntent() error = %v, want invalid checksum_secret_ref", err)
	}

	tool, method = candidate("demo", "github", map[string]any{"repo": "example/demo", "asset": "demo.tar.gz"})
	method.SignatureSecretRef = &config.SecretReference{Provider: "env", Name: "SIGNATURE_TOKEN"}
	if _, err := planner.BuildCandidateIntent(tool, method); err == nil || !strings.Contains(err.Error(), "signature_secret_ref") {
		t.Fatalf("BuildCandidateIntent() error = %v, want unsupported signature_secret_ref", err)
	}
}

func TestBuildCandidateIntentProjectsGitHubSecretReference(t *testing.T) {
	tool, method := candidate("demo", "github", map[string]any{"repo": "example/demo", "asset": "demo.tar.gz"})
	method.SecretRef = &config.SecretReference{Provider: "env", Name: "GITHUB_TOKEN"}
	p, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	want := plan.SecretReference{Provider: "env", Name: "GITHUB_TOKEN"}
	if len(p.Secrets) != 1 || p.Secrets[0] != want {
		t.Fatalf("secret requirements = %+v, want [%+v]", p.Secrets, want)
	}
	if len(p.Operations) == 0 || p.Operations[0].Kind != "resolve-artifact" {
		t.Fatalf("operations = %+v, want artifact resolution as the first operation", p.Operations)
	}
}

func TestBuildCandidateIntentRejectsInvalidGitHubSecretReference(t *testing.T) {
	tool, method := candidate("demo", "github", map[string]any{"repo": "example/demo", "asset": "demo.tar.gz"})
	method.SecretRef = &config.SecretReference{Provider: "env", Name: ""}
	if _, err := planner.BuildCandidateIntent(tool, method); err == nil || !strings.Contains(err.Error(), "secret_ref") {
		t.Fatalf("BuildCandidateIntent() error = %v, want invalid secret_ref", err)
	}
}

func TestBuildCandidateIntentRejectsSecretReferenceOnUnsupportedMethod(t *testing.T) {
	tool, method := candidate("demo", "native", map[string]any{"pkg": "demo"})
	method.SecretRef = &config.SecretReference{Provider: "env", Name: "TOKEN"}
	if _, err := planner.BuildCandidateIntent(tool, method); err == nil || !strings.Contains(err.Error(), "secret_ref") {
		t.Fatalf("BuildCandidateIntent() error = %v, want unsupported secret_ref for native", err)
	}
}
