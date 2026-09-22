package plan_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
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

	p.Identity.Digest = "sha256:" + strings.Repeat("0", 64)
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

	p.Artifacts[0].Checksum = "sha256:" + strings.Repeat("a", 64)
	got, err = plan.ProjectLock(p)
	if err != nil {
		t.Fatalf("ProjectLock() with checksum error: %v", err)
	}
	if got.Stability != plan.LockImmutable {
		t.Fatalf("Stability = %q, want immutable (%s)", got.Stability, got.Reason)
	}
}

func TestProjectLockRejectsCredentialBearingResolvedPlan(t *testing.T) {
	p := plan.New("private", "http", true)
	p.Identity.Version = "1.0.0"
	p.Identity.Source = "https://user:password@example.test/repo"
	p.Artifacts = []plan.Artifact{{URL: "https://example.test/tool.tar.gz", Checksum: "sha256:" + strings.Repeat("a", 64)}}

	if _, err := plan.ProjectLock(p); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("ProjectLock() error = %v, want credential rejection", err)
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
				Checksum: "sha256:" + strings.Repeat("a", 64),
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
		Kind:  "cargo-registry",
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
	if got.Identity.Sources[0].Name != "corp" || got.Identity.Sources[0].Role != plan.SourceRegistry || got.Identity.Sources[0].Kind != "cargo-registry" {
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

func TestLockProjectionMarshalDoesNotMutateInput(t *testing.T) {
	secret := "lock-mutation-secret-42"
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockUnavailable,
		Reason:    "https://user:" + secret + "@example.test/reason",
		Identity: plan.LockIdentity{
			Artifacts: []plan.LockedArtifact{{URL: "https://example.test/a?token=" + secret}},
			Sources: []plan.LockedSource{{
				Role:  plan.SourceSelection,
				URL:   "https://user:" + secret + "@example.test/source",
				Trust: &plan.SourceTrust{KeyReference: "https://example.test/key?token=" + secret},
			}},
		},
	}
	originalArtifactURL := p.Identity.Artifacts[0].URL
	originalSourceURL := p.Identity.Sources[0].URL
	originalKey := p.Identity.Sources[0].Trust.KeyReference

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("serialized lock leaked secret: %s", data)
	}
	if p.Identity.Artifacts[0].URL != originalArtifactURL || p.Identity.Sources[0].URL != originalSourceURL || p.Identity.Sources[0].Trust.KeyReference != originalKey {
		t.Fatal("MarshalJSON mutated lock projection")
	}
}

func TestVerifyResolvedPlanAgainstLockRejectsMutableReresolution(t *testing.T) {
	base := plan.New("redis", "container", true)
	base.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "7"}
	base.Identity.Version = "7"
	base.Identity.Digest = "sha256:" + strings.Repeat("a", 64)
	locked, err := plan.ProjectLock(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := locked.RequireImmutable(); err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyResolvedPlanAgainstLock(locked, base); err != nil {
		t.Fatalf("unchanged plan rejected: %v", err)
	}

	reresolved := base
	reresolved.Identity.Digest = "sha256:" + strings.Repeat("b", 64)
	if err := plan.VerifyResolvedPlanAgainstLock(locked, reresolved); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("digest re-resolution error = %v, want ErrLockMismatch", err)
	}
}

func TestVerifyResolvedPlanAgainstLockRejectsGitAndArtifactIdentityDrift(t *testing.T) {
	gitPlan := plan.New("tool", "git", true)
	gitPlan.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"}
	gitPlan.Identity.Revision = "abc123"
	gitLock, err := plan.ProjectLock(gitPlan)
	if err != nil {
		t.Fatal(err)
	}
	changedGit := gitPlan
	changedGit.Identity.Revision = "def456"
	if err := plan.VerifyResolvedPlanAgainstLock(gitLock, changedGit); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("git re-resolution error = %v, want ErrLockMismatch", err)
	}

	artifactPlan := plan.New("tool", "http", true)
	artifactPlan.Artifacts = []plan.Artifact{{URL: "https://example.test/tool.tar.gz", Checksum: "sha256:" + strings.Repeat("a", 64)}}
	artifactLock, err := plan.ProjectLock(artifactPlan)
	if err != nil {
		t.Fatal(err)
	}
	changedArtifact := artifactPlan
	changedArtifact.Artifacts = append([]plan.Artifact(nil), artifactPlan.Artifacts...)
	changedArtifact.Artifacts[0].Checksum = "sha256:" + strings.Repeat("b", 64)
	if err := plan.VerifyResolvedPlanAgainstLock(artifactLock, changedArtifact); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("artifact re-resolution error = %v, want ErrLockMismatch", err)
	}
}

func TestVerifyResolvedPlanAgainstLockRejectsUnavailableExpectedLock(t *testing.T) {
	p := plan.New("tool", "native", true)
	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyResolvedPlanAgainstLock(locked, p); !errors.Is(err, plan.ErrLockUnavailable) {
		t.Fatalf("VerifyResolvedPlanAgainstLock() = %v, want ErrLockUnavailable", err)
	}
}

func TestVerifyResolvedPlansAgainstLockRequiresExactToolSet(t *testing.T) {
	plans := []plan.ResolvedInstallPlan{gitRevisionPlan(), nativePackagePlan(), githubArtifactPlan()}
	doc, err := plan.BuildLockDocument(plans)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyResolvedPlansAgainstLock(doc, []plan.ResolvedInstallPlan{plans[2], plans[0], plans[1]}); err != nil {
		t.Fatalf("reordered equivalent plan set rejected: %v", err)
	}
	if err := plan.VerifyResolvedPlansAgainstLock(doc, plans[:2]); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("missing plan error = %v, want ErrLockMismatch", err)
	}

	extra := plan.New("extra", "local", true)
	extra.Artifacts = []plan.Artifact{{LocalPath: "vendor/extra", Checksum: "sha256:" + strings.Repeat("c", 64)}}
	withExtra := append(append([]plan.ResolvedInstallPlan(nil), plans...), extra)
	if err := plan.VerifyResolvedPlansAgainstLock(doc, withExtra); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("extra plan error = %v, want ErrLockMismatch", err)
	}

	duplicate := []plan.ResolvedInstallPlan{plans[0], plans[0], plans[2]}
	if err := plan.VerifyResolvedPlansAgainstLock(doc, duplicate); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("duplicate plan error = %v, want ErrLockMismatch", err)
	}
}

