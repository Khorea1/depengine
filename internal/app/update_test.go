package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
)

// writeUpdateTestSchema writes schema.toml to dir and returns its path.
func writeUpdateTestSchema(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runTestUpdate drives runUpdate in-process with hermetic flags: no manifest,
// no frozen check, no dry-run, so the resolved lock is merged and saved.
func runTestUpdate(t *testing.T, schemaPath, profile string) {
	t.Helper()
	noManifest, frozen, dryRun, verbose := true, false, false, false
	manifest, lockFlag := "", ""
	if err := runUpdate(context.Background(), &schemaPath, &manifest, &noManifest, &lockFlag, &profile, &frozen, &dryRun, &verbose); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
}
func TestUpdatePinValueShowsImmutableContainerDigest(t *testing.T) {
	const digest = "sha256:6123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got := updatePinValue(lock.ToolPin{ContainerTag: "stable", ContainerDigest: digest})
	want := "tag:stable @ " + digest
	if got != want {
		t.Fatalf("updatePinValue() = %q, want %q", got, want)
	}
}

func TestUpdatePinValueShowsImmutableGitRevision(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	got := updatePinValue(lock.ToolPin{Selector: "branch:main", Revision: revision})
	want := "branch:main @ " + revision
	if got != want {
		t.Fatalf("updatePinValue() = %q, want %q", got, want)
	}
}

// TestRunUpdatePreservesPinsAndHashesOutsideProfile is the regression test for
// `depengine update --profile` rewriting the whole lock: pins and method
// identities for tools outside the profile must survive, while tools the fresh
// resolution covers accept their new pins and (unlike install) their new
// method identity.
func TestRunUpdatePreservesPinsAndHashesOutsideProfile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	freshChecksum := "sha256:" + strings.Repeat("a", 64)
	materialized := "sha256:" + strings.Repeat("c", 64)
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.profiled]\ntags = [\"dev\"]\n\n"+
		"[tools.profiled.http]\nurl = \"https://example.com/profiled\"\nchecksum = \""+freshChecksum+"\"\n\n"+
		"[tools.profiled.native]\npkg = \"profiled\"\nsources = [{ kind = \"brew-tap\", name = \"profiled/tools\", url = \"https://example.com/profiled/tools.git\" }]\n\n"+
		"[tools.outside]\ntags = [\"other\"]\n\n"+
		"[tools.outside.http]\nurl = \"https://example.com/outside\"\nchecksum = \"sha256:auto\"\n\n"+
		"[tools.outside.native]\npkg = \"outside\"\nsources = [{ kind = \"brew-tap\", name = \"outside/tools\", url = \"https://example.com/outside/tools.git\" }]\n")

	old := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			// Composite pin: fresh resolution re-resolves the checksum but
			// skips Latest (no {latest} placeholder), so Latest must survive.
			"profiled/http/0": {Latest: "v9.9.9", Checksum: "sha256:" + strings.Repeat("b", 64)},
			// Materialized `:auto` checksum for a tool outside the profile.
			"outside/http/0": {Checksum: materialized},
		},
		MethodsHash: map[string]string{
			"profiled": "stale-profiled-identity",
			"outside":  "preserve-outside-identity",
		},
		SourceHash: map[string]string{
			"profiled/native/0": "stale-profiled-source",
			"outside/native/0":  "preserve-outside-source",
		},
	}
	lockPath := lock.DefaultPath(schemaPath)
	if err := lock.Save(lockPath, old); err != nil {
		t.Fatal(err)
	}

	runTestUpdate(t, schemaPath, "dev")

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("runUpdate did not write a lock")
	}

	// Tool absent from the fresh resolution: pin and identity preserved.
	if pin := got.Tools["outside/http/0"]; pin.Checksum != materialized {
		t.Fatalf("outside pin = %#v, want preserved materialized checksum", pin)
	}
	if h := got.MethodsHash["outside"]; h != "preserve-outside-identity" {
		t.Fatalf("outside methods_hash = %q, want preserved hash", h)
	}
	if h := got.SourceHash["outside/native/0"]; h != "preserve-outside-source" {
		t.Fatalf("outside source_hash = %q, want preserved hash", h)
	}

	// Tool present in the fresh resolution: fresh checksum wins, the
	// unresolved Latest field keeps its old value, and update accepts the
	// changed method identity instead of preserving the stale hash.
	pin := got.Tools["profiled/http/0"]
	if pin.Checksum != freshChecksum {
		t.Fatalf("profiled checksum = %q, want fresh %q", pin.Checksum, freshChecksum)
	}
	if pin.Latest != "v9.9.9" {
		t.Fatalf("profiled Latest = %q, want preserved v9.9.9", pin.Latest)
	}
	if h := got.MethodsHash["profiled"]; h == "" || h == "stale-profiled-identity" {
		t.Fatalf("profiled methods_hash = %q, want freshly computed hash (identity change accepted)", h)
	}
	if h := got.SourceHash["profiled/native/0"]; h == "" || h == "stale-profiled-source" {
		t.Fatalf("profiled source_hash = %q, want freshly computed hash (source identity change accepted)", h)
	}
}

