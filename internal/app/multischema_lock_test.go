package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
)

// A lock is part of the project intent, not the shared host state. Two named
// schemas in one directory must be able to update and use their own v2 locks.
func TestNamedSchemasKeepIndependentFrozenLocks(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	one := filepath.Join(dir, "schema.toml")
	two := filepath.Join(dir, "desktop.toml")
	write := func(path, url, hexDigit string) {
		t.Helper()
		data := "schema_version = 1\n\n[tools.shared.http]\nurl = \"" + url + "\"\nchecksum = \"sha256:" + strings.Repeat(hexDigit, 64) + "\"\n"
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(one, "https://example.com/one", "a")
	write(two, "https://example.com/two", "b")

	// Ensure order of update does not cause one schema to inherit the other's lock.
	runTestUpdate(t, two, "")
	twoBefore, err := os.ReadFile(lock.DefaultPath(two))
	if err != nil {
		t.Fatal(err)
	}
	runTestUpdate(t, one, "")
	twoAfter, err := os.ReadFile(lock.DefaultPath(two))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(twoBefore, twoAfter) {
		t.Fatal("updating schema.toml overwrote desktop.toml.lock")
	}
	if lock.DefaultPath(one) == lock.DefaultPath(two) {
		t.Fatal("different schemas share a lock path")
	}
	for _, schema := range []string{one, two} {
		lk, err := lock.Load(lock.DefaultPath(schema))
		if err != nil || lk == nil || lk.Version != lock.CurrentVersion {
			t.Fatalf("load lock for %s: %v / %+v", schema, err, lk)
		}
		doc, err := lk.ProjectionDocument()
		if err != nil {
			t.Fatal(err)
		}
		if err := doc.VerifyCoverage([]string{"shared"}); err != nil {
			t.Fatalf("lock coverage for %s: %v", schema, err)
		}
		cmd := newInstallCmd()
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		for _, pair := range [][2]string{{"schema", schema}, {"no-manifest", "true"}, {"frozen-lockfile", "true"}, {"dry-run", "true"}} {
			if err := cmd.Flags().Set(pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("frozen dry run %s: %v", schema, err)
		}
	}
}

// An explicit writer path has to be readable by the next command. A stale
// standard lock must not be silently substituted when --lock was supplied.
func TestExplicitUpdateLockConsumedByFrozenInstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	schema := filepath.Join(dir, "desktop.toml")
	data := "schema_version = 1\n\n[tools.shared.http]\nurl = \"https://example.com/explicit\"\nchecksum = \"sha256:" + strings.Repeat("c", 64) + "\"\n"
	if err := os.WriteFile(schema, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "explicit.lock")
	cmd := newUpdateCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	for _, pair := range [][2]string{{"schema", schema}, {"lock", custom}, {"no-manifest", "true"}} {
		if err := cmd.Flags().Set(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("update custom lock: %v", err)
	}
	if _, err := os.Stat(lock.DefaultPath(schema)); !os.IsNotExist(err) {
		t.Fatalf("default lock should not have been created: %v", err)
	}
	install := func(withCustom bool) error {
		cmd := newInstallCmd()
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		flags := [][2]string{{"schema", schema}, {"no-manifest", "true"}, {"frozen-lockfile", "true"}, {"dry-run", "true"}}
		if withCustom {
			flags = append(flags, [2]string{"lock", custom})
		}
		for _, pair := range flags {
			if err := cmd.Flags().Set(pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
		return cmd.ExecuteContext(context.Background())
	}
	if err := install(false); err == nil {
		t.Fatal("frozen install without --lock must not guess a custom lock")
	}
	if err := install(true); err != nil {
		t.Fatalf("frozen install with --lock: %v", err)
	}
}

// Read-only commands and upgrade must honor the same explicit lock path as
// update and install. A broken neighboring lock must never override --lock.
func TestLockReadersHonorExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(dir, "desktop.toml")
	if err := os.WriteFile(schema, []byte("schema_version = 1\n\n[tools.shared.http]\nurl = \"https://example.com/shared\"\nchecksum = \"sha256:"+strings.Repeat("d", 64)+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock.DefaultPath(schema), []byte("version = 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "pinned.lock")
	if err := lock.Save(custom, &lock.Lock{Version: 1}); err != nil {
		t.Fatal(err)
	}
	manifest := ""
	noManifest := true
	project, lk, err := loadStatusSchema(schema, &manifest, &noManifest, true, nil, custom)
	if err != nil || lk == nil || lk.Version != 1 {
		t.Fatalf("status lock = (%v, %v), want selected v1", lk, err)
	}
	if project == nil {
		t.Fatal("expected valid schema project")
	}
	if _, _, err := loadStatusSchema(schema, &manifest, &noManifest, true, nil); err == nil {
		t.Fatal("status without override must not silently select the custom lock")
	}
	got := doctorLockFor(schema, custom)
	if got.Path != custom || !got.Present || !got.Readable || got.Version != 1 {
		t.Fatalf("doctor lock %+v, want readable custom lock", got)
	}
	upgraded, err := loadUpgradeLock(schema, log.Default, custom)
	if err != nil || upgraded == nil || upgraded.Version != 1 {
		t.Fatalf("upgrade lock = (%v, %v), want selected v1", upgraded, err)
	}
	report := &auditReport{Findings: []auditFinding{}}
	audited := auditLock(report, project, custom)
	if audited == nil || report.LockPath != custom || report.LockVersion != 1 {
		t.Fatalf("audit lock = (%v, %+v), want custom lock", audited, report)
	}
}