func TestLockDocumentUsesSharedFormatVersionPolicy(t *testing.T) {
	doc := plan.LockDocument{Version: plan.CurrentLockVersion + 1}
	if err := doc.Validate(); err == nil || !strings.Contains(err.Error(), "lock format version") {
		t.Fatalf("Validate() error = %v, want shared lock format-version diagnostic", err)
	}
}

func FuzzBuildLockDocumentOrderAndPurity(f *testing.F) {
	f.Add(uint8(0))
	f.Add(uint8(1))
	f.Add(uint8(5))
	f.Fuzz(func(t *testing.T, selector uint8) {
		base := []plan.ResolvedInstallPlan{gitRevisionPlan(), nativePackagePlan(), githubArtifactPlan()}
		original := append([]plan.ResolvedInstallPlan(nil), base...)
		permutations := [][]int{
			{0, 1, 2}, {0, 2, 1}, {1, 0, 2},
			{1, 2, 0}, {2, 0, 1}, {2, 1, 0},
		}
		order := permutations[int(selector)%len(permutations)]
		input := []plan.ResolvedInstallPlan{base[order[0]], base[order[1]], base[order[2]]}

		doc, err := plan.BuildLockDocument(input)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := plan.BuildLockDocument(base)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("input order changed lock bytes:\n got %s\nwant %s", got, want)
		}
		if !reflect.DeepEqual(base, original) {
			t.Fatal("BuildLockDocument mutated source plans")
		}
	})
}

func TestProjectLockCanonicalizesDigestRequestedIntent(t *testing.T) {
	digestUpper := "SHA256:" + strings.Repeat("A", 64)
	p := plan.New("tool", "container", true)
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionDigest, Value: digestUpper}
	p.Identity.Digest = digestUpper

	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "sha256:" + strings.Repeat("a", 64)
	if locked.RequestedIntent == nil || locked.RequestedIntent.Value != want {
		t.Fatalf("requested digest intent = %#v, want %q", locked.RequestedIntent, want)
	}
}

func TestVerifyResolvedPlanAgainstLockTreatsDigestIntentSpellingAsEquivalent(t *testing.T) {
	digestLower := "sha256:" + strings.Repeat("a", 64)
	digestUpper := "SHA256:" + strings.Repeat("A", 64)

	base := plan.New("tool", "container", true)
	base.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionDigest, Value: digestLower}
	base.Identity.Digest = digestLower
	locked, err := plan.ProjectLock(base)
	if err != nil {
		t.Fatal(err)
	}
	locked.RequestedIntent.Value = digestUpper // simulate equivalent legacy spelling

	if err := plan.VerifyResolvedPlanAgainstLock(locked, base); err != nil {
		t.Fatalf("equivalent digest intent caused lock mismatch: %v", err)
	}
}

func TestVerifyResolvedPlanAgainstLockTreatsTrustFingerprintSpacingAsEquivalent(t *testing.T) {
	base := plan.New("tool", "native", true)
	base.Identity.Version = "1.0.0"
	base.Sources = []plan.SourceReference{{
		Role:  plan.SourceRegistry,
		Name:  "corp",
		Trust: &plan.SourceTrust{Fingerprint: "ABCD EFGH"},
	}}
	locked, err := plan.ProjectLock(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Sources[0].Trust.Fingerprint; got != "ABCDEFGH" {
		t.Fatalf("canonical fingerprint = %q, want ABCDEFGH", got)
	}
	legacy := locked
	trust := *legacy.Identity.Sources[0].Trust
	trust.Fingerprint = "abcd efgh"
	legacy.Identity.Sources[0].Trust = &trust

	if err := plan.VerifyResolvedPlanAgainstLock(legacy, base); err != nil {
		t.Fatalf("equivalent trust fingerprint caused lock mismatch: %v", err)
	}
}

func TestVerifyResolvedPlanAgainstLockRejectsRequestedIntentChangeAtSameResolution(t *testing.T) {
	base := plan.New("tool", "git", true)
	base.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"}
	base.Identity.Revision = "abc123"
	locked, err := plan.ProjectLock(base)
	if err != nil {
		t.Fatal(err)
	}
	if locked.RequestedIntent == nil || locked.RequestedIntent.Value != "main" {
		t.Fatalf("requested intent not preserved: %#v", locked.RequestedIntent)
	}

	changed := base
	changed.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "develop"}
	changed.Identity.Revision = "abc123" // same concrete commit must not hide changed manifest intent
	if err := plan.VerifyResolvedPlanAgainstLock(locked, changed); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("intent change error = %v, want ErrLockMismatch", err)
	}
}

func TestLockProjectionValidatesRequestedIntentConsistency(t *testing.T) {
	p := plan.LockProjection{
		Version:         plan.CurrentLockVersion,
		Tool:            plan.ToolIdentity{Name: "tool"},
		Candidate:       plan.CandidateIdentity{Method: "git"},
		RequestedMode:   plan.VersionGitBranch,
		RequestedIntent: &plan.VersionIntent{Mode: plan.VersionGitTag, Value: "v1"},
		Stability:       plan.LockImmutable,
		Identity:        plan.LockIdentity{Revision: "abc123"},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("lock accepted inconsistent requested mode/intent")
	}
}

