//go:build windows

package lock

import (
	"context"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	transactionKernel32     = syscall.NewLazyDLL("kernel32.dll")
	transactionLockFileEx   = transactionKernel32.NewProc("LockFileEx")
	transactionUnlockFileEx = transactionKernel32.NewProc("UnlockFileEx")
)

const transactionLockViolation = syscall.Errno(33)

type transactionOverlapped struct {
	Internal, InternalHigh uintptr
	Offset, OffsetHigh     uint32
	HEvent                 uintptr
}

func acquireOSFileLock(ctx context.Context, f *os.File) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		o := &transactionOverlapped{}
		ret, _, callErr := syscall.Syscall6(transactionLockFileEx.Addr(), 6, f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(o)))
		if ret != 0 {
			return nil
		}
		if callErr != transactionLockViolation {
			return callErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func releaseOSFileLock(f *os.File) error {
	o := &transactionOverlapped{}
	ret, _, err := syscall.Syscall6(transactionUnlockFileEx.Addr(), 5, f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(o)), 0)
	closeErr := f.Close()
	if ret == 0 {
		return err
	}
	return closeErr
}
