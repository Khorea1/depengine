package exec

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
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

func TestSyncManagerSyncFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Err: errors.New("network error")}

	sm := NewSyncManager(fr, "debian")
	if err := sm.Sync(context.Background()); err == nil {
		t.Fatal("expected error for failed sync")
	}
}

func TestSyncManagerSyncExitCodeFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}

	sm := NewSyncManager(fr, "debian")
	if err := sm.Sync(context.Background()); err == nil {
		t.Fatal("expected error for non-zero exit code")
	}
}