func TestImmutableLockProjectionRequiresConcreteIdentity(t *testing.T) {
	base := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "native"},
		Stability: plan.LockImmutable,
	}
	tests := []struct {
		name string
		edit func(*plan.LockProjection)
	}{
		{name: "empty identity", edit: func(*plan.LockProjection) {}},
		{name: "artifact without checksum", edit: func(p *plan.LockProjection) {
			p.Candidate.Method = "http"
			p.Identity.Artifacts = []plan.LockedArtifact{{URL: "https://example.test/tool"}}
		}},
		{name: "git branch without revision", edit: func(p *plan.LockProjection) {
			p.Candidate.Method = "git"
			p.RequestedMode = plan.VersionGitBranch
			p.RequestedIntent = &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"}
		}},
		{name: "container tag without digest", edit: func(p *plan.LockProjection) {
			p.Candidate.Method = "container"
			p.RequestedMode = plan.VersionContainerTag
			p.RequestedIntent = &plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "latest"}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.edit(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate() accepted immutable lock without a concrete pin")
			}
		})
	}
}

func TestLockProjectionRejectsUnsafePersistedIdentity(t *testing.T) {
	base := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "native"},
		Stability: plan.LockImmutable,
		Identity:  plan.LockIdentity{Version: "1.0.0"},
	}
	tests := []struct {
		name string
		edit func(*plan.LockProjection)
	}{
		{name: "tool NUL", edit: func(p *plan.LockProjection) { p.Tool.Name = "tool\x00bad" }},
		{name: "candidate whitespace", edit: func(p *plan.LockProjection) { p.Candidate.Method = " native" }},
		{name: "source credentials", edit: func(p *plan.LockProjection) { p.Identity.Source = "https://token@example.test/repo" }},
		{name: "registry query secret", edit: func(p *plan.LockProjection) { p.Identity.Registry = "https://example.test/index?token=secret" }},
		{name: "artifact credentials", edit: func(p *plan.LockProjection) {
			p.Identity.Version = ""
			p.Identity.Artifacts = []plan.LockedArtifact{{URL: "https://token@example.test/tool", Checksum: "sha256:" + strings.Repeat("a", 64)}}
		}},
		{name: "source trust NUL", edit: func(p *plan.LockProjection) {
			p.Identity.Sources = []plan.LockedSource{{Role: plan.SourceRegistry, Name: "corp", Trust: &plan.SourceTrust{Fingerprint: "ABCD\x00EF"}}}
		}},
		{name: "environment NUL", edit: func(p *plan.LockProjection) {
			p.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "prod\x00bad"}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.edit(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate() accepted unsafe persisted lock identity")
			}
		})
	}
}

func TestProjectLockDoesNotAliasPlanPointers(t *testing.T) {
	p := plan.New("tool", "native", true)
	p.Identity.Version = "1.0.0"
	p.Identity.Environment = &plan.EnvironmentTarget{Kind: plan.EnvironmentNamed, Value: "prod"}

	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Identity.Environment == nil {
		t.Fatal("projected environment is nil")
	}
	locked.Identity.Environment.Value = "changed-through-lock"
	if p.Identity.Environment.Value != "prod" {
		t.Fatalf("mutating projected lock changed source plan environment: %q", p.Identity.Environment.Value)
	}

	p.Identity.Environment.Value = "changed-through-plan"
	if locked.Identity.Environment.Value != "changed-through-lock" {
		t.Fatalf("mutating source plan changed projected lock environment: %q", locked.Identity.Environment.Value)
	}
}

func TestLockProjectionRejectsUnresolvedOrMalformedArtifactChecksums(t *testing.T) {
	base := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{{
			URL: "https://example.test/tool.tar.gz",
		}}},
	}
	for _, checksum := range []string{"garbage", "sha256:", "sha256:auto", "sha256:not-hex", "sha256:abcd", " sha256:abcd", "sha 256:" + strings.Repeat("a", 64), "-sha256:" + strings.Repeat("a", 64)} {
		t.Run(checksum, func(t *testing.T) {
			p := base
			p.Identity.Artifacts = append([]plan.LockedArtifact(nil), base.Identity.Artifacts...)
			p.Identity.Artifacts[0].Checksum = checksum
			if err := p.Validate(); err == nil {
				t.Fatalf("immutable lock accepted checksum %q", checksum)
			}
		})
	}

	p := base
	p.Identity.Artifacts = append([]plan.LockedArtifact(nil), base.Identity.Artifacts...)
	p.Identity.Artifacts[0].Checksum = "sha256:" + strings.Repeat("a", 64)
	if err := p.Validate(); err != nil {
		t.Fatalf("concrete checksum rejected: %v", err)
	}
}

func TestLockProjectionRejectsMalformedResolvedDigest(t *testing.T) {
	base := plan.LockProjection{
		Version:         plan.CurrentLockVersion,
		Tool:            plan.ToolIdentity{Name: "image"},
		Candidate:       plan.CandidateIdentity{Method: "container"},
		RequestedMode:   plan.VersionContainerTag,
		RequestedIntent: &plan.VersionIntent{Mode: plan.VersionContainerTag, Value: "latest"},
		Stability:       plan.LockImmutable,
	}
	for _, digest := range []string{"garbage", "sha256:", "sha256:auto", "sha256:not-hex", "sha256:abcd", " sha256:abcd", "sha 256:" + strings.Repeat("a", 64), ".sha256:" + strings.Repeat("a", 64)} {
		t.Run(digest, func(t *testing.T) {
			p := base
			p.Identity.Digest = digest
			if err := p.Validate(); err == nil {
				t.Fatalf("immutable lock accepted digest %q", digest)
			}
		})
	}
	p := base
	p.Identity.Digest = "sha256:" + strings.Repeat("a", 64)
	if err := p.Validate(); err != nil {
		t.Fatalf("concrete digest rejected: %v", err)
	}
}

