//go:build !windows

package state

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// fileLock implements io.Closer for a flock-based lock.
type fileLock struct {
	f *os.File
}

func (l *fileLock) Close() error {
	// Release the lock, then close the file.
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// lock acquires an exclusive file lock on state.json.lock.
func lock() (io.Closer, error) {
	return lockWithMode(syscall.LOCK_EX, "acquire lock")
}

// lockShared acquires a shared (read) file lock on state.json.lock.
func lockShared() (io.Closer, error) {
	return lockWithMode(syscall.LOCK_SH, "acquire shared lock")
}

// lockWithMode is the shared implementation for lock and lockShared.
// mode is syscall.LOCK_EX or syscall.LOCK_SH; desc is used in error messages.
func lockWithMode(mode int, desc string) (io.Closer, error) {
	path := DefaultPath() + ".lock"
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	// Retry on EINTR — flock can be interrupted by signals on some systems.
	for {
		if err := syscall.Flock(int(f.Fd()), mode); err != nil {
			if err == syscall.EINTR {
				continue
			}
			_ = f.Close()
			return nil, fmt.Errorf("%s: %w", desc, err)
		}
		break
	}

	return &fileLock{f: f}, nil
}
