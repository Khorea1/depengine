package app

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

// A separate test process exercises the real update entry point and OS lock.
// It cannot pass merely because two goroutines share a mutex.
func TestUpdateMutationLockProcessHelper(t *testing.T) {
	mode := os.Getenv("DEPENGINE_UPDATE_MUTATION_CHILD")
	if mode == "" {
		return
	}
	path := os.Getenv("DEPENGINE_UPDATE_MUTATION_SCHEMA")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	noManifest, frozen, dryRun, verbose, quiet := true, false, false, false, true
	manifest, lockOverride, profile := "", "", ""
	err := runUpdate(ctx, &path, &manifest, &noManifest, &lockOverride, &profile, &frozen, &dryRun, &verbose, &quiet, new(bool), new(bool), nil)
	switch mode {
	case "blocked":
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("update raced a host mutation: error=%v, want deadline exceeded", err)
		}
	case "released":
		if err != nil {
			t.Fatalf("update after host mutation: %v", err)
		}
	default:
		t.Fatalf("unexpected test mode %q", mode)
	}
}

func TestUpdateSerializesLockRefreshWithHostMutationAcrossProcesses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	checksum := "sha256:" + strings.Repeat("a", 64)
	schemaPath := writeUpdateTestSchema(t, dir, "schema_version = 1\n\n"+
		"[tools.demo]\nmethod_only = [\"http\"]\n\n"+
		"[tools.demo.http]\nurl = \"https://example.com/demo\"\nchecksum = \""+checksum+"\"\n")

	held, err := state.LockMutationsContext(context.Background())
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
		cmd := osexec.Command(os.Args[0], "-test.run=^TestUpdateMutationLockProcessHelper$") // #nosec G204,G702 -- fixed test helper entry point.
		cmd.Env = append(os.Environ(), "DEPENGINE_UPDATE_MUTATION_CHILD="+mode, "DEPENGINE_UPDATE_MUTATION_SCHEMA="+schemaPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("update child %s: %v\n%s", mode, err, output)
		}
	}
	child("blocked")
	if _, err := os.Stat(lock.DefaultPath(schemaPath)); !os.IsNotExist(err) {
		t.Fatalf("blocked update modified lockfile: %v", err)
	}

	// A dry-run does not write a lockfile or wait for a mutating invocation.
	noManifest, frozen, dryRun, verbose, quiet := true, false, true, false, true
	manifest, lockOverride, profile := "", "", ""
	if err := runUpdate(context.Background(), &schemaPath, &manifest, &noManifest, &lockOverride, &profile, &frozen, &dryRun, &verbose, &quiet, new(bool), new(bool), nil); err != nil {
		t.Fatalf("dry-run while mutation lock is held: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "depengine.lock")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created a lockfile: %v", err)
	}

	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	held = nil
	child("released")
	lk, err := lock.Load(lock.DefaultPath(schemaPath))
	if err != nil || lk == nil || lk.Version != lock.CurrentVersion {
		t.Fatalf("update did not persist a valid lock after release: lock=%v err=%v", lk, err)
	}
}
