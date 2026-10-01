package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

func TestResolveInstallLockUsesV2ProjectionWithoutLegacyResolution(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	const staleRevision = "abcdef0123456789abcdef0123456789abcdef01"

	intent := plan.New("tool", "git", true)
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitBranch, Value: "main"}
	intent.Identity.Revision = revision
	document, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{intent})
	if err != nil {
		t.Fatalf("BuildLockDocument() error: %v", err)
	}
	lk, err := lock.NewUniversal(document, nil, nil, map[string]lock.ToolPin{
		"tool/git/0": {Selector: "branch:main", Revision: staleRevision},
	})
	if err != nil {
		t.Fatalf("NewUniversal() error: %v", err)
	}

	schemaPath := filepath.Join(t.TempDir(), "schema.toml")
	if err := lock.Save(lock.DefaultPath(schemaPath), lk); err != nil {
		t.Fatal(err)
	}
	method := &config.MethodCandidate{
		Kind:   "git",
		Config: map[string]any{"url": "https://example.test/tool.git", "branch": "main"},
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}},
	}}
	runner := &run.FakeRunner{Stdout: revision + "\trefs/heads/main\n"}

	got, err := resolveInstallLock(context.Background(), installPlan{schema: schemaPath}, schema, log.Default, runner)
	if err != nil {
		t.Fatalf("resolveInstallLock() error: %v", err)
	}
	if got == nil || got.Version != lock.CurrentVersion {
		t.Fatalf("resolved lock = %#v, want v2 lock", got)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("legacy selector resolver ran %d commands, want none", len(runner.Calls))
	}
	if method.LockedRevision != "" {
		t.Fatalf("v2 legacy pin mutated schema LockedRevision to %q", method.LockedRevision)
	}
	if pin := got.Tools["tool/git/0"]; pin.Revision != staleRevision {
		t.Fatalf("legacy compatibility pin changed: %+v", pin)
	}
	entry, ok := document.EntryForTool("tool")
	if !ok || entry.Identity.Revision != revision {
		t.Fatalf("universal entry = (%+v, %v), want revision %q", entry, ok, revision)
	}
}

func TestSyncInstalledVersionsReadsV2ProjectionWithoutLegacyPins(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	writeTestState(t, stateHome, map[string]state.ToolState{
		"demo": {Method: "http", MethodKind: "http"},
	})

	lockPath := filepath.Join(t.TempDir(), "depengine.lock")
	lk := buildV2InstallTestLock(t, nil)
	if err := lock.Save(lockPath, lk); err != nil {
		t.Fatal(err)
	}
	schema := &config.Schema{Tools: map[string]*config.Tool{"demo": {Name: "demo"}}}
	report := &exec.ExecReport{Tools: []exec.ToolResult{{Tool: "demo", Status: exec.StatusInstalled}}}

	syncInstalledVersions(context.Background(), schema, lockPath, report, log.Default)

	ls, err := state.LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()
	if got := ls.State().Tools["demo"].Version; got != "1.2.3" {
		t.Fatalf("backfilled version = %q, want v2 projection version 1.2.3", got)
	}
}
