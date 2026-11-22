//go:build !windows

package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Khorea1/depengine/internal/log"
)

const lockPollInterval = 50 * time.Millisecond

// exclusiveLockMode is the platform value passed to lockFilePathContext for an
// exclusive (writer) lock.
const exclusiveLockMode = syscall.LOCK_EX

// sharedLockMode is the platform value passed to lockFilePathContext for a
// shared (reader) lock.
const sharedLockMode = syscall.LOCK_SH

type fileLock struct{ f *os.File }

func (l *fileLock) Close() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		log.Default.Warn("flock unlock failed", "error", err)
	}
	return l.f.Close()
}

func lock() (io.Closer, error) { return lockContext(context.Background()) }
func lockContext(ctx context.Context) (io.Closer, error) {
	return lockWithModeContext(ctx, syscall.LOCK_EX, "acquire lock")
}
func lockShared() (io.Closer, error) { return lockSharedContext(context.Background()) }
func lockSharedContext(ctx context.Context) (io.Closer, error) {
	return lockWithModeContext(ctx, syscall.LOCK_SH, "acquire shared lock")
}

func lockWithModeContext(ctx context.Context, mode int, desc string) (io.Closer, error) {
	return lockFilePathContext(ctx, DefaultPath()+".lock", mode, desc)
}

// lockSharedFilePathContext acquires a shared lock on an arbitrary private
// lock file. Journal queries use it to exclude rotation and in-flight appends.
func lockSharedFilePathContext(ctx context.Context, path, desc string) (io.Closer, error) {
	return lockFilePathContext(ctx, path, sharedLockMode, desc)
}

// lockFilePathContext locks an arbitrary private lock file. Journal writes use
// their own lock so appending cannot deadlock against a held state lock.
func lockFilePathContext(ctx context.Context, path string, mode int, desc string) (io.Closer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("create private lock dir: %w", err)
	}
	// A lock path is a stable synchronization object, not arbitrary content.
	// Never follow a symlink into another file and never block opening a FIFO.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600) // #nosec G304 -- generated private lock path; no symlink traversal.
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
		return nil, fmt.Errorf("lock file is not a regular file: %s", path)
	}
	// chmod on a hardlinked lock would silently chmod an unrelated file.
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		_ = f.Close()
		return nil, fmt.Errorf("lock file must have exactly one link: %s", path)
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("tighten lock file permissions: %w", err)
	}
	ticker := time.NewTicker(lockPollInterval)
	defer ticker.Stop()
	for {
		err := syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			// Reject a pathname swapped while we were waiting. File locks
			// protect inodes, so locking an orphaned inode is not exclusion.
			current, statErr := os.Lstat(path)
			if statErr != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
				_ = (&fileLock{f: f}).Close()
				return nil, fmt.Errorf("%s: lock file path changed during acquisition", desc)
			}
			return &fileLock{f: f}, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, fmt.Errorf("%s: %w", desc, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