func TestLockProjectionRequiresCanonicalSourceOrderAndUniqueness(t *testing.T) {
	base := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "native"},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{
			Version: "1.0.0",
			Sources: []plan.LockedSource{
				{Role: plan.SourceRegistry, Name: "alpha", URL: "https://alpha.example.test"},
				{Role: plan.SourceRegistry, Name: "beta", URL: "https://beta.example.test"},
			},
		},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("canonical sources rejected: %v", err)
	}

	reordered := base
	reordered.Identity.Sources = []plan.LockedSource{base.Identity.Sources[1], base.Identity.Sources[0]}
	if err := reordered.Validate(); err == nil {
		t.Fatal("Validate() accepted non-canonical lock source order")
	}

	duplicate := base
	duplicate.Identity.Sources = []plan.LockedSource{base.Identity.Sources[0], base.Identity.Sources[0]}
	if err := duplicate.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate lock source identity")
	}
}

func TestLockProjectionRejectsDuplicateArtifactLocation(t *testing.T) {
	sumA := "sha256:" + strings.Repeat("a", 64)
	sumB := "sha256:" + strings.Repeat("b", 64)
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{
			{Kind: plan.ArtifactArchive, URL: "https://example.test/tool.tar.gz", Checksum: sumA},
			{Kind: plan.ArtifactArchive, URL: "https://example.test/tool.tar.gz", Checksum: sumB},
		}},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate lock artifact location")
	}
}

func TestProjectLockCanonicalizesArtifactOrder(t *testing.T) {
	sumA := "sha256:" + strings.Repeat("a", 64)
	sumB := "sha256:" + strings.Repeat("b", 64)
	p := plan.New("tool", "http", true)
	p.Artifacts = []plan.Artifact{
		{Kind: plan.ArtifactArchive, URL: "https://example.test/z.tar.gz", Checksum: sumB},
		{Kind: plan.ArtifactArchive, URL: "https://example.test/a.tar.gz", Checksum: sumA},
	}

	first, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Artifacts[0], p.Artifacts[1] = p.Artifacts[1], p.Artifacts[0]
	second, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Identity.Artifacts, second.Identity.Artifacts) {
		t.Fatalf("artifact lock identity depends on plan order:\nfirst=%+v\nsecond=%+v", first.Identity.Artifacts, second.Identity.Artifacts)
	}
	if got := first.Identity.Artifacts[0].URL; got != "https://example.test/a.tar.gz" {
		t.Fatalf("first canonical artifact = %q", got)
	}
}

func TestProjectLockPreservesNonSecretSSHUsernameIdentity(t *testing.T) {
	p := plan.New("tool", "git", true)
	p.Identity.Revision = "0123456789abcdef"
	p.Identity.Source = "ssh://git@example.test/org/repo.git"

	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Source; got != p.Identity.Source {
		t.Fatalf("locked SSH source = %q, want %q", got, p.Identity.Source)
	}

	changed := p
	changed.Identity.Source = "ssh://deploy@example.test/org/repo.git"
	if err := plan.VerifyResolvedPlanAgainstLock(locked, changed); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("VerifyResolvedPlanAgainstLock() error = %v, want ErrLockMismatch", err)
	}
}

func TestProjectLockCanonicalizesPersistedURLs(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Identity.Source = "HTTPS://EXAMPLE.TEST/index?z=2&a=1"
	p.Artifacts = []plan.Artifact{{
		Kind:     plan.ArtifactArchive,
		URL:      "HTTPS://EXAMPLE.TEST/tool.tar.gz?z=2&a=1",
		Checksum: "sha256:" + strings.Repeat("a", 64),
	}}

	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Source; got != "https://example.test/index?a=1&z=2" {
		t.Fatalf("canonical source = %q", got)
	}
	if got := locked.Identity.Artifacts[0].URL; got != "https://example.test/tool.tar.gz?a=1&z=2" {
		t.Fatalf("canonical artifact URL = %q", got)
	}
}

func TestProjectLockPreservesIPv6ZoneSpelling(t *testing.T) {
	p := plan.New("tool", "git", true)
	p.Identity.Revision = "0123456789abcdef"
	p.Identity.Source = "ssh://git@[fe80::1%25ETH0]/repo.git"

	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Source; got != p.Identity.Source {
		t.Fatalf("zone-bearing source = %q, want %q", got, p.Identity.Source)
	}
}

func TestLockProjectionRejectsNonCanonicalPersistedURLs(t *testing.T) {
	base := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{{
			Kind:     plan.ArtifactArchive,
			URL:      "https://example.test/tool.tar.gz",
			Checksum: "sha256:" + strings.Repeat("a", 64),
		}},
		},
	}

	artifact := base
	artifact.Identity.Artifacts = append([]plan.LockedArtifact(nil), base.Identity.Artifacts...)
	artifact.Identity.Artifacts[0].URL = "HTTPS://EXAMPLE.TEST/tool.tar.gz"
	if err := artifact.Validate(); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("non-canonical artifact URL error = %v", err)
	}

	source := base
	source.Identity.Artifacts = append([]plan.LockedArtifact(nil), base.Identity.Artifacts...)
	source.Identity.Sources = []plan.LockedSource{{Role: plan.SourceRegistry, URL: "HTTPS://EXAMPLE.TEST/index"}}
	if err := source.Validate(); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("non-canonical source URL error = %v", err)
	}

	identity := base
	identity.Identity.Artifacts = append([]plan.LockedArtifact(nil), base.Identity.Artifacts...)
	identity.Identity.Registry = "HTTPS://EXAMPLE.TEST/index"
	if err := identity.Validate(); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("non-canonical registry URL error = %v", err)
	}
}

