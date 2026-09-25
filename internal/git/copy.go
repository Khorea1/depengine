package git

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func copyArtifact(ctx context.Context, src, dst string) error {
	srcParent, srcName := filepath.Split(filepath.Clean(src))
	if srcParent == "" {
		srcParent = "."
	}
	if srcName == "" {
		srcName = "."
	}

	srcParentRoot, err := os.OpenRoot(srcParent)
	if err != nil {
		return err
	}
	defer srcParentRoot.Close()

	info, err := srcParentRoot.Lstat(srcName)
	if err != nil {
		return err
	}

	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer dstRoot.Close()

	if info.IsDir() {
		srcRoot, err := openVerifiedSubroot(srcParentRoot, srcName, info)
		if err != nil {
			return err
		}
		defer srcRoot.Close()
		return copyRootContents(ctx, srcRoot, dstRoot, ".")
	}

	return copyRootEntry(ctx, srcParentRoot, dstRoot, srcName, filepath.Base(src), filepath.Base(src), info)
}

func copyRootContents(ctx context.Context, srcRoot, dstRoot *os.Root, relPrefix string) error {
	entries, err := fs.ReadDir(srcRoot.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := srcRoot.Lstat(entry.Name())
		if err != nil {
			return err
		}
		globalRel := filepath.Join(relPrefix, entry.Name())
		if err := copyRootEntry(ctx, srcRoot, dstRoot, entry.Name(), entry.Name(), globalRel, info); err != nil {
			return err
		}
	}
	return nil
}

func copyRootEntry(ctx context.Context, srcRoot, dstRoot *os.Root, srcName, dstName, globalRel string, info fs.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch {
	case info.IsDir():
		dstInfo, err := ensureRootDirectory(dstRoot, dstName, info.Mode().Perm())
		if err != nil {
			return err
		}

		srcChild, err := openVerifiedSubroot(srcRoot, srcName, info)
		if err != nil {
			return err
		}
		defer srcChild.Close()

		dstChild, err := openVerifiedSubroot(dstRoot, dstName, dstInfo)
		if err != nil {
			return err
		}
		defer dstChild.Close()

		return copyRootContents(ctx, srcChild, dstChild, globalRel)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := srcRoot.Readlink(srcName)
		if err != nil {
			return err
		}
		if err := validateCopiedSymlinkTarget(globalRel, target); err != nil {
			return err
		}
		if err := removeRootCopyTarget(dstRoot, dstName); err != nil {
			return err
		}
		return dstRoot.Symlink(target, dstName)
	case info.Mode().IsRegular():
		return copyRegularRootFile(srcRoot, dstRoot, srcName, dstName, info)
	default:
		return fmt.Errorf("unsupported file type %s", globalRel)
	}
}

func ensureRootDirectory(root *os.Root, name string, mode fs.FileMode) (fs.FileInfo, error) {
	info, err := root.Lstat(name)
	switch {
	case os.IsNotExist(err):
		if err := root.Mkdir(name, mode); err != nil {
			return nil, err
		}
		info, err = root.Lstat(name)
	case err != nil:
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("replace %s: destination is not a directory", name)
	}
	return info, nil
}

func openVerifiedSubroot(root *os.Root, name string, expected fs.FileInfo) (*os.Root, error) {
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := child.Stat(".")
	if err != nil {
		_ = child.Close()
		return nil, err
	}
	if !actual.IsDir() || !os.SameFile(expected, actual) {
		_ = child.Close()
		return nil, fmt.Errorf("directory %s changed during copy", name)
	}
	return child, nil
}

func validateCopiedSymlinkTarget(linkRel, target string) error {
	if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
		return fmt.Errorf("symlink %s points outside copied artifact", linkRel)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkRel), target))
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) || filepath.IsAbs(resolved) {
		return fmt.Errorf("symlink %s points outside copied artifact", linkRel)
	}
	return nil
}

func copyRegularRootFile(srcRoot, dstRoot *os.Root, srcName, dstName string, expected fs.FileInfo) error {
	in, err := srcRoot.Open(srcName)
	if err != nil {
		return err
	}
	actual, err := in.Stat()
	if err != nil {
		_ = in.Close()
		return err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		_ = in.Close()
		return fmt.Errorf("source %s changed during copy", srcName)
	}
	defer in.Close()

	if err := removeRootCopyTarget(dstRoot, dstName); err != nil {
		return err
	}
	out, err := dstRoot.OpenFile(dstName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, expected.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	chmodErr := out.Chmod(expected.Mode().Perm())
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if chmodErr != nil {
		return chmodErr
	}
	return closeErr
}

func removeRootCopyTarget(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("replace %s: destination is a directory", name)
	}
	return root.Remove(name)
}