func TestRunUpdateAcceptsRemovedSourceIdentity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.demo.http]\nurl = \"https://example.com/demo\"\nchecksum = \"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n")
	lockPath := lock.DefaultPath(schemaPath)
	old := &lock.Lock{
		Version:     1,
		Tools:       map[string]lock.ToolPin{},
		MethodsHash: map[string]string{"demo": "stale-method-identity"},
		SourceHash:  map[string]string{"demo/native/0": "stale-source-identity"},
	}
	if err := lock.Save(lockPath, old); err != nil {
		t.Fatal(err)
	}

	runTestUpdate(t, schemaPath, "")

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.SourceHash["demo/native/0"]; ok {
		t.Fatalf("SourceHash = %v, want removed source identity accepted by update", got.SourceHash)
	}
	if got.Version != lock.CurrentVersion {
		t.Fatalf("lock version = %d, want %d", got.Version, lock.CurrentVersion)
	}
	document, err := got.ProjectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Entries) != 1 || document.Entries[0].Tool.Name != "demo" {
		t.Fatalf("universal projection = %#v, want demo entry", document.Entries)
	}
}

// TestRunUpdatePreservesMaterializedAutoChecksumPin is the regression test for
// `depengine update` dropping a materialized `sha256:auto` pin: ResolveAll
// deliberately skips `:auto`, so without the merge the next
// `install --frozen-lockfile` fails immediately after a successful update.
func TestRunUpdatePreservesMaterializedAutoChecksumPin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	freshChecksum := "sha256:" + strings.Repeat("d", 64)
	materialized := "sha256:" + strings.Repeat("e", 64)
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.auto.http]\nurl = \"https://example.com/auto\"\nchecksum = \"sha256:auto\"\n\n"+
		"[tools.pinned.http]\nurl = \"https://example.com/pinned\"\nchecksum = \""+freshChecksum+"\"\n")

	old := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"auto/http/0":   {Checksum: materialized},
			"pinned/http/0": {Checksum: "sha256:" + strings.Repeat("f", 64)},
		},
		MethodsHash: map[string]string{
			"auto":   "stale-auto-identity",
			"pinned": "stale-pinned-identity",
		},
	}
	lockPath := lock.DefaultPath(schemaPath)
	if err := lock.Save(lockPath, old); err != nil {
		t.Fatal(err)
	}

	runTestUpdate(t, schemaPath, "")

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("runUpdate did not write a lock")
	}

	// The :auto tool is covered by the fresh schema, so its method identity
	// is recomputed — but ResolveAll produced no checksum pin for it, so the
	// materialized checksum must be carried over field-wise.
	if pin := got.Tools["auto/http/0"]; pin.Checksum != materialized {
		t.Fatalf("auto pin = %#v, want preserved materialized checksum", pin)
	}
	if h := got.MethodsHash["auto"]; h == "" || h == "stale-auto-identity" {
		t.Fatalf("auto methods_hash = %q, want freshly computed hash (identity change accepted)", h)
	}

	// A tool with a re-resolvable checksum gets the fresh value.
	if pin := got.Tools["pinned/http/0"]; pin.Checksum != freshChecksum {
		t.Fatalf("pinned checksum = %q, want fresh %q", pin.Checksum, freshChecksum)
	}
	if h := got.MethodsHash["pinned"]; h == "" || h == "stale-pinned-identity" {
		t.Fatalf("pinned methods_hash = %q, want freshly computed hash", h)
	}
}

func TestRunUpdateRegeneratesUnreadableLock(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	checksum := "sha256:" + strings.Repeat("a", 64)
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.demo.http]\nurl = \"https://example.com/demo\"\nchecksum = \""+checksum+"\"\n")
	lockPath := lock.DefaultPath(schemaPath)
	if err := os.WriteFile(lockPath, []byte("not valid toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	runTestUpdate(t, schemaPath, "")

	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatalf("regenerated lock: %v", err)
	}
	if got == nil {
		t.Fatal("runUpdate did not regenerate the unreadable lock")
	}
	if pin := got.Tools["demo/http/0"]; pin.Checksum != checksum {
		t.Fatalf("regenerated pin = %#v, want checksum %q", pin, checksum)
	}
	if got.MethodsHash["demo"] == "" {
		t.Fatal("regenerated lock is missing the fresh method identity")
	}
}
