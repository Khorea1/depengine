//go:build windows

package privatepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TempRootPath uses only the OS-provided per-user cache location. Windows
// permissions on that location are inherited from the user's profile ACL.
func TempRootPath() string {
	cacheHome, err := os.UserCacheDir()
	if err != nil || cacheHome == "" {
		return ""
	}
	return filepath.Join(cacheHome, "depengine", "temp")
}

func TempRoot() (string, error) {
	path := TempRootPath()
	if path == "" {
		return "", fmt.Errorf("resolve per-user cache directory")
	}
	if err := EnsurePrivateDir(path); err != nil {
		return path, err
	}
	return path, nil
}

func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

// IsPrivateDir and IsPrivateFile trust only paths rooted in the OS-provided
// per-user cache directory, whose ACL is managed by the user's profile.
func IsPrivateDir(path string) bool {
	info, err := os.Lstat(path)
	root, rootErr := os.UserCacheDir()
	return err == nil && info.IsDir() && rootErr == nil && belowRoot(root, path)
}

func IsPrivateFile(path string) bool {
	info, err := os.Lstat(path)
	root, rootErr := os.UserCacheDir()
	return err == nil && info.Mode().IsRegular() && rootErr == nil && belowRoot(root, path)
}

func belowRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
