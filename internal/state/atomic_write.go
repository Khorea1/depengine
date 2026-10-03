package state

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

func atomicWritePrivateFile(path string, data []byte, mode fs.FileMode) error {
	return atomicWritePrivateFileWithSync(path, data, mode, func(f *os.File) error { return f.Sync() })
}

func atomicWritePrivateFileWithSync(path string, data []byte, mode fs.FileMode, syncFile func(*os.File) error) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return fmt.Errorf("create private file dir: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmp := f.Name()
	published := false
	defer func() {
		if !published {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("set temp file mode: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := syncFile(f); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("publish temp file: %w", err)
	}
	published = true
	if runtime.GOOS != "windows" {
		d, err := os.Open(dir) // #nosec G304 -- private generated directory.
		if err != nil {
			return fmt.Errorf("open private file dir for sync: %w", err)
		}
		if err := d.Sync(); err != nil {
			_ = d.Close()
			return fmt.Errorf("sync private file dir: %w", err)
		}
		if err := d.Close(); err != nil {
			return fmt.Errorf("close private file dir: %w", err)
		}
	}
	return nil
}
