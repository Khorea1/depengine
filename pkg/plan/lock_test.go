package plan_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestProjectLockGitBranchPinsRevision(t *testing.T) {
	p := plan.New("tool", "git", true)
	p.Identity = plan.ResolvedIdentity{
		RequestedVersion: &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"},
		Revision:         "0123456789abcdef",
		Source:           "https://example.test/org/tool.git",
		Architecture:     "amd64",
		Platform:         "linux",
	}

	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	if got.Stability != plan.LockImmutable {
		t.Fatalf("Stability = %q, want immutable (%s)", got.Stability, got.Reason)
	}
	if got.Identity.Revision != "0123456789abcdef" {
		t.Fatalf("Revision = %q", got.Identity.Revision)
	}
	if got.RequestedMode != plan.VersionGitBranch {
		t.Fatalf("RequestedMode = %q, want git_branch", got.RequestedMode)
	}
	if got.Identity.Architecture != "amd64" || got.Identity.Platform != "linux" {
		t.Fatalf("target identity not preserved: %+v", got.Identity)
	}
}

func TestProjectLockMutableContainerTagRequiresDigest(t *testing.T) {
	p := plan.New("redis", "container", true)
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "7"}
	p.Identity.Version = "7"

	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	if got.Stability != plan.LockUnavailable {
		t.Fatalf("Stability = %q, want unavailable", got.Stability)
	}
	if !strings.Contains(got.Reason, "digest") {
		t.Fatalf("Reason = %q, want digest explanation", got.Reason)
	}
	if err := got.RequireImmutable(); !errors.Is(err, plan.ErrLockUnavailable) {
		t.Fatalf("RequireImmutable() = %v, want ErrLockUnavailable", err)
	}

	p.Identity.Digest = "sha256:0123456789abcdef"
	got, err = plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() with digest error: %v", err)
	}
	if err := got.RequireImmutable(); err != nil {
		t.Fatalf("RequireImmutable() with digest error: %v", err)
	}
}

func TestProjectLockArtifactRequiresChecksum(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Identity.Version = "1.0.0"
	p.Artifacts = []plan.Artifact{{URL: "https://example.test/tool.tar.gz"}}

	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	if got.Stability != plan.LockUnavailable || !strings.Contains(got.Reason, "checksum") {
		t.Fatalf("projection = %+v, want unavailable checksum reason", got)
	}

	p.Artifacts[0].Checksum = "sha256:abc"
	got, err = plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() with checksum error: %v", err)
	}
	if got.Stability != plan.LockImmutable {
		t.Fatalf("Stability = %q, want immutable (%s)", got.Stability, got.Reason)
	}
}

func TestProjectLockDoesNotStoreCredentials(t *testing.T) {
	p := plan.New("private", "http", true)
	p.Identity.Version = "1.0.0"
	p.Identity.Source = "https://user:password@example.test/repo?token=source-secret"
	p.Identity.Registry = "https://registry.test/index?api_key=registry-secret"
	p.Artifacts = []plan.Artifact{{
		URL:      "https://example.test/tool?sig=artifact-secret",
		Checksum: "sha256:abc",
	}}

	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"password", "source-secret", "registry-secret", "artifact-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("lock projection leaked %q: %s", secret, text)
		}
	}
}

func TestProjectLockUnresolvedManagerIsExplicitlyUnavailable(t *testing.T) {
	p := plan.New("tool", "native", true)
	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	if got.Stability != plan.LockUnavailable {
		t.Fatalf("Stability = %q, want unavailable", got.Stability)
	}
	if got.Reason == "" {
		t.Fatal("unavailable lock projection has empty reason")
	}
}

func TestLockProjectionRejectsUnknownSchemaVersion(t *testing.T) {
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion + 1,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "native"},
		Stability: plan.LockUnavailable,
		Reason:    "fixture",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted unknown lock projection version")
	}
}