func TestLockProjectionRejectsNonCanonicalArtifactOrder(t *testing.T) {
	sumA := "sha256:" + strings.Repeat("a", 64)
	sumB := "sha256:" + strings.Repeat("b", 64)
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{
			{Kind: plan.ArtifactArchive, URL: "https://example.test/z.tar.gz", Checksum: sumB},
			{Kind: plan.ArtifactArchive, URL: "https://example.test/a.tar.gz", Checksum: sumA},
		}},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "canonical order") {
		t.Fatalf("Validate() error = %v, want canonical artifact order rejection", err)
	}
}

func TestProjectLockStripsSensitiveURLFragmentParameters(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Identity = plan.ResolvedIdentity{Version: "1.0.0"}
	// A credential-bearing resolved plan is intentionally invalid. Construct the
	// persisted projection directly to exercise its defense-in-depth sanitizer.
	projection := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      p.Tool,
		Candidate: p.Candidate,
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Version: "1.0.0", Artifacts: []plan.LockedArtifact{{
			Kind:     plan.ArtifactRaw,
			URL:      "https://example.test/tool#access_token=secret&section=install",
			Checksum: "sha256:" + strings.Repeat("a", 64),
		}}},
	}
	data, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("lock JSON leaked fragment secret: %s", data)
	}
	if !strings.Contains(string(data), "section%3Dinstall") && !strings.Contains(string(data), "section=install") {
		t.Fatalf("lock JSON lost benign fragment identity: %s", data)
	}
}

func TestLockSerializationPreservesBenignURLFragmentSpelling(t *testing.T) {
	projection := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http", Explicit: true},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Version: "1.0.0", Artifacts: []plan.LockedArtifact{{
			Kind:     plan.ArtifactRaw,
			URL:      "https://example.test/tool#v1.2.3",
			Checksum: "sha256:" + strings.Repeat("a", 64),
		}}},
	}
	data, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "#v1.2.3") {
		t.Fatalf("lock JSON rewrote benign fragment: %s", data)
	}
}

func TestProjectLockPinsArtifactSignatureURL(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Artifacts = []plan.Artifact{{
		Kind:         plan.ArtifactArchive,
		URL:          "HTTPS://EXAMPLE.TEST/tool.tar.gz?z=2&a=1",
		Checksum:     "sha256:" + strings.Repeat("a", 64),
		SignatureURL: "HTTPS://EXAMPLE.TEST/tool.tar.gz.sig?z=2&a=1",
	}}

	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Artifacts[0].SignatureURL; got != "https://example.test/tool.tar.gz.sig?a=1&z=2" {
		t.Fatalf("canonical signature URL = %q", got)
	}

	changed := p
	changed.Artifacts = append([]plan.Artifact(nil), p.Artifacts...)
	changed.Artifacts[0].SignatureURL = "https://example.test/other.sig?a=1&z=2"
	if err := plan.VerifyResolvedPlanAgainstLock(locked, changed); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("VerifyResolvedPlanAgainstLock() error = %v, want ErrLockMismatch", err)
	}
}

func TestLockProjectionRejectsCredentialBearingArtifactSignatureURL(t *testing.T) {
	p := plan.LockProjection{
		Version:   plan.CurrentLockVersion,
		Tool:      plan.ToolIdentity{Name: "tool"},
		Candidate: plan.CandidateIdentity{Method: "http"},
		Stability: plan.LockImmutable,
		Identity: plan.LockIdentity{Artifacts: []plan.LockedArtifact{{
			Kind:         plan.ArtifactArchive,
			URL:          "https://example.test/tool.tar.gz",
			Checksum:     "sha256:" + strings.Repeat("a", 64),
			SignatureURL: "https://token@example.test/tool.sig",
		}}},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "signature URL") {
		t.Fatalf("Validate() error = %v, want signature URL rejection", err)
	}
}

func TestLockVerificationUsesSemanticDigestAndTrustIdentity(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Identity.Digest = "SHA256:" + strings.Repeat("A", 64)
	p.Sources = []plan.SourceReference{{
		Role: plan.SourceRegistry,
		URL:  "https://example.test/index",
		Trust: &plan.SourceTrust{
			KeyReference: "HTTPS://KEYS.EXAMPLE.TEST/root.asc?z=2&a=1",
			Fingerprint:  strings.Repeat("AB", 20),
		},
	}}
	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := locked.Identity.Digest, "sha256:"+strings.Repeat("a", 64); got != want {
		t.Fatalf("canonical digest = %q, want %q", got, want)
	}
	if got, want := locked.Identity.Sources[0].Trust.KeyReference, "https://keys.example.test/root.asc?a=1&z=2"; got != want {
		t.Fatalf("canonical trust key = %q, want %q", got, want)
	}

	legacy := locked
	legacy.Identity.Digest = "SHA256:" + strings.Repeat("A", 64)
	legacy.Identity.Sources = append([]plan.LockedSource(nil), locked.Identity.Sources...)
	trust := *legacy.Identity.Sources[0].Trust
	trust.KeyReference = "HTTPS://KEYS.EXAMPLE.TEST/root.asc?z=2&a=1"
	trust.Fingerprint = strings.ToLower(trust.Fingerprint)
	legacy.Identity.Sources[0].Trust = &trust
	// Older/manual lock projections may contain equivalent non-canonical trust
	// spelling. Verification compares semantic identity instead of reporting
	// drift solely because of URL/fingerprint casing.
	if err := plan.VerifyResolvedPlanAgainstLock(legacy, p); err != nil {
		t.Fatalf("semantic equivalent lock rejected: %v", err)
	}
}

func TestLockVerificationTreatsChecksumHexCaseAsEquivalent(t *testing.T) {
	p := plan.New("tool", "http", true)
	p.Artifacts = []plan.Artifact{{
		Kind:     plan.ArtifactArchive,
		URL:      "https://example.test/tool.tar.gz",
		Checksum: "SHA256:" + strings.Repeat("A", 64),
	}}
	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Artifacts[0].Checksum; got != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("canonical checksum = %q", got)
	}
	legacy := locked
	legacy.Identity.Artifacts = append([]plan.LockedArtifact(nil), locked.Identity.Artifacts...)
	legacy.Identity.Artifacts[0].Checksum = "SHA256:" + strings.Repeat("A", 64)
	if err := plan.VerifyResolvedPlanAgainstLock(legacy, p); err != nil {
		t.Fatalf("checksum casing caused false mismatch: %v", err)
	}
}

