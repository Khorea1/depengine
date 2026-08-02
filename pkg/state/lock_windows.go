//go:build windows

package state

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	LOCKFILE_EXCLUSIVE_LOCK = 2
)

// overlapped mirrors the Windows OVERLAPPED structure for synchronous
// byte-range locking calls. All fields zeroed means lock from offset 0.
type overlapped struct {
	Internal     uintptr
	InternalHigh uintptr
	Offset       uint32
	OffsetHigh   uint32
	HEvent       uintptr
}

// fileLock implements io.Closer for a LockFileEx/UnlockFileEx-based lock.
type fileLock struct {
	f *os.File
}

func (l *fileLock) Close() error {
	// Release the lock, then close the file.
	hFile := l.f.Fd()
	overlap := &overlapped{}

	syscall.Syscall6(
		procUnlockFileEx.Addr(),
		5,
		hFile,
		0, // dwReserved
		1, // nNumberOfBytesToLockLow  — unlock the same 1 byte we locked
		0, // nNumberOfBytesToLockHigh
		uintptr(unsafe.Pointer(overlap)),
		0,
	)
	return l.f.Close()
}

// lock acquires an exclusive file lock on state.json.lock.
func lock() (io.Closer, error) {
	return lockWithMode(true, "acquire lock")
}

// lockShared acquires a shared (read) file lock on state.json.lock.
func lockShared() (io.Closer, error) {
	return lockWithMode(false, "acquire shared lock")
}

// lockWithMode is the shared implementation for lock and lockShared.
// exclusive=true requests an exclusive lock; false requests a shared (read) lock.
// desc is used in error messages.
func lockWithMode(exclusive bool, desc string) (io.Closer, error) {
	path := DefaultPath() + ".lock"
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	hFile := f.Fd()
	overlap := &overlapped{}

	// dwFlags=2 → exclusive (waits); dwFlags=0 → shared (waits).
	// We do NOT set LOCKFILE_FAIL_IMMEDIATELY so the call blocks until the
	// lock is available (same blocking behaviour as the flock-based Unix path).
	var flags uint32
	if exclusive {
		flags = LOCKFILE_EXCLUSIVE_LOCK
	}

	// BOOL LockFileEx(HANDLE, DWORD, DWORD, DWORD, DWORD, LPOVERLAPPED)
	ret, _, callErr := syscall.Syscall6(
		procLockFileEx.Addr(),
		6,
		hFile,
		uintptr(flags),
		0, // dwReserved
		1, // nNumberOfBytesToLockLow  — lock 1 byte at offset 0
		0, // nNumberOfBytesToLockHigh
		uintptr(unsafe.Pointer(overlap)),
	)
	if ret == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", desc, callErr)
	}

	return &fileLock{f: f}, nil
}
