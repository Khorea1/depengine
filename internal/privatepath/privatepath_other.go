//go:build !unix && !windows

package privatepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func TempRootPath() string {
	cacheHome, err := os.UserCacheDir()
	if err != nil || cacheHome == "" || !filepath.IsAbs(cacheHome) {
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
	if path == "" {
		return fmt.Errorf("private directory path is empty")
	}
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
