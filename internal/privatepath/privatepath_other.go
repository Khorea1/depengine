//go:build !unix && !windows

package privatepath

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

func TempRootPath() string {
	if cacheHome, err := os.UserCacheDir(); err == nil && cacheHome != "" {
		return filepath.Join(cacheHome, "depengine", "temp")
	}
	current, err := user.Current()
	if err != nil {
		return ""
	}
	return filepath.Join(os.TempDir(), "depengine-"+current.Uid)
}

func TempRoot() (string, error) {
	path := TempRootPath()
	if path == "" {
		return "", fmt.Errorf("resolve current user for private temp directory")
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

func IsPrivateDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func IsPrivateFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
