package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
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

// runTestInstallDryRun drives `depengine install` in-process for the whole
// schema: no manifest, dry-run so neither the host nor the lock is mutated.
// It returns the command error, which carries a failed plan (for example a
// lock mismatch) as a non-nil exit error.
func runTestInstallDryRun(t *testing.T, schemaPath string) error {
	t.Helper()
	cmd := newInstallCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	for _, flag := range [][2]string{
		{"schema", schemaPath},
		{"no-manifest", "true"},
		{"dry-run", "true"},
	} {
		if err := cmd.Flags().Set(flag[0], flag[1]); err != nil {
			t.Fatalf("set --%s: %v", flag[0], err)
		}
	}
	return cmd.ExecuteContext(context.Background())
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
//
// Both tools pin `method_only = ["http"]` so universal projection uses the
// checksummed http candidate deterministically. Without it, the native
// brew-tap candidate is host-dependent: on machines with brew installed it
// becomes `would_install` (missing tap) and is selected ahead of http, while
// on machines without brew it fails and http is selected. That made the test
// pass on Linux and fail on macOS with "git-backed source did not resolve to
// a concrete revision".
func TestRunUpdatePreservesPinsAndHashesOutsideProfile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	freshChecksum := "sha256:" + strings.Repeat("a", 64)
	materialized := "sha256:" + strings.Repeat("c", 64)
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.profiled]\ntags = [\"dev\"]\nmethod_only = [\"http\"]\n\n"+
		"[tools.profiled.http]\nurl = \"https://example.com/profiled\"\nchecksum = \""+freshChecksum+"\"\n\n"+
		"[tools.profiled.native]\npkg = \"profiled\"\nsources = [{ kind = \"brew-tap\", name = \"profiled/tools\", url = \"https://example.com/profiled/tools.git\" }]\n\n"+
		"[tools.outside]\ntags = [\"other\"]\nmethod_only = [\"http\"]\n\n"+
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

	// The lock stays on the legacy format. A --profile run cannot prove a
	// universal projection from a v1 lock: there is no previous projection to
	// recover `outside` from, so promoting to v2 would persist a document
	// whose identity the v2 consumer cannot resolve for tools outside the
	// profile. A later full-scope update performs the migration.
	if got.Version != 1 {
		t.Fatalf("lock version = %d, want 1 (profiled update promoted a lock it cannot project universally)", got.Version)
	}
	if got.UniversalProjection != "" {
		t.Fatalf("universal projection = %q, want none while the lock stays v1", got.UniversalProjection)
	}
}

// TestFullInstallAfterProfiledUpdateKeepsToolsOutsideProfileConsumable is the
// regression test for `depengine update --profile` promoting a legacy v1 lock
// to v2 with a projection that only covers the profiled tools. The v1 pins of
// tools outside the profile survive, but the v2 consumer never looks them up:
// the next full install fails with `lock mismatch: tool "outside" is not
// present in lock` instead of planning the tool from its legacy identity.
//
// The policy under test: a profiled update refreshes the profile's v1 pins and
// keeps the legacy format, because only a full-scope update has enough
// information to prove a universal projection. A later full update migrates the
// lock to v2.
func TestFullInstallAfterProfiledUpdateKeepsToolsOutsideProfileConsumable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	profiledChecksum := "sha256:" + strings.Repeat("a", 64)
	outsideChecksum := "sha256:" + strings.Repeat("b", 64)
	// Both tools pin `method_only = ["http"]` so resolution is hermetic: the
	// native candidate is excluded and a literal checksum URL resolves
	// without network access.
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.profiled]\ntags = [\"dev\"]\nmethod_only = [\"http\"]\n\n"+
		"[tools.profiled.http]\nurl = \"https://example.com/profiled\"\nchecksum = \""+profiledChecksum+"\"\n\n"+
		"[tools.outside]\ntags = [\"other\"]\nmethod_only = [\"http\"]\n\n"+
		"[tools.outside.http]\nurl = \"https://example.com/outside\"\nchecksum = \""+outsideChecksum+"\"\n")

	lockPath := lock.DefaultPath(schemaPath)
	if err := lock.Save(lockPath, &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"profiled/http/0": {Checksum: profiledChecksum},
			"outside/http/0":  {Checksum: outsideChecksum},
		},
		MethodsHash: map[string]string{
			"profiled": "profiled-legacy-identity",
			"outside":  "outside-legacy-identity",
		},
	}); err != nil {
		t.Fatal(err)
	}

	runTestUpdate(t, schemaPath, "dev")

	updated, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil {
		t.Fatal("runUpdate did not write a lock")
	}
	if updated.Version != 1 {
		t.Fatalf("lock version = %d, want 1 (profiled update promoted a lock it cannot project universally)", updated.Version)
	}
	if pin := updated.Tools["outside/http/0"]; pin.Checksum != outsideChecksum {
		t.Fatalf("outside pin = %#v, want preserved legacy pin", pin)
	}

	// The full install must still be able to plan every tool of the schema
	// against that lock: nothing may disappear from the consumable identity.
	if err := runTestInstallDryRun(t, schemaPath); err != nil {
		t.Fatalf("full install after profiled update: %v", err)
	}
}

