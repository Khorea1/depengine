package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestResolveUniversalLockDocumentUsesEachToolSourceRevision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a hermetic POSIX brew executable")
	}

	root := t.TempDir()
	brewRoot := filepath.Join(root, "taps")
	for _, name := range []string{"alpha", "beta"} {
		tapPath := filepath.Join(brewRoot, name)
		if err := os.MkdirAll(tapPath, 0o700); err != nil {
			t.Fatal(err)
		}
		initLocalGitRepository(t, tapPath)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	brew := filepath.Join(binDir, "brew")
	const fakeBrew = `#!/bin/sh
case "$1" in
  tap)
    printf 'alpha\nbeta\n'
    ;;
  tap-info)
    printf '[{"name":"%s","remote":"file://%s/%s"}]\n' "$3" "$DEPENGINE_TEST_BREW_ROOT" "$3"
    ;;
  --repo)
    printf '%s/%s\n' "$DEPENGINE_TEST_BREW_ROOT" "$2"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(brew, []byte(fakeBrew), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	cargo := filepath.Join(binDir, "cargo")
	if err := os.WriteFile(cargo, []byte("#!/bin/sh\nif [ \"$*\" = \"install --list\" ]; then exit 0; fi\nexit 1\n"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	t.Setenv("DEPENGINE_TEST_BREW_ROOT", brewRoot)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	schemaPath := filepath.Join(root, "schema.toml")
	var schemaText strings.Builder
	schemaText.WriteString("schema_version = 1\n\n")
	for _, name := range []string{"alpha", "beta"} {
		fmt.Fprintf(&schemaText, "[tools.%s]\nmethod_only = [\"cargo\"]\n[tools.%s.cargo]\npkg = %q\nversion = \"1.2.3\"\nsources = [{ kind = \"brew-tap\", name = \"%s\", url = %q }]\n\n", name, name, name, name, "file://"+filepath.Join(brewRoot, name))
	}
	if err := os.WriteFile(schemaPath, []byte(schemaText.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := config.ParseProjectSchema(schemaPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	wantRevisions := make(map[string]string, 2)
	for _, name := range []string{"alpha", "beta"} {
		wantRevisions[name] = gitHead(t, filepath.Join(brewRoot, name))
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	document, err := resolveUniversalLockDocument(context.Background(), schema, "macos", nil, schemaPath, logger, nil, universalLockCoverageNames(schema.Tools), nil)
	if err != nil {
		resolver := newInstallExecutor(installPlan{schema: schemaPath, dryRun: true}, schema, "macos", nil, time.Time{}, logger)
		t.Fatalf("%v; alpha attempts: %+v", err, resolver.ExplainTool(context.Background(), schema.Tools["alpha"], "macos"))
	}
	entries := make(map[string]struct {
		method, version, kind, name, revision string
	}, len(document.Entries))
	for _, entry := range document.Entries {
		if len(entry.Identity.Sources) != 1 {
			t.Fatalf("tool %q locked sources = %+v, want only its own Git-backed source", entry.Tool.Name, entry.Identity.Sources)
		}
		source := entry.Identity.Sources[0]
		entries[entry.Tool.Name] = struct {
			method, version, kind, name, revision string
		}{entry.Candidate.Method, entry.Identity.Version, source.Kind, source.Name, source.Revision}
	}
	for _, name := range []string{"alpha", "beta"} {
		got, ok := entries[name]
		if !ok {
			t.Fatalf("projection omitted tool %q", name)
		}
		if got.method != "cargo" || got.version != "1.2.3" || got.kind != "brew-tap" || got.name != name || got.revision != wantRevisions[name] {
			t.Fatalf("tool %q lock identity = %+v, want cargo/1.2.3 with brew-tap %s at %s", name, got, name, wantRevisions[name])
		}
	}
}

func initLocalGitRepository(t *testing.T, path string) {
	t.Helper()
	gitCommand(t, path, "init", "-q")
	gitCommand(t, path, "config", "user.email", "test@example.invalid")
	gitCommand(t, path, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(path, "README"), []byte("local fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, path, "add", "README")
	gitCommand(t, path, "commit", "-q", "-m", "fixture")
}

func gitHead(t *testing.T, path string) string {
	t.Helper()
	return gitCommand(t, path, "rev-parse", "HEAD")
}

func gitCommand(t *testing.T, path string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", path}, args...)
	result := (run.OSExecRunner{}).RunInDir(context.Background(), path, "git", args...)
	if err := run.CheckResult(result, "git "+strings.Join(commandArgs, " ")); err != nil {
		t.Fatalf("%v\n%s", err, result.Stderr)
	}
	return strings.TrimSpace(string(result.Stdout))
}

func TestCarryForwardArtifactIntegrityUsesMatchingV2ProjectionOnly(t *testing.T) {
	projectedChecksum := "sha256:" + strings.Repeat("a", 64)
	legacyChecksum := "sha256:" + strings.Repeat("b", 64)
	base := plan.Artifact{
		Kind: plan.ArtifactRaw, URL: "https://example.com/file.tar.gz", Checksum: projectedChecksum,
		ChecksumURL: "https://example.com/checksums.txt", ChecksumFileFormat: "sha256sum",
		SignatureURL: "https://example.com/file.tar.gz.sig", SigningKey: "release-key",
	}
	priorPlan := plan.New("demo", "http", true)
	priorPlan.Artifacts = []plan.Artifact{base}
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{priorPlan})
	if err != nil {
		t.Fatal(err)
	}
	previous := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{"demo/http/0": {Checksum: legacyChecksum}}}
	if err := previous.SetProjection(document); err != nil {
		t.Fatal(err)
	}
	currentPlan := func(artifact plan.Artifact) plan.ResolvedInstallPlan {
		artifact.Checksum = "sha256:auto"
		p := plan.New("demo", "http", true)
		p.Artifacts = []plan.Artifact{artifact}
		return p
	}
	matching := currentPlan(base)
	carryForwardArtifactIntegrity(&matching, previous, document)
	if got := matching.Artifacts[0].Checksum; got != projectedChecksum {
		t.Fatalf("matching v2 checksum = %q, want projected %q (not legacy %q)", got, projectedChecksum, legacyChecksum)
	}

	cases := []struct {
		name   string
		mutate func(*plan.Artifact)
	}{
		{name: "artifact kind", mutate: func(a *plan.Artifact) { a.Kind = plan.ArtifactArchive }},
		{name: "url", mutate: func(a *plan.Artifact) { a.URL = "https://example.com/changed.tar.gz" }},
		{name: "checksum URL", mutate: func(a *plan.Artifact) { a.ChecksumURL = "https://example.com/other-checksums.txt" }},
		{name: "checksum format", mutate: func(a *plan.Artifact) { a.ChecksumFileFormat = "bsd" }},
		{name: "signature URL", mutate: func(a *plan.Artifact) { a.SignatureURL = "https://example.com/other.sig" }},
		{name: "signing key", mutate: func(a *plan.Artifact) { a.SigningKey = "other-key" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			artifact := base
			tc.mutate(&artifact)
			changed := currentPlan(artifact)
			carryForwardArtifactIntegrity(&changed, previous, document)
			if got := changed.Artifacts[0].Checksum; got != "sha256:auto" {
				t.Fatalf("incompatible artifact checksum = %q, want unresolved auto checksum", got)
			}
		})
	}
	localBase := plan.Artifact{
		Kind: plan.ArtifactArchive, LocalPath: "vendor/tool.tar.gz",
		SignaturePath: "vendor/tool.tar.gz.sig", SigningKey: "release-key", Checksum: projectedChecksum,
	}
	localPrior := plan.New("local-demo", "http", true)
	localPrior.Artifacts = []plan.Artifact{localBase}
	localDocument, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{localPrior})
	if err != nil {
		t.Fatal(err)
	}
	localPrevious := &lock.Lock{Version: 1}
	if err := localPrevious.SetProjection(localDocument); err != nil {
		t.Fatal(err)
	}
	localPlan := func(artifact plan.Artifact) plan.ResolvedInstallPlan {
		artifact.Checksum = "sha256:auto"
		p := plan.New("local-demo", "http", true)
		p.Artifacts = []plan.Artifact{artifact}
		return p
	}
	localCases := []struct {
		name   string
		mutate func(*plan.Artifact)
	}{
		{name: "local path", mutate: func(a *plan.Artifact) { a.LocalPath = "vendor/other.tar.gz" }},
		{name: "signature path", mutate: func(a *plan.Artifact) { a.SignaturePath = "vendor/other.tar.gz.sig" }},
	}
	for _, tc := range localCases {
		t.Run(tc.name, func(t *testing.T) {
			artifact := localBase
			tc.mutate(&artifact)
			changed := localPlan(artifact)
			carryForwardArtifactIntegrity(&changed, localPrevious, localDocument)
			if got := changed.Artifacts[0].Checksum; got != "sha256:auto" {
				t.Fatalf("incompatible local artifact checksum = %q, want unresolved auto checksum", got)
			}
		})
	}

	concrete := base
	concrete.Checksum = "sha256:" + strings.Repeat("c", 64)
	currentWithChecksum := plan.New("demo", "http", true)
	currentWithChecksum.Artifacts = []plan.Artifact{concrete}
	carryForwardArtifactIntegrity(&currentWithChecksum, previous, document)
	if got, want := currentWithChecksum.Artifacts[0].Checksum, concrete.Checksum; got != want {
		t.Fatalf("concrete current checksum = %q, want %q", got, want)
	}
}

func TestCarryForwardArtifactIntegrityDoesNotReuseV1ChecksumAcrossUnverifiableArtifactIdentity(t *testing.T) {
	const oldURL = "https://example.com/old.tar.gz"
	const newURL = "https://example.com/new.tar.gz"
	checksum := "sha256:" + strings.Repeat("a", 64)
	previousMethod := &config.MethodCandidate{Kind: "http", Label: "mirror", Config: map[string]any{"url": oldURL}}
	currentMethod := &config.MethodCandidate{Kind: "http", Label: "mirror", Config: map[string]any{"url": newURL}}
	previousTool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{previousMethod}}
	currentTool := &config.Tool{Name: "demo", Methods: []*config.MethodCandidate{currentMethod}}
	previousMethodsHash, _, err := lock.SnapshotIntentMetadata(&config.Schema{Tools: map[string]*config.Tool{"demo": previousTool}})
	if err != nil {
		t.Fatal(err)
	}
	currentMethodsHash, _, err := lock.SnapshotIntentMetadata(&config.Schema{Tools: map[string]*config.Tool{"demo": currentTool}})
	if err != nil {
		t.Fatal(err)
	}
	if previousMethodsHash["demo"] != currentMethodsHash["demo"] {
		t.Fatalf("same HTTP kind and label hashes differ: old=%q current=%q", previousMethodsHash["demo"], currentMethodsHash["demo"])
	}
	previous := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{"demo/http/0": {Checksum: checksum}}, MethodsHash: previousMethodsHash}
	currentPlan := plan.New("demo", "http", true)
	currentPlan.Artifacts = []plan.Artifact{{URL: newURL, Checksum: "sha256:auto"}}
	carryForwardArtifactIntegrity(&currentPlan, previous, plan.LockDocument{})
	if got := currentPlan.Artifacts[0].Checksum; got != "sha256:auto" {
		t.Fatalf("v1 checksum carried from artifact %q to %q: got %q, want unresolved checksum", oldURL, newURL, got)
	}
}
func TestUniversalLockIntentDriftRequiresRetainedMetadata(t *testing.T) {
	makeDocument := func(name string) plan.LockDocument {
		resolved := plan.New(name, "http", true)
		resolved.Artifacts = []plan.Artifact{{URL: "https://example.com/" + name, Checksum: "sha256:" + strings.Repeat("a", 64)}}
		document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{resolved})
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	methods := map[string]string{"demo": "current"}
	document := makeDocument("demo")
	missingMethods, err := lock.NewUniversal(document, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !universalLockIntentDrifted(missingMethods, document, "demo", methods, nil) {
		t.Fatal("retained projection with missing methods_hash did not require refresh")
	}

	withMethods, err := lock.NewUniversal(document, methods, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentSource := map[string]string{"demo/native/0": "current-source"}
	if !universalLockIntentDrifted(withMethods, document, "demo", methods, currentSource) {
		t.Fatal("retained projection with missing source_hash did not require refresh")
	}
	withMethods.SourceHash = map[string]string{"demo/native/0": "old-source"}
	if !universalLockIntentDrifted(withMethods, document, "demo", methods, currentSource) {
		t.Fatal("changed source_hash did not require refresh")
	}
	if !universalLockIntentDrifted(withMethods, document, "demo", methods, nil) {
		t.Fatal("removed source identity did not require refresh")
	}

	withMethods.SourceHash = map[string]string{"demo-extra/native/0": "unrelated"}
	if universalLockIntentDrifted(withMethods, document, "demo", methods, nil) {
		t.Fatal("similar-prefix tool source hash triggered refresh")
	}

	slashDocument := makeDocument("group/demo")
	slashLock, err := lock.NewUniversal(slashDocument, map[string]string{"group/demo": "current"}, map[string]string{"group/demo/native/0": "old"})
	if err != nil {
		t.Fatal(err)
	}
	if !universalLockIntentDrifted(slashLock, slashDocument, "group/demo", map[string]string{"group/demo": "current"}, map[string]string{"group/demo/native/0": "new"}) {
		t.Fatal("source identity for slash-containing tool name did not trigger refresh")
	}

	if universalLockIntentDrifted(withMethods, makeDocument("other"), "demo", methods, nil) {
		t.Fatal("tool absent from previous projection was conflated with missing retained metadata")
	}
}
