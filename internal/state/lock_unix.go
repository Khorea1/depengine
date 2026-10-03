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
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := DefaultPath() + ".lock"
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("create private lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600) // #nosec G304 -- generated owner-only lock path.
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
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
