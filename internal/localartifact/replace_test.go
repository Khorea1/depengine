package localartifact

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplacePathReportsRestoreFailure(t *testing.T) {
	commitErr := errors.New("commit failed")
	restoreErr := errors.New("restore failed")
	renameCalls := 0
	ops := replacePathOps{
		lstat:     func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		mkdirTemp: func(string, string) (string, error) { return "/parent/backup", nil },
		rename: func(_, _ string) error {
			renameCalls++
			switch renameCalls {
			case 1:
				return nil // destination -> backup
			case 2:
				return commitErr // stage -> destination
			case 3:
				return restoreErr // backup -> destination
			default:
				t.Fatalf("unexpected rename call %d", renameCalls)
				return nil
			}
		},
		remove:    func(string) error { return nil },
		removeAll: func(string) error { return nil },
	}

	err := replacePathWithOps("/parent/stage", "/parent/destination", ops)
	if !errors.Is(err, commitErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("replacePathWithOps() error = %v, want commit and restore errors", err)
	}
	if !strings.Contains(err.Error(), "restore previous destination") {
		t.Fatalf("replacePathWithOps() error = %q, want restore context", err)
	}
}

func TestReplacePathReportsCleanupFailureAfterRestore(t *testing.T) {
	commitErr := errors.New("commit failed")
	cleanupErr := errors.New("cleanup failed")
	renameCalls := 0
	ops := replacePathOps{
		lstat:     func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		mkdirTemp: func(string, string) (string, error) { return "/parent/backup", nil },
		rename: func(_, _ string) error {
			renameCalls++
			if renameCalls == 2 {
				return commitErr
			}
			return nil
		},
		remove:    func(string) error { return cleanupErr },
		removeAll: func(string) error { return nil },
	}

	err := replacePathWithOps("/parent/stage", "/parent/destination", ops)
	if !errors.Is(err, commitErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("replacePathWithOps() error = %v, want commit and cleanup errors", err)
	}
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "destination" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

func TestReplacePathRollsBackCommittedDestinationWhenBackupCleanupFails(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	renameCalls := make([][2]string, 0, 4)
	ops := replacePathOps{
		lstat:     func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		mkdirTemp: func(string, string) (string, error) { return "/parent/backup", nil },
		rename: func(from, to string) error {
			renameCalls = append(renameCalls, [2]string{from, to})
			return nil
		},
		remove:    func(string) error { return nil },
		removeAll: func(string) error { return cleanupErr },
	}

	err := replacePathWithOps("/parent/stage", "/parent/destination", ops)
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("replacePathWithOps() error = %v, want cleanup error", err)
	}
	// The backup path is built with filepath.Join in the product; only the
	// injected mkdirTemp result is literal, so spell it the same way here.
	backup := filepath.Join("/parent/backup", "original")
	want := [][2]string{
		{"/parent/destination", backup},
		{"/parent/stage", "/parent/destination"},
		{"/parent/destination", "/parent/stage"},
		{backup, "/parent/destination"},
	}
	if len(renameCalls) != len(want) {
		t.Fatalf("rename calls = %#v, want %#v", renameCalls, want)
	}
	for i := range want {
		if renameCalls[i] != want[i] {
			t.Fatalf("rename call %d = %#v, want %#v", i, renameCalls[i], want[i])
		}
	}
}

func TestReplacePathReportsRollbackFailureAfterBackupCleanupFailure(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	rollbackErr := errors.New("rollback failed")
	renameCount := 0
	ops := replacePathOps{
		lstat:     func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		mkdirTemp: func(string, string) (string, error) { return "/parent/backup", nil },
		rename: func(_, _ string) error {
			renameCount++
			if renameCount == 3 {
				return rollbackErr
			}
			return nil
		},
		remove:    func(string) error { return nil },
		removeAll: func(string) error { return cleanupErr },
	}

	err := replacePathWithOps("/parent/stage", "/parent/destination", ops)
	if !errors.Is(err, cleanupErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("replacePathWithOps() error = %v, want cleanup and rollback errors", err)
	}
}

func TestReplacePathRecommitsNewDestinationWhenRestoreAfterCleanupFailureFails(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	restoreErr := errors.New("restore failed")
	renameCalls := make([][2]string, 0, 5)
	ops := replacePathOps{
		lstat:     func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		mkdirTemp: func(string, string) (string, error) { return "/parent/backup", nil },
		rename: func(from, to string) error {
			renameCalls = append(renameCalls, [2]string{from, to})
			if len(renameCalls) == 4 {
				return restoreErr
			}
			return nil
		},
		remove:    func(string) error { return nil },
		removeAll: func(string) error { return cleanupErr },
	}

	err := replacePathWithOps("/parent/stage", "/parent/destination", ops)
	if !errors.Is(err, cleanupErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("replacePathWithOps() error = %v, want cleanup and restore errors", err)
	}
	wantLast := [2]string{"/parent/stage", "/parent/destination"}
	if got := renameCalls[len(renameCalls)-1]; got != wantLast {
		t.Fatalf("last rename = %#v, want recommit %#v; all calls=%#v", got, wantLast, renameCalls)
	}
}
