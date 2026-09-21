package exec

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func init() {
	run.OverrideElevation("sudo")
}
func TestSyncManagerNeedsSyncTrue(t *testing.T) {
	t.Parallel()
	sm := NewSyncManager(&run.FakeRunner{}, "debian")
	if !sm.NeedsSync() {
		t.Fatal("expected NeedsSync=true for debian")
	}
}

func TestSyncManagerNeedsSyncFalse(t *testing.T) {
	t.Parallel()
	sm := NewSyncManager(&run.FakeRunner{}, "arch")
	if sm.NeedsSync() {
		t.Fatal("expected NeedsSync=false for arch")
	}
}

func TestSyncManagerSyncRunsCommand(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	sm := NewSyncManager(fr, "debian")
	if err := sm.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected Sync error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	// apt-get update for debian prepends sudo → [sudo, apt-get, update]
	got := fr.Calls[0]
	if got.Name != "sudo" || got.Args[0] != "apt-get" || got.Args[1] != "update" {
		t.Errorf("unexpected sync command: %s %v", got.Name, got.Args)
	}
}

func TestSyncManagerSyncOnlyOnce(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}

	sm := NewSyncManager(fr, "debian")
	if err := sm.Sync(context.Background()); err != nil {
		t.Fatalf("first Sync error: %v", err)
	}
	// Second call should be a no-op (already synced).
	if err := sm.Sync(context.Background()); err != nil {
		t.Fatalf("second Sync error: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected only 1 call (cached), got %d", len(fr.Calls))
	}
}

func TestSyncManagerSyncAlreadySynced(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{}
	sm := NewSyncManager(fr, "debian")
	sm.synced = true // mark as synced without running

	if err := sm.Sync(context.Background()); err != nil {
		t.Fatalf("Sync error on already-synced manager: %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatal("expected no calls when already synced")
	}
}

// A failed sync (runtime error, e.g. the network being unreachable) must
// not be fatal: Sync() swallows it after the runner has already logged the
// failure at WARN, per Achado 1 in findings.md. The command is still
// actually attempted, and the manager honestly reports itself as not
// synced (so a hypothetical future retry within the session wouldn't be
// short-circuited by a false "already done").
func TestSyncManagerSyncFailureIsNotFatal(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Err: errors.New("network error")}

	sm := NewSyncManager(fr, "debian")
	if err := sm.Sync(context.Background()); err != nil {
		t.Fatalf("Sync must not return an error on failure, got: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected the sync command to still be attempted, got %d calls", len(fr.Calls))
	}
	if sm.synced {
		t.Fatal("a failed sync must not be recorded as synced")
	}
}

// Same as above but for a non-zero exit code (the common real-world case:
// `apt-get update` returning 100 because one unrelated third-party repo is
// broken, even though the indexes the requested packages need are fine).
func TestSyncManagerSyncExitCodeFailureIsNotFatal(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 100, Stderr: "E: Failed to fetch ... 403 Forbidden"}

	sm := NewSyncManager(fr, "debian")
	if err := sm.Sync(context.Background()); err != nil {
		t.Fatalf("Sync must not return an error on non-zero exit, got: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected the sync command to still be attempted, got %d calls", len(fr.Calls))
	}
	if sm.synced {
		t.Fatal("a failed sync must not be recorded as synced")
	}
}
