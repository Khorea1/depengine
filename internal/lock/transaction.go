package lock

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AcquireTransaction serializes a read/resolve/write transaction against the
// same lockfile across depengine processes. Callers must acquire it BEFORE
// loading a lock and hold it until the final write has completed. Never remove
// the sidecar: deleting an in-use advisory-lock inode permits split locks.
// Readers that only inspect a lock may continue relying on Save's atomic rename.
func AcquireTransaction(ctx context.Context, path string) (io.Closer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The parent is the caller-selected location of a project lockfile, which
	// may be a shared, traversable repository directory rather than private
	// depengine state. Only the guard file itself must be owner-only.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301,G703 -- shared project lockfile directory from the caller-selected path.
		return nil, fmt.Errorf("lock: prepare transaction directory: %w", err)
	}
	// Canonicalize parent-directory aliases, so foo/../bar and symlinked
	// directory paths cannot take different locks for the same file.
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("lock: resolve transaction directory: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("lock: absolute transaction directory: %w", err)
	}
	name := filepath.Base(path)
	// Save replaces its destination by rename. Following a symlink here would
	// lock the wrong logical path; reject it rather than advertise safety.
	if info, err := os.Lstat(filepath.Join(abs, name)); err == nil { // #nosec G703 -- abs is canonicalized and name is filepath.Base(path).
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("lock: refusing symbolic-link lockfile %q", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("lock: inspect lockfile: %w", err)
	}
	// The guard is deliberately distinct from the atomically renamed TOML.
	// It is persistent and is safe after a process crash: OS locks are released.
	guard := filepath.Join(abs, "."+name+".depengine-txn")
	if info, err := os.Lstat(guard); err == nil { // #nosec G703 -- guard is derived from the canonicalized lockfile path.
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("lock: unsafe transaction guard %q", guard)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("lock: inspect transaction guard: %w", err)
	}
	f, err := os.OpenFile(guard, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304,G703 -- derived next to caller-selected lock path.
	if err != nil {
		return nil, fmt.Errorf("lock: open transaction guard: %w", err)
	}
	info, statErr := os.Lstat(guard) // #nosec G703 -- guard is derived from the canonicalized lockfile path.
	opened, openErr := f.Stat()
	if statErr != nil || openErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, fmt.Errorf("lock: transaction guard changed during open")
	}
	if err := acquireOSFileLock(ctx, f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock: acquire transaction for %q: %w", path, err)
	}
	return &transactionGuard{file: f}, nil
}

type transactionGuard struct{ file *os.File }

func (g *transactionGuard) Close() error { return releaseOSFileLock(g.file) }
