package state

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The helper uses the test executable as another process, not a second
// goroutine. A process-local mutex would pass a goroutine test but would not
// prevent two depengine invocations from interleaving physical operations.
func TestMutationLockChild(t *testing.T) {
	mode := os.Getenv("DEPENGINE_MUTATION_LOCK_CHILD")
	if mode == "" {
		return
	}
	timeout := 200 * time.Millisecond
	if mode == "released" {
		// Other cooperating processes may legitimately hold the user-level lock.
		// Release tests must not depend on them finishing within 200 ms.
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	lock, err := LockMutationsContext(ctx)
	if mode == "blocked" {
		if lock != nil {
			_ = lock.Close()
			t.Fatal("competing process acquired a held mutation lock")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting for mutation lock: got %v, want deadline exceeded", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("mutation lock after release: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release mutation lock: %v", err)
	}
}

// Keep process-lock tests isolated from other packages running concurrently
// under `go test ./...` while preserving the shared namespace across their
// parent and child processes. The production lock never has this override.
func isolateMutationLockTestNamespace(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())       // Unix privatepath.TempRoot
	t.Setenv("LOCALAPPDATA", t.TempDir()) // Windows privatepath.TempRoot
}

func TestMutationLockSerializesProcessesAndReleases(t *testing.T) {
	isolateMutationLockTestNamespace(t)
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	lock, err := LockMutationsContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "depengine", "state.json.mutations.lock")
	if fi, err := os.Stat(lockPath); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 {
		t.Fatalf("mutation lock not an owner-only regular file: %v, %v", fi, err)
	}

	child := func(mode string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestMutationLockChild$") // #nosec G204,G702 -- fixed test helper entry point.
		cmd.Env = append(os.Environ(), "DEPENGINE_MUTATION_LOCK_CHILD="+mode)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child mode %s: %v\n%s", mode, err, output)
		}
	}
	child("blocked")
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	child("released")
}

// Different XDG state roots do not mean different host package inventories.
// A second CLI process in the same user profile must not bypass the host
// mutation boundary merely by selecting a different state directory.
func TestMutationLockSerializesDistinctStateRootsForSameUser(t *testing.T) {
	isolateMutationLockTestNamespace(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	t.Setenv("XDG_STATE_HOME", firstRoot)

	held, err := LockMutationsContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if held != nil {
			_ = held.Close()
		}
	})

	child := func(mode string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestMutationLockChild$") // #nosec G204,G702 -- fixed test helper entry point.
		cmd.Env = append(os.Environ(),
			"XDG_STATE_HOME="+secondRoot,
			"DEPENGINE_MUTATION_LOCK_CHILD="+mode)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("different-root child %q: %v\n%s", mode, err, output)
		}
	}
	child("blocked")
	// Waiting on the per-user boundary should not reach the second root's
	// own lock. In particular, it must not create a second state document.
	if _, err := os.Stat(filepath.Join(secondRoot, "depengine", "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked child touched second state: %v", err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	held = nil
	child("released")
}

// A failed acquisition of the legacy state-root lock must not strand the
// already-acquired host lock and block unrelated state roots indefinitely.
func TestMutationLockReleasesUserLockWhenStateRootLockFails(t *testing.T) {
	isolateMutationLockTestNamespace(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	localLockPath := DefaultPath() + ".mutations.lock"
	if err := os.MkdirAll(localLockPath, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := LockMutationsContext(context.Background())
	if lock != nil {
		_ = lock.Close()
		t.Fatal("accepted directory in place of state lock")
	}
	if err == nil {
		t.Fatal("state lock failure not reported")
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	other, err := LockMutationsContext(ctx)
	if err != nil {
		t.Fatalf("host lock was leaked after state-root lock failure: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMutationLockCanceledBeforeAcquisitionDoesNotCreateFile(t *testing.T) {
	isolateMutationLockTestNamespace(t)
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lock, err := LockMutationsContext(ctx)
	if lock != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled lock = (%v, %v), want nil and canceled", lock, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "depengine", "state.json.mutations.lock")); !os.IsNotExist(err) {
		t.Fatalf("canceled acquisition created lock file: %v", err)
	}
}
