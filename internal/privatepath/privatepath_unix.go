//go:build unix

package privatepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// TempRootPath returns the stable per-user directory name inside the system
// temp directory without creating it.
func TempRootPath() string {
	tmp := os.TempDir()
	if tmp == "" || !filepath.IsAbs(tmp) {
		return ""
	}
	return filepath.Join(tmp, fmt.Sprintf("depengine-%d", os.Getuid()))
}

// TempRoot creates and validates the stable per-user directory used when the
// OS user cache/home directory is unavailable.
func TempRoot() (string, error) {
	path := TempRootPath()
	if err := EnsurePrivateDir(path); err != nil {
		return path, err
	}
	return path, nil
}

// EnsurePrivateDir creates and validates a private directory. For fallback
// paths below TempRootPath it validates every component to reject symlinked
// or untrusted ancestors.
func EnsurePrivateDir(path string) error {
	if path == "" {
		return fmt.Errorf("private directory path is empty")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	root := TempRootPath()
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ensurePrivateComponent(path)
	}
	current := root
	if err := ensurePrivateComponent(current); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := ensurePrivateComponent(current); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateComponent(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("%s is not owned by the current user", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- private fallback root must be traversable by its owner.
			return err
		}
		info, err = os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok = info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%s is not a private directory", path)
		}
	}
	return nil
}

// IsPrivateDir reports whether path is a real directory owned by this user and
// inaccessible to group and other users.
func IsPrivateDir(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

// IsPrivateFile reports whether path is a regular, owner-only file owned by
// this user.
func IsPrivateFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