func TestLockProjectionRejectsMalformedURLLikeIdentityReferences(t *testing.T) {
	for field, value := range map[string]string{"source": "https://", "registry": "https:///registry"} {
		t.Run(field, func(t *testing.T) {
			p := plan.LockProjection{
				Version:   plan.CurrentLockVersion,
				Tool:      plan.ToolIdentity{Name: "tool"},
				Candidate: plan.CandidateIdentity{Method: "native"},
				Stability: plan.LockImmutable,
				Identity:  plan.LockIdentity{Version: "1.0.0"},
			}
			if field == "source" {
				p.Identity.Source = value
			} else {
				p.Identity.Registry = value
			}
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate() accepted malformed %s URL", field)
			}
		})
	}
}

func TestLockProjectionRejectsIntentWithoutRequestedMode(t *testing.T) {
	p := plan.LockProjection{
		Version:         plan.CurrentLockVersion,
		Tool:            plan.ToolIdentity{Name: "tool"},
		Candidate:       plan.CandidateIdentity{Method: "native"},
		RequestedIntent: &plan.VersionIntent{Mode: plan.VersionExact, Value: "1.0.0"},
		Stability:       plan.LockImmutable,
		Identity:        plan.LockIdentity{Version: "1.0.0"},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "requires requested_mode") {
		t.Fatalf("Validate() error = %v, want requested_mode requirement", err)
	}
}

func TestProjectLockCanonicalizesSCPStyleRemoteHostOnly(t *testing.T) {
	p := plan.New("tool", "git", true)
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitRevision, Value: "abc123"}
	p.Identity.Revision = "abc123"
	p.Identity.Source = "Deploy@GIT.EXAMPLE.TEST:Org/Repo.git"
	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := locked.Identity.Source, "Deploy@git.example.test:Org/Repo.git"; got != want {
		t.Fatalf("canonical scp-style source = %q, want %q", got, want)
	}

	equivalent := p
	equivalent.Identity.Source = "Deploy@git.example.test:Org/Repo.git"
	if err := plan.VerifyResolvedPlanAgainstLock(locked, equivalent); err != nil {
		t.Fatalf("equivalent scp-style remote rejected: %v", err)
	}

	changedPath := p
	changedPath.Identity.Source = "Deploy@git.example.test:org/repo.git"
	if err := plan.VerifyResolvedPlanAgainstLock(locked, changedPath); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("path case change error = %v, want ErrLockMismatch", err)
	}
}

func TestProjectLockCanonicalizesCaseInsensitiveSourceNameIdentity(t *testing.T) {
	p := plan.New("tool", "native", true)
	p.Identity.Version = "1.0.0"
	p.Sources = []plan.SourceReference{{Role: plan.SourceSelection, Name: "CratesIO"}}
	locked, err := plan.ProjectLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Identity.Sources[0].Name; got != "cratesio" {
		t.Fatalf("canonical source name = %q, want cratesio", got)
	}

	legacy := locked
	legacy.Identity.Sources = append([]plan.LockedSource(nil), locked.Identity.Sources...)
	legacy.Identity.Sources[0].Name = "CratesIO"
	if err := plan.VerifyResolvedPlanAgainstLock(legacy, p); err != nil {
		t.Fatalf("case-equivalent source name caused lock mismatch: %v", err)
	}
}

func TestLockProjectionRejectsRequestedDigestDifferentFromResolvedDigest(t *testing.T) {
	p := plan.LockProjection{
		Version:       plan.CurrentLockVersion,
		Tool:          plan.ToolIdentity{Name: "demo"},
		Candidate:     plan.CandidateIdentity{Method: "container", Explicit: true},
		RequestedMode: plan.VersionDigest,
		RequestedIntent: &plan.VersionIntent{
			Mode:  plan.VersionDigest,
			Value: "sha256:" + strings.Repeat("a", 64),
		},
		Stability: plan.LockImmutable,
		Identity:  plan.LockIdentity{Digest: "sha256:" + strings.Repeat("b", 64)},
	}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "requested lock digest does not match") {
		t.Fatalf("Validate() error = %v, want requested/resolved digest mismatch", err)
	}

	p.Identity.Digest = "SHA256:" + strings.Repeat("A", 64)
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() equivalent digest spelling error = %v", err)
	}
}

func TestLockDocumentEntryForPlanReturnsCompatibleImmutableResolution(t *testing.T) {
	resolved := githubArtifactPlan()
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}

	intent := resolved
	intent.Identity.Version = ""
	intent.Identity.Revision = ""
	intent.Artifacts = nil

	entry, err := doc.EntryForPlan(intent)
	if err != nil {
		t.Fatalf("EntryForPlan() error: %v", err)
	}
	if got, want := entry.Identity.Version, "14.1.1"; got != want {
		t.Fatalf("locked version = %q, want %q", got, want)
	}
	if got, want := entry.Identity.Revision, "14.1.1"; got != want {
		t.Fatalf("locked revision = %q, want %q", got, want)
	}
	if len(entry.Identity.Artifacts) != 1 || entry.Identity.Artifacts[0].Checksum == "" {
		t.Fatalf("locked artifact = %+v, want concrete artifact checksum", entry.Identity.Artifacts)
	}

	entry.Identity.Artifacts[0].URL = "https://mutated.invalid/tool.tar.gz"
	entry.RequestedIntent.Value = "mutated"
	if doc.Entries[0].Identity.Artifacts[0].URL == entry.Identity.Artifacts[0].URL {
		t.Fatal("EntryForPlan returned artifact storage aliased with lock document")
	}
	if doc.Entries[0].RequestedIntent.Value == entry.RequestedIntent.Value {
		t.Fatal("EntryForPlan returned requested intent aliased with lock document")
	}
}

