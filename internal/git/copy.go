package git

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// copyArtifactFromRoot copies srcName while keeping every source lookup rooted
// under srcRootPath. Production passes the clone directory as srcRootPath, so
// intermediate symlinks in an artifact path cannot redirect reads outside the
// checkout before the root boundary is established.
func copyArtifactFromRoot(ctx context.Context, srcRootPath, srcName, dst string) (retErr error) {
	srcName = filepath.Clean(srcName)
	if filepath.IsAbs(srcName) || srcName == ".." || strings.HasPrefix(srcName, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source %s escapes copy root", srcName)
	}

	srcParentRoot, err := os.OpenRoot(srcRootPath)
	if err != nil {
		return err
	}
	defer closeRoot(srcParentRoot, &retErr)

	info, err := srcParentRoot.Lstat(srcName)
	if err != nil {
		return err
	}

	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer closeRoot(dstRoot, &retErr)

	if info.IsDir() {
		srcRoot, err := openVerifiedSubroot(srcParentRoot, srcName, info)
		if err != nil {
			return err
		}
		defer closeRoot(srcRoot, &retErr)
		return copyRootContents(ctx, srcRoot, dstRoot, ".")
	}

	return copyRootEntry(ctx, srcParentRoot, dstRoot, srcName, filepath.Base(srcName), filepath.Base(srcName), info)
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

func copyRootEntry(ctx context.Context, srcRoot, dstRoot *os.Root, srcName, dstName, globalRel string, info fs.FileInfo) (retErr error) {
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
		defer closeRoot(srcChild, &retErr)

		dstChild, err := openVerifiedSubroot(dstRoot, dstName, dstInfo)
		if err != nil {
			return err
		}
		defer closeRoot(dstChild, &retErr)

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

func closeRoot(root *os.Root, retErr *error) {
	if err := root.Close(); err != nil && *retErr == nil {
		*retErr = err
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
	defer func() { _ = in.Close() }()

	if info, err := dstRoot.Lstat(dstName); err == nil {
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("replace %s: destination is a directory", dstName)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// Write a sibling staging file and rename it over the destination instead
	// of unlinking the old file first. The rename is atomic on one filesystem,
	// so a failed build or interrupted copy cannot leave an installed binary
	// missing or truncated.
	out, stageName, err := createRootTempFile(dstRoot, expected.Mode().Perm())
	if err != nil {
		return err
	}
	removeStage := true
	defer func() {
		if removeStage {
			_ = dstRoot.Remove(stageName)
		}
	}()
	_, copyErr := io.Copy(out, in)
	chmodErr := out.Chmod(expected.Mode().Perm())
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if chmodErr != nil {
		return chmodErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := dstRoot.Rename(stageName, dstName); err != nil {
		return err
	}
	removeStage = false
	return nil
}

// createRootTempFile allocates an exclusive staging file below root. The
// random name avoids collisions between concurrent installations that share a
// destination directory without introducing a lock file into user directories.
func createRootTempFile(root *os.Root, perm fs.FileMode) (*os.File, string, error) {
	for i := 0; i < 16; i++ {
		var suffix [12]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, "", err
		}
		name := ".depengine-copy-" + hex.EncodeToString(suffix[:])
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err == nil {
			return file, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("cannot allocate a temporary copy name")
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
