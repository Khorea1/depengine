package httpdownload

import (
	"compress/bzip2"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

// elevationGuard checks whether sudo-required elevation is available.
// Returns nil when elevation is available or not needed.
// Returns an error when sudo is required but no working elevation method
// is available and we're not already root.
func elevationGuard(sudoRequired bool, toolName string) error {
	if !sudoRequired || os.Geteuid() == 0 {
		return nil
	}
	if run.ElevationPrefix() != nil {
		return nil
	}
	var hint string
	if runtime.GOOS == "windows" {
		hint = "Run this command in an elevated (Administrator) shell"
	} else {
		hint = "Install sudo with passwordless access, or run as root"
	}
	if toolName != "" {
		return fmt.Errorf("cannot elevate for tool %q: %s", toolName, hint)
	}
	return fmt.Errorf("cannot elevate: no working elevation method (sudo/doas/pkexec) available. %s", hint)
}

// defaultSudoRequired reports whether installing into dest needs elevation
// when sudo_required is not set in the schema: destinations under the user's
// home directory are user-writable (e.g. ~/.local/share/fonts on Linux,
// ~/Library on macOS) and default to NO sudo; everything else (the
// /usr/local/bin default, /opt, system font dirs…) needs elevation.
func defaultSudoRequired(dest string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return true // can't determine home → stay safe
	}
	home = filepath.Clean(home)
	dest = filepath.Clean(dest)
	if dest == home {
		return false
	}
	return !strings.HasPrefix(dest, home+string(filepath.Separator))
}

// Extract materializes src into dest based on the file extension.
// ZIP and every supported TAR variant are written through an os.Root-confined
// materializer. XZ/Zstd use external decoders only as byte producers; those
// subprocesses never receive the extraction destination.
func Extract(ctx context.Context, src, dest, ext string, rn run.Runner, sudoRequired bool, toolName string) error {
	return extract(ctx, src, dest, ext, "", rn, sudoRequired, toolName)
}

func extract(ctx context.Context, src, dest, ext, binaryName string, rn run.Runner, sudoRequired bool, toolName string) error {
	if ext == ".deb" {
		return fmt.Errorf("http: .deb package lifecycle is unsupported because exact dpkg package identity is not persisted; use a native package-manager method")
	}
	// Ensure destination exists.
	if err := os.MkdirAll(dest, 0o755); err != nil { // #nosec G301 -- Extraction destinations are installed payload roots and must remain traversable.
		return fmt.Errorf("extract: mkdir %s: %w", dest, err)
	}

	switch ext {
	case ".tar", ".tar.gz", ".tgz", ".tar.bz2":
		return extractNativeTar(ctx, src, dest, ext)
	case ".tar.xz", ".tar.zst":
		return extractExternalTar(ctx, src, dest, ext, rn)
	case ".zip":
		return extractNativeZip(ctx, src, dest)
	case ".bz2":
		return extractBzip2(ctx, src, dest, binaryName, rn, sudoRequired, toolName)
	default:
		// Treat as a plain binary — copy and chmod.
		return copyBinary(ctx, src, dest, binaryName, rn, sudoRequired, toolName)
	}
}

func extractBzip2(ctx context.Context, src, dest, binaryName string, rn run.Runner, sudoRequired bool, toolName string) error {
	return extractBzip2WithBudget(ctx, src, dest, binaryName, rn, sudoRequired, toolName, &archiveExpansionBudget{})
}

func extractBzip2WithBudget(ctx context.Context, src, dest, binaryName string, rn run.Runner, sudoRequired bool, toolName string, budget *archiveExpansionBudget) error {
	in, err := os.Open(src) // #nosec G304 -- src is the depengine-managed downloaded archive path.
	if err != nil {
		return fmt.Errorf("bzip2: open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	tmp, err := os.CreateTemp("", ".depengine-bzip2-*")
	if err != nil {
		return fmt.Errorf("bzip2: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := copyBzip2(ctx, tmp, in, budget); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("bzip2: decompress %s: %w", src, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("bzip2: close temporary file: %w", err)
	}
	return copyBinary(ctx, tmpName, dest, binaryName, rn, sudoRequired, toolName)
}

func copyBzip2(ctx context.Context, dst io.Writer, src io.Reader, budget *archiveExpansionBudget) error {
	_, err := io.Copy(budget.writer(dst), contextReader{ctx: ctx, r: bzip2.NewReader(src)})
	return err
}

// copyBinary installs src as a single executable file inside destDir. When
// sudoRequired is set and the process isn't already root, os.WriteFile can't
// help — an unprivileged process has no way to write into a root-owned
// directory — so the copy is done via an elevated `install`, mirroring how
// installDeb shells out through run.ElevationPrefix() instead of touching the
// filesystem directly.
// `install -m 0755` also creates the destination with the right mode in one
// step, avoiding a separate chmod call under sudo.
func copyBinary(ctx context.Context, src, destDir, binaryName string, rn run.Runner, sudoRequired bool, toolName string) error {
	if binaryName == "" {
		binaryName = filepath.Base(src)
	}
	dest := filepath.Join(destDir, binaryName)

	if sudoRequired && os.Geteuid() != 0 {
		if err := elevationGuard(sudoRequired, toolName); err != nil {
			return fmt.Errorf("copy: %w", err)
		}
		sudoBin := run.ElevationPrefix()[0]
		tmp := dest + ".depengine-new"
		res := rn.Run(ctx, sudoBin, "install", "-m", "0755", src, tmp)
		if err := run.CheckResult(res, "install"); err != nil {
			return err
		}
		return run.CheckResult(rn.Run(ctx, sudoBin, "mv", "-f", "--", tmp, dest), "install commit")
	}

	in, err := os.Open(src) // #nosec G304 -- src is the depengine-managed downloaded archive path.
	if err != nil {
		return fmt.Errorf("copy: read %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(destDir, ".depengine-binary-*")
	if err != nil {
		return fmt.Errorf("copy: stage %s: %w", dest, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy: write %s: %w", dest, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("copy: commit %s: %w", dest, err)
	}
	return nil
}

// safeJoin joins name onto dest and confirms the result does not escape
// dest, rejecting absolute paths and ".." traversal. It does not require the
// path to exist. name is an ARCHIVE entry, which always uses "/" separators,
// so absoluteness is tested with path.IsAbs: filepath.IsAbs would miss
// "/absolute" entries on Windows and hand them to the system extractor.
func safeJoin(dest, name string) error {
	if name == "" {
		return fmt.Errorf("empty entry name")
	}
	if path.IsAbs(name) {
		return fmt.Errorf("absolute path in archive entry: %q", name)
	}
	cleanDest := filepath.Clean(dest)
	joined := filepath.Join(cleanDest, name)
	if joined != cleanDest && !strings.HasPrefix(joined, cleanDest+string(os.PathSeparator)) {
		return fmt.Errorf("entry escapes destination: %q", name)
	}
	return nil
}