// TestRunUpdateRebuildsProjectionForProfileOnV2Lock is the counterpart of the
// profiled-v1 regression test above. When the previous lock is already v2, its
// projection is the only source of identity for tools outside the profile, so
// update must rebuild and persist it: the profiled tool accepts fresh identity
// while the omitted tool keeps its previous entry.
func TestRunUpdateRebuildsProjectionForProfileOnV2Lock(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	dir := t.TempDir()
	checksum := "sha256:" + strings.Repeat("a", 64)
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.profiled]\ntags = [\"dev\"]\nmethod_only = [\"http\"]\n\n"+
		"[tools.profiled.http]\nurl = \"https://example.com/profiled\"\nchecksum = \""+checksum+"\"\n\n"+
		"[tools.outside]\ntags = [\"other\"]\nmethod_only = [\"http\"]\n\n"+
		"[tools.outside.http]\nurl = \"https://example.com/outside\"\nchecksum = \""+checksum+"\"\n")

	// Previous v2 lock whose projection deliberately seeds identities that a
	// fresh resolution must replace for the profiled tool and preserve for the
	// tool outside the profile.
	seeded := func(name, version string) plan.ResolvedInstallPlan {
		p := plan.New(name, "http", true)
		p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: version}
		p.Identity.Version = version
		return p
	}
	previous, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{
		seeded("profiled", "0.0.0"),
		seeded("outside", "9.9.9"),
	})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := lock.DefaultPath(schemaPath)
	previousLock := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"profiled/http/0": {Checksum: checksum},
			"outside/http/0":  {Checksum: checksum},
		},
		MethodsHash: map[string]string{"profiled": "stale", "outside": "stale"},
	}
	if err := previousLock.SetProjection(previous); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(lockPath, previousLock); err != nil {
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
	if got.Version != lock.CurrentVersion {
		t.Fatalf("lock version = %d, want %d (profiled update left a v2 lock behind)", got.Version, lock.CurrentVersion)
	}
	document, err := got.ProjectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]plan.LockProjection, len(document.Entries))
	for _, entry := range document.Entries {
		entries[entry.Tool.Name] = entry
	}
	profiled, ok := entries["profiled"]
	if !ok {
		t.Fatalf("projection entries = %v, want profiled", entries)
	}
	if profiled.Identity.Version == "0.0.0" {
		t.Fatalf("profiled projection = %#v, want freshly resolved identity (stale 0.0.0 survived)", profiled.Identity)
	}
	outside, ok := entries["outside"]
	if !ok {
		t.Fatalf("projection entries = %v, want retained outside entry", entries)
	}
	if outside.Identity.Version != "9.9.9" {
		t.Fatalf("outside projection = %#v, want retained 9.9.9 entry", outside.Identity)
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