func TestLockDocumentEntryForPlanRejectsChangedIntent(t *testing.T) {
	resolved := nativePackagePlan()
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(*plan.ResolvedInstallPlan)
	}{
		{name: "candidate", edit: func(p *plan.ResolvedInstallPlan) { p.Candidate.Method = "apt" }},
		{name: "requested version", edit: func(p *plan.ResolvedInstallPlan) {
			p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "2.0.0"}
			p.Identity.Version = "2.0.0"
		}},
		{name: "source", edit: func(p *plan.ResolvedInstallPlan) { p.Identity.Source = "other-repo" }},
		{name: "architecture", edit: func(p *plan.ResolvedInstallPlan) { p.Identity.Architecture = "arm64" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := resolved
			intent.Identity.Version = ""
			tt.edit(&intent)
			if _, err := doc.EntryForPlan(intent); !errors.Is(err, plan.ErrLockMismatch) {
				t.Fatalf("EntryForPlan() error = %v, want ErrLockMismatch", err)
			}
		})
	}
}

func TestLockDocumentEntryForPlanRejectsMissingTool(t *testing.T) {
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{nativePackagePlan()})
	if err != nil {
		t.Fatal(err)
	}
	intent := plan.New("missing", "native", true)
	if _, err := doc.EntryForPlan(intent); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("EntryForPlan() error = %v, want ErrLockMismatch", err)
	}
}

func TestLockDocumentPinnedPlanForHydratesImmutableIdentityWithoutResolution(t *testing.T) {
	resolved := nativePackagePlan()
	resolved.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionLatest}
	resolved.Identity.Version = "2.4.1"
	resolved.Sources = []plan.SourceReference{{Role: plan.SourceRegistry, Name: "stable", SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"}}}
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}
	intent := resolved
	intent.Identity.Version = ""
	intent.Artifacts = nil
	pinned, err := doc.PinnedPlanFor(intent)
	if err != nil {
		t.Fatalf("PinnedPlanFor() error: %v", err)
	}
	if pinned.Identity.Version != "2.4.1" {
		t.Fatalf("version = %q", pinned.Identity.Version)
	}
	if len(pinned.Sources) != 1 || pinned.Sources[0].SecretRef == nil || pinned.Sources[0].SecretRef.Name != "TOKEN" {
		t.Fatalf("source secret reference not preserved: %#v", pinned.Sources)
	}
	pinned.Sources[0].SecretRef.Name = "CHANGED"
	if intent.Sources[0].SecretRef.Name != "TOKEN" {
		t.Fatal("PinnedPlanFor aliased intent secret reference")
	}
	if doc.Entries[0].Identity.Version != "2.4.1" {
		t.Fatal("PinnedPlanFor mutated lock document")
	}
}

