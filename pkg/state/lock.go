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

// lock acquires an exclusive file lock on state.json.lock, creating the
// lock file and parent directories as needed. Returns an io.Closer that
// releases the lock. The lock is advisory (flock), so concurrent processes
// that do not call lock may still read/write the state file.
func lock() (io.Closer, error) {
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
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			if err == syscall.EINTR {
				continue
			}
			_ = f.Close()
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		break
	}

	return &fileLock{f: f}, nil
}

// lockShared acquires a shared (read) file lock on state.json.lock.
// Multiple processes may hold a shared lock simultaneously; an exclusive
// lock (lock) blocks until all shared locks are released.
func lockShared() (io.Closer, error) {
	path := DefaultPath() + ".lock"
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
			if err == syscall.EINTR {
				continue
			}
			_ = f.Close()
			return nil, fmt.Errorf("acquire shared lock: %w", err)
		}
		break
	}

	return &fileLock{f: f}, nil
}
