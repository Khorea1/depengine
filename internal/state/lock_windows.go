//go:build windows

package state

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	LOCKFILE_FAIL_IMMEDIATELY = 1
	LOCKFILE_EXCLUSIVE_LOCK   = 2
	lockPollInterval          = 50 * time.Millisecond
	errorLockViolation        = syscall.Errno(33)
)

// exclusiveLockMode is the platform value passed to lockFilePathContext for an
// exclusive (writer) lock.
const exclusiveLockMode = true

// sharedLockMode is the platform value passed to lockFilePathContext for a
// shared (reader) lock.
const sharedLockMode = false

type overlapped struct {
	Internal, InternalHigh uintptr
	Offset, OffsetHigh     uint32
	HEvent                 uintptr
}
type fileLock struct{ f *os.File }

func (l *fileLock) Close() error {
	o := &overlapped{}
	_, _, _ = syscall.Syscall6(procUnlockFileEx.Addr(), 5, l.f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(o)), 0)
	return l.f.Close()
}
func lock() (io.Closer, error) { return lockContext(context.Background()) }
func lockContext(ctx context.Context) (io.Closer, error) {
	return lockWithModeContext(ctx, true, "acquire lock")
}
func lockShared() (io.Closer, error) { return lockSharedContext(context.Background()) }
func lockSharedContext(ctx context.Context) (io.Closer, error) {
	return lockWithModeContext(ctx, false, "acquire shared lock")
}
func lockWithModeContext(ctx context.Context, exclusive bool, desc string) (io.Closer, error) {
	return lockFilePathContext(ctx, DefaultPath()+".lock", exclusive, desc)
}

// lockSharedFilePathContext acquires a shared lock on an arbitrary private
// lock file. Journal queries use it to exclude rotation and in-flight appends.
func lockSharedFilePathContext(ctx context.Context, path, desc string) (io.Closer, error) {
	return lockFilePathContext(ctx, path, sharedLockMode, desc)
}

// lockFilePathContext locks an arbitrary private lock file. Journal writes use
// their own lock so appending cannot deadlock against a held state lock.
func lockFilePathContext(ctx context.Context, path string, exclusive bool, desc string) (io.Closer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("create private lock dir: %w", err)
	}
	// Reject pre-existing reparse points and directories rather than locking
	// an unrelated file through a symlink.
	if prior, err := os.Lstat(path); err == nil {
		if !prior.Mode().IsRegular() {
			return nil, fmt.Errorf("lock file is not regular: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect lock file: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat lock file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("lock file is not regular: %s", path)
	}
	// Lstat sees the link itself, unlike the opened handle. Comparing their
	// identities catches a link swapped in during open, and a removed path.
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		_ = f.Close()
		return nil, fmt.Errorf("lock file path changed during open: %s", path)
	}
	flags := uint32(LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= LOCKFILE_EXCLUSIVE_LOCK
	}
	ticker := time.NewTicker(lockPollInterval)
	defer ticker.Stop()
	for {
		o := &overlapped{}
		ret, _, callErr := syscall.Syscall6(procLockFileEx.Addr(), 6, f.Fd(), uintptr(flags), 0, 1, 0, uintptr(unsafe.Pointer(o)))
		if ret != 0 {
			current, statErr := os.Lstat(path)
			if statErr != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
				_ = (&fileLock{f: f}).Close()
				return nil, fmt.Errorf("%s: lock file path changed during acquisition", desc)
			}
			return &fileLock{f: f}, nil
		}
		if callErr != errorLockViolation {
			_ = f.Close()
			return nil, fmt.Errorf("%s: %w", desc, callErr)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
