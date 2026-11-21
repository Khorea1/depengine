package app

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
)

func TestPruneRemovedLockEntriesKeepsActiveToolsAcrossProfiles(t *testing.T) {
	l := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"outside/http/0":  {Checksum: "keep"},
			"profiled/http/0": {Checksum: "keep"},
			"removed/http/0":  {Checksum: "discard"},
		},
		MethodsHash: map[string]string{"outside": "keep", "profiled": "keep", "removed": "discard", "empty": "discard"},
		SourceHash: map[string]string{
			"outside/native/0": "keep", "profiled/native/0": "keep", "removed/native/0": "discard", "other/http/0": "discard",
		},
		RecipeDigest: map[string]string{"outside": "keep", "removed": "discard"},
	}
	full := map[string]*config.Tool{"outside": {Name: "outside"}, "profiled": {Name: "profiled"}}
	got := pruneRemovedLockEntries(l, full)
	if want := []string{"empty", "other", "removed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed = %v, want %v", got, want)
	}
	if len(l.Tools) != 2 || len(l.MethodsHash) != 2 || len(l.SourceHash) != 2 || len(l.RecipeDigest) != 1 {
		t.Fatalf("unexpected leftover metadata: %+v", l)
	}
	if l.Tools["outside/http/0"].Checksum != "keep" || l.MethodsHash["profiled"] != "keep" || l.SourceHash["outside/native/0"] != "keep" {
		t.Fatalf("current tools' metadata lost: %+v", l)
	}
}

func TestUpdatePruneOnlyRemovesObsoleteLockMetadata(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	checksum := "sha256:" + strings.Repeat("a", 64)
	schemaPath := writeUpdateTestSchema(t, root, "schema_version = 1\n\n"+
		"[tools.profiled]\ntags = [\"dev\"]\nmethod_only = [\"http\"]\n"+
		"[tools.profiled.http]\nurl = \"https://example.com/profiled\"\nchecksum = \""+checksum+"\"\n\n"+
		"[tools.outside]\ntags = [\"other\"]\nmethod_only = [\"http\"]\n"+
		"[tools.outside.http]\nurl = \"https://example.com/outside\"\nchecksum = \""+checksum+"\"\n\n"+
		"[tools.dependency]\ndependency_only = true\nmethod_only = [\"http\"]\n"+
		"[tools.dependency.http]\nurl = \"https://example.com/dependency\"\nchecksum = \""+checksum+"\"\n")
	lockPath := lock.DefaultPath(schemaPath)
	prior := &lock.Lock{
		Version: 1,
		Tools: map[string]lock.ToolPin{
			"profiled/http/0": {Checksum: checksum}, "outside/http/0": {Checksum: checksum}, "dependency/http/0": {Checksum: checksum}, "removed/http/0": {Checksum: checksum},
		},
		MethodsHash: map[string]string{"profiled": "old", "outside": "old", "dependency": "old", "removed": "old"},
		SourceHash:  map[string]string{"outside/native/0": "retained", "dependency/native/0": "retained", "removed/native/0": "obsolete"},
	}
	if err := lock.Save(lockPath, prior); err != nil {
		t.Fatal(err)
	}

	// Default remains backward compatible and deliberately retains old pins.
	runTestUpdate(t, schemaPath, "dev")
	before, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := before.Tools["removed/http/0"]; !found {
		t.Fatal("default update pruned metadata without opt-in")
	}

	// Explicit pruning uses the FULL effective schema, never just --profile.
	cmd := newUpdateCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"--schema", schemaPath, "--no-manifest", "--profile", "dev", "--prune", "--quiet"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("profiled v1 update promoted lock: v%d", got.Version)
	}
	if _, found := got.Tools["removed/http/0"]; found {
		t.Fatal("obsolete pin retained with --prune")
	}
	if _, found := got.MethodsHash["removed"]; found {
		t.Fatal("obsolete methods hash retained with --prune")
	}
	if _, found := got.SourceHash["removed/native/0"]; found {
		t.Fatal("obsolete source hash retained with --prune")
	}
	if _, found := got.Tools["outside/http/0"]; !found {
		t.Fatal("out-of-profile tool pin was deleted")
	}
	if _, found := got.SourceHash["outside/native/0"]; !found {
		t.Fatal("out-of-profile source hash was deleted")
	}
	if _, found := got.Tools["dependency/http/0"]; !found {
		t.Fatal("declared dependency_only pin was deleted even though the tool remains in the schema")
	}
	if _, found := got.MethodsHash["dependency"]; !found {
		t.Fatal("declared dependency_only methods hash was deleted")
	}
	if _, found := got.SourceHash["dependency/native/0"]; !found {
		t.Fatal("declared dependency_only source hash was deleted")
	}
}