func TestLockProjectionMarshalRedactsManualValues(t *testing.T) {
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockUnavailable,
		Reason:    "resolution failed at https://example.test/x?token=reason-secret",
		Identity: plan.LockIdentity{
			Source:   "https://user:source-password@example.test/repo",
			Registry: "https://registry.test/x?api_key=registry-secret",
			Artifacts: []plan.LockedArtifact{{
				URL:      "https://example.test/tool?sig=artifact-secret",
				Checksum: "sha256:abc",
			}},
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"reason-secret", "source-password", "registry-secret", "artifact-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("manual lock projection leaked %q: %s", secret, text)
		}
	}
}

func TestBuildLockDocumentIsDeterministicAndAllOrNothing(t *testing.T) {
	plans := []plan.ResolvedInstallPlan{gitRevisionPlan(), nativePackagePlan(), githubArtifactPlan()}
	doc, err := plan.BuildLockDocument(plans)
	if err != nil {
		t.Fatalf("BuildLockDocument() error: %v", err)
	}
	if got, want := len(doc.Entries), 3; got != want {
		t.Fatalf("len(Entries) = %d, want %d", got, want)
	}
	for i := 1; i < len(doc.Entries); i++ {
		if doc.Entries[i-1].Tool.Name >= doc.Entries[i].Tool.Name {
			t.Fatalf("entries not sorted: %q then %q", doc.Entries[i-1].Tool.Name, doc.Entries[i].Tool.Name)
		}
	}

	unpinnable := plan.New("zzz", "native", true)
	if _, err := plan.BuildLockDocument(append(plans, unpinnable)); !errors.Is(err, plan.ErrLockUnavailable) {
		t.Fatalf("BuildLockDocument(unpinnable) = %v, want ErrLockUnavailable", err)
	}
}

func TestBuildLockDocumentRejectsDuplicateTool(t *testing.T) {
	first := nativePackagePlan()
	second := first
	second.Candidate.Method = "apt"
	if _, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{first, second}); err == nil {
		t.Fatal("BuildLockDocument() accepted duplicate tool")
	}
}

func TestProjectLockPersistsSourceIdentityWithoutSecretReference(t *testing.T) {
	p := plan.New("private", "cargo", true)
	p.Identity.Version = "1.2.3"
	p.Sources = []plan.SourceReference{{
		Role:  plan.SourceRegistry,
		Name:  "corp",
		URL:   "https://packages.example.test/index",
		Owned: false,
		Trust: &plan.SourceTrust{Fingerprint: "SHA256:abc"},
		SecretRef: &plan.SecretReference{
			Provider: "env",
			Name:     "CORP_REGISTRY_TOKEN",
		},
	}}

	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() error: %v", err)
	}
	if len(got.Identity.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(got.Identity.Sources))
	}
	if got.Identity.Sources[0].Name != "corp" || got.Identity.Sources[0].Role != plan.SourceRegistry {
		t.Fatalf("locked source = %+v", got.Identity.Sources[0])
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	if strings.Contains(string(data), "CORP_REGISTRY_TOKEN") || strings.Contains(string(data), "secret_ref") {
		t.Fatalf("lock persisted secret reference: %s", data)
	}
}

func TestLockRejectsMachineSpecificLocalArtifactPath(t *testing.T) {
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "local", Explicit: true},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{{
			Kind:      plan.ArtifactRaw,
			LocalPath: "/home/alice/project/vendor/tool",
			Checksum:  "sha256:" + strings.Repeat("a", 64),
		}}},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted machine-specific absolute local artifact path")
	}
}

func TestProjectLockPreservesPortableLocalArtifactIdentity(t *testing.T) {
	p := plan.New("tool", "local", true)
	p.Artifacts = []plan.Artifact{{
		Kind:      plan.ArtifactRaw,
		LocalPath: "vendor/tool",
		Checksum:  "sha256:" + strings.Repeat("a", 64),
	}}
	got, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stability != plan.LockImmutable {
		t.Fatalf("stability = %q, reason=%q", got.Stability, got.Reason)
	}
	if path := got.Identity.Artifacts[0].LocalPath; path != "vendor/tool" {
		t.Fatalf("locked local path = %q", path)
	}
}
