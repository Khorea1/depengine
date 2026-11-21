//go:build !windows

package state

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Khorea1/depengine/internal/privatepath"
)

func TestMutationLockRejectsLinkedFilesWithoutTouchingVictim(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_STATE_HOME", dir)
			lockPath := DefaultPath() + ".mutations.lock"
			if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(dir, "unrelated-file")
			if err := os.WriteFile(victim, []byte("important"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(victim, 0644); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "symlink" {
				err = os.Symlink(victim, lockPath)
			} else {
				err = os.Link(victim, lockPath)
			}
			if err != nil {
				t.Skipf("cannot create %s fixture: %v", kind, err)
			}
			lock, err := LockMutationsContext(context.Background())
			if lock != nil {
				_ = lock.Close()
				t.Fatal("linked lock file was accepted")
			}
			if err == nil {
				t.Fatal("linked lock file was accepted without error")
			}
			info, err := os.Stat(victim)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0644 {
				t.Fatalf("linked lock changed unrelated file permissions: %04o", info.Mode().Perm())
			}
			data, err := os.ReadFile(victim)
			if err != nil || string(data) != "important" {
				t.Fatalf("linked lock modified unrelated file: data=%q err=%v", data, err)
			}
		})
	}
}

func TestMutationLockRejectsNonregularFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	lockPath := DefaultPath() + ".mutations.lock"
	if err := os.MkdirAll(lockPath, 0755); err != nil {
		t.Fatal(err)
	}
	lock, err := LockMutationsContext(context.Background())
	if lock != nil {
		_ = lock.Close()
		t.Fatal("directory lock target was accepted")
	}
	if err == nil {
		t.Fatal("directory lock target was accepted without error")
	}
}

func TestMutationLockRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	lockPath := DefaultPath() + ".mutations.lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(lockPath, 0600); err != nil {
		t.Skipf("cannot create FIFO fixture: %v", err)
	}
	lock, err := LockMutationsContext(context.Background())
	if lock != nil {
		_ = lock.Close()
		t.Fatal("FIFO lock target was accepted")
	}
	if err == nil {
		t.Fatal("FIFO lock target was accepted without error")
	}
}

func TestUserHostMutationLockRejectsSymlinkWithoutTouchingVictim(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root, err := privatepath.TempRoot()
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "unrelated-file")
	if err := os.WriteFile(victim, []byte("important"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "host.mutations.lock")); err != nil {
		t.Skipf("cannot create symlink fixture: %v", err)
	}
	lock, err := LockMutationsContext(context.Background())
	if lock != nil {
		_ = lock.Close()
		t.Fatal("per-user host lock symlink was accepted")
	}
	if err == nil {
		t.Fatal("per-user host lock symlink accepted without error")
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "important" {
		t.Fatalf("symlink victim changed: data=%q err=%v", data, err)
	}
	info, err := os.Stat(victim)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("victim permissions changed: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(DefaultPath() + ".mutations.lock"); !os.IsNotExist(err) {
		t.Fatalf("failed host lock acquisition reached state-root lock: %v", err)
	}
}