func TestUpdatePruneDryRunDoesNotWrite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	checksum := "sha256:" + strings.Repeat("a", 64)
	schemaPath := writeUpdateTestSchema(t, root, "schema_version = 1\n\n[tools.keep.http]\nurl = \"https://example.com/keep\"\nchecksum = \""+checksum+"\"\n")
	lockPath := lock.DefaultPath(schemaPath)
	old := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{"keep/http/0": {Checksum: checksum}, "removed/http/0": {Checksum: checksum}}}
	if err := lock.Save(lockPath, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := newUpdateCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--schema", schemaPath, "--no-manifest", "--prune", "--dry-run", "--quiet"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("update --prune --dry-run modified lockfile")
	}
}

func TestUpdatePruneUniversalRetainsProjectionAndOutOfProfileMetadata(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	checksum := "sha256:" + strings.Repeat("a", 64)
	schemaPath := writeUpdateTestSchema(t, root, "schema_version = 1\n\n"+
		"[tools.active]\ntags = [\"dev\"]\nmethod_only = [\"http\"]\n"+
		"[tools.active.http]\nurl = \"https://example.com/active\"\nchecksum = \""+checksum+"\"\n\n"+
		"[tools.outside]\ntags = [\"other\"]\nmethod_only = [\"http\"]\n"+
		"[tools.outside.http]\nurl = \"https://example.com/outside\"\nchecksum = \""+checksum+"\"\n")
	// Build a real v2 lock first, so this checks an authoritative projection.
	runTestUpdate(t, schemaPath, "")
	lockPath := lock.DefaultPath(schemaPath)
	old, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if old.Version != lock.CurrentVersion {
		t.Fatalf("version = %d, want %d", old.Version, lock.CurrentVersion)
	}
	old.Tools = map[string]lock.ToolPin{"removed/http/0": {Checksum: checksum}, "outside/http/0": {Checksum: checksum}}
	old.MethodsHash["removed"] = "obsolete"
	if old.SourceHash == nil {
		old.SourceHash = make(map[string]string)
	}
	old.SourceHash["removed/http/0"] = "obsolete"
	if old.RecipeDigest == nil {
		old.RecipeDigest = make(map[string]string)
	}
	old.RecipeDigest["removed"] = checksum
	if err := lock.Save(lockPath, old); err != nil {
		t.Fatal(err)
	}
	cmd := newUpdateCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"--schema", schemaPath, "--no-manifest", "--profile", "dev", "--prune", "--quiet"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := lock.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != lock.CurrentVersion {
		t.Fatalf("prune demoted v2 lock to v%d", got.Version)
	}
	if _, exists := got.Tools["removed/http/0"]; exists {
		t.Fatal("obsolete v2 compatibility pin retained")
	}
	if _, exists := got.MethodsHash["removed"]; exists {
		t.Fatal("obsolete v2 methods hash retained")
	}
	if _, exists := got.SourceHash["removed/http/0"]; exists {
		t.Fatal("obsolete v2 source hash retained")
	}
	if _, exists := got.RecipeDigest["removed"]; exists {
		t.Fatal("obsolete v2 recipe digest retained")
	}
	if _, exists := got.Tools["outside/http/0"]; !exists {
		t.Fatal("out-of-profile compatibility pin lost")
	}
	projection, err := got.ProjectionDocument()
	if err != nil {
		t.Fatal(err)
	}
	if err := projection.VerifyCoverage([]string{"active", "outside"}); err != nil {
		t.Fatalf("prune damaged universal coverage: %v", err)
	}
}