func TestLockDocumentPinnedPlanForDoesNotAliasIntentOperationalState(t *testing.T) {
	resolved := nativePackagePlan()
	resolved.Sources = []plan.SourceReference{{
		Role:      plan.SourceRegistry,
		Name:      "stable",
		Trust:     &plan.SourceTrust{Fingerprint: "ABC123"},
		SecretRef: &plan.SecretReference{Provider: "env", Name: "TOKEN"},
	}}
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}

	rollback := plan.Operation{Kind: "remove-source", Effect: plan.EffectMutation, Command: []string{"remove-source", "stable"}, ArbitraryCode: true}
	intent := resolved
	intent.Identity.Version = ""
	intent.Preparation = &plan.PreparationPlan{
		Probe: []plan.Operation{{Kind: "probe-source", Effect: plan.EffectReadOnly}},
		Prepare: []plan.PreparationMutation{{
			ID:        "source",
			Resource:  plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "stable"},
			Ownership: plan.OwnershipDepengine,
			Apply:     plan.Operation{Kind: "add-source", Effect: plan.EffectMutation, Command: []string{"add-source", "stable"}, ArbitraryCode: true},
			Rollback:  &rollback,
			Policy:    plan.RollbackSafe,
		}},
		Commit: []plan.Operation{{Kind: "commit-source", Effect: plan.EffectMutation}},
	}
	intent.Hooks = []plan.LifecycleHook{{
		ID:            "before-install",
		Transition:    plan.TransitionInstall,
		Timing:        plan.HookBefore,
		Operation:     plan.Operation{Kind: "hook", Effect: plan.EffectMutation, Command: []string{"hook", "before"}, ArbitraryCode: true},
		FailurePolicy: plan.HookFailAbort,
	}}
	intent.Ensures = []plan.EnsureAction{{
		ID:       "config",
		Resource: "config-file",
		Check:    plan.Operation{Kind: "check-config", Effect: plan.EffectReadOnly, Command: []string{"check-config"}, ArbitraryCode: true},
		Apply:    plan.Operation{Kind: "write-config", Effect: plan.EffectMutation, Command: []string{"write-config"}, ArbitraryCode: true},
	}}
	intent.SourceMutations = []plan.Operation{{Kind: "source-mutation", Effect: plan.EffectMutation, Command: []string{"source", "stable"}, ArbitraryCode: true}}
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation, Command: []string{"install", "jq"}, ArbitraryCode: true}}
	intent.OwnedPaths = []string{"/opt/depengine/jq"}
	intent.Removal.OwnedPaths = []string{"/opt/depengine/jq"}
	intent.Entrypoints = map[string]string{"jq": "/opt/depengine/jq/bin/jq"}
	intent.Secrets = []plan.SecretReference{{Provider: "env", Name: "TOKEN"}}
	intent.Prerequisites = []plan.Prerequisite{{Name: "ca-certificates", Method: "native"}}

	pinned, err := doc.PinnedPlanFor(intent)
	if err != nil {
		t.Fatalf("PinnedPlanFor() error: %v", err)
	}

	pinned.Identity.RequestedVersion.Value = "changed"
	pinned.Sources[0].Trust.Fingerprint = "CHANGED"
	pinned.Sources[0].SecretRef.Name = "CHANGED"
	pinned.Prerequisites[0].Name = "changed"
	pinned.Preparation.Probe[0].Kind = "changed"
	pinned.Preparation.Prepare[0].Apply.Command[0] = "changed"
	pinned.Preparation.Prepare[0].Rollback.Command[0] = "changed"
	pinned.Preparation.Commit[0].Kind = "changed"
	pinned.Hooks[0].Operation.Command[0] = "changed"
	pinned.Ensures[0].Check.Command[0] = "changed"
	pinned.Ensures[0].Apply.Command[0] = "changed"
	pinned.SourceMutations[0].Command[0] = "changed"
	pinned.Operations[0].Command[0] = "changed"
	pinned.OwnedPaths[0] = "/changed"
	pinned.Removal.OwnedPaths[0] = "/changed"
	pinned.Entrypoints["jq"] = "/changed"
	pinned.Secrets[0].Name = "CHANGED"

	if intent.Identity.RequestedVersion.Value != "1.7.1" {
		t.Fatal("PinnedPlanFor aliased requested version intent")
	}
	if intent.Sources[0].Trust.Fingerprint != "ABC123" || intent.Sources[0].SecretRef.Name != "TOKEN" {
		t.Fatal("PinnedPlanFor aliased source metadata")
	}
	if intent.Prerequisites[0].Name != "ca-certificates" {
		t.Fatal("PinnedPlanFor aliased prerequisites")
	}
	if intent.Preparation.Probe[0].Kind != "probe-source" || intent.Preparation.Prepare[0].Apply.Command[0] != "add-source" || intent.Preparation.Prepare[0].Rollback.Command[0] != "remove-source" || intent.Preparation.Commit[0].Kind != "commit-source" {
		t.Fatal("PinnedPlanFor aliased preparation state")
	}
	if intent.Hooks[0].Operation.Command[0] != "hook" || intent.Ensures[0].Check.Command[0] != "check-config" || intent.Ensures[0].Apply.Command[0] != "write-config" {
		t.Fatal("PinnedPlanFor aliased lifecycle operations")
	}
	if intent.SourceMutations[0].Command[0] != "source" || intent.Operations[0].Command[0] != "install" {
		t.Fatal("PinnedPlanFor aliased executable operations")
	}
	if intent.OwnedPaths[0] != "/opt/depengine/jq" || intent.Removal.OwnedPaths[0] != "/opt/depengine/jq" || intent.Entrypoints["jq"] != "/opt/depengine/jq/bin/jq" {
		t.Fatal("PinnedPlanFor aliased ownership or entrypoint state")
	}
	if intent.Secrets[0].Name != "TOKEN" {
		t.Fatal("PinnedPlanFor aliased secret reference list")
	}
}

func TestLockDocumentPinnedPlanForRejectsIntentDrift(t *testing.T) {
	resolved := nativePackagePlan()
	resolved.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionLatest}
	resolved.Identity.Version = "2.4.1"
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}
	intent := resolved
	intent.Identity.Version = ""
	intent.Identity.Architecture = "arm64"
	if _, err := doc.PinnedPlanFor(intent); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("PinnedPlanFor() error = %v, want ErrLockMismatch", err)
	}
}

func TestLockPreservesArtifactIntegritySemantics(t *testing.T) {
	resolved := plan.New("demo", "http", true)
	resolved.Identity.Version = "1.0.0"
	resolved.Artifacts = []plan.Artifact{{
		Kind:               plan.ArtifactArchive,
		URL:                "https://example.test/tool.tar.gz",
		Checksum:           "sha256:" + strings.Repeat("a", 64),
		ChecksumURL:        "https://example.test/tool.tar.gz.sha256",
		ChecksumFileFormat: "sha256sum",
		SignatureURL:       "https://example.test/tool.tar.gz.sig",
		SigningKey:         "https://example.test/release-key.asc",
	}}

	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
	if err != nil {
		t.Fatal(err)
	}
	locked := doc.Entries[0].Identity.Artifacts[0]
	if locked.ChecksumURL != resolved.Artifacts[0].ChecksumURL || locked.ChecksumFileFormat != "sha256sum" || locked.SigningKey != resolved.Artifacts[0].SigningKey {
		t.Fatalf("locked integrity metadata = %+v", locked)
	}

	intent := resolved
	intent.Identity.Version = ""
	intent.Artifacts = nil
	pinned, err := doc.PinnedPlanFor(intent)
	if err != nil {
		t.Fatalf("PinnedPlanFor() error: %v", err)
	}
	if len(pinned.Artifacts) != 1 {
		t.Fatalf("pinned artifacts = %d, want 1", len(pinned.Artifacts))
	}
	got := pinned.Artifacts[0]
	if got.ChecksumURL != resolved.Artifacts[0].ChecksumURL || got.ChecksumFileFormat != "sha256sum" || got.SigningKey != resolved.Artifacts[0].SigningKey {
		t.Fatalf("pinned integrity metadata = %+v", got)
	}

	changed := resolved
	changed.Artifacts = append([]plan.Artifact(nil), resolved.Artifacts...)
	changed.Artifacts[0].SigningKey = "https://example.test/other-key.asc"
	if err := plan.VerifyResolvedPlanAgainstLock(doc.Entries[0], changed); !errors.Is(err, plan.ErrLockMismatch) {
		t.Fatalf("VerifyResolvedPlanAgainstLock() error = %v, want ErrLockMismatch", err)
	}
}
