package git

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func copyArtifact(ctx context.Context, src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyEntry(ctx, src, filepath.Join(dst, filepath.Base(src)), info)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(src, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if err := copyEntry(ctx, path, filepath.Join(dst, entry.Name()), info); err != nil {
			return err
		}
	}
	return nil
}

func copyEntry(ctx context.Context, src, dst string, info fs.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch {
	case info.IsDir():
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		return copyArtifact(ctx, src, dst)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := removeCopyTarget(dst); err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.Mode().IsRegular():
		return copyRegularFile(src, dst, info.Mode().Perm())
	default:
		return fmt.Errorf("unsupported file type %s", src)
	}
}

func copyRegularFile(src, dst string, mode fs.FileMode) error {
	if info, err := os.Lstat(dst); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(dst); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(dst, mode)
}

func removeCopyTarget(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("replace %s: destination is a directory", path)
	}
	return os.Remove(path)
}
