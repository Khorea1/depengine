package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
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
// ZIP and uncompressed TAR archives are extracted in-process through an
// os.Root-confined materializer. Compressed TAR variants still use the
// existing subprocess backend until their decompression streams are moved
// behind the same rooted materialization boundary.
func Extract(ctx context.Context, src, dest, ext string, rn run.Runner, sudoRequired bool, toolName string) error {
	return extract(ctx, src, dest, ext, "", rn, sudoRequired, toolName)
}

func extract(ctx context.Context, src, dest, ext, binaryName string, rn run.Runner, sudoRequired bool, toolName string) error {
	// Ensure destination exists.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("extract: mkdir %s: %w", dest, err)
	}

	// Native ZIP/TAR extraction validates each entry at the write boundary.
	// Compressed TAR variants still delegated to host tar retain the legacy
	// preflight safety validation until they are migrated to streamed native
	// materialization in the next phase.
	if ext != ".zip" && ext != ".tar" {
		if err := validateArchiveSafety(src, dest, ext); err != nil {
			return fmt.Errorf("extract: refusing unsafe archive: %w", err)
		}
	}

	switch ext {
	case ".tar.gz", ".tgz":
		return extractTar(ctx, src, dest, []string{"xzf"}, rn, sudoRequired, toolName)
	case ".tar.bz2":
		return extractTar(ctx, src, dest, []string{"xjf"}, rn, sudoRequired, toolName)
	case ".tar.xz":
		return extractTar(ctx, src, dest, []string{"xJf"}, rn, sudoRequired, toolName)
	case ".tar.zst":
		return extractTar(ctx, src, dest, []string{"--zstd", "-xf"}, rn, sudoRequired, toolName)
	case ".tar":
		return extractNativeTar(ctx, src, dest)
	case ".zip":
		return extractNativeZip(ctx, src, dest)
	case ".bz2":
		return extractBzip2(ctx, src, dest, binaryName, rn, sudoRequired, toolName)
	case ".deb":
		return installDeb(ctx, src, rn, sudoRequired, toolName)
	default:
		// Treat as a plain binary — copy and chmod.
		return copyBinary(ctx, src, dest, binaryName, rn, sudoRequired, toolName)
	}
}

func extractBzip2(ctx context.Context, src, dest, binaryName string, rn run.Runner, sudoRequired bool, toolName string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("bzip2: open %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp("", ".depengine-bzip2-*")
	if err != nil {
		return fmt.Errorf("bzip2: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, bzip2.NewReader(in)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("bzip2: decompress %s: %w", src, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("bzip2: close temporary file: %w", err)
	}
	return copyBinary(ctx, tmpName, dest, binaryName, rn, sudoRequired, toolName)
}

func extractTar(ctx context.Context, src, dest string, flags []string, rn run.Runner, sudoRequired bool, toolName string) error {
	args := append(append([]string(nil), flags...), src, "-C", dest)
	if sudoRequired && os.Geteuid() != 0 {
		if err := elevationGuard(sudoRequired, toolName); err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		sudoBin := run.ElevationPrefix()[0]
		res := rn.Run(ctx, sudoBin, append([]string{"tar"}, args...)...)
		if err := run.CheckResult(res, "tar"); err != nil {
			return err
		}
	} else {
		res := rn.Run(ctx, "tar", args...)
		if err := run.CheckResult(res, "tar"); err != nil {
			return err
		}
	}
	return nil
}

func extractZip(ctx context.Context, src, dest string, rn run.Runner, sudoRequired bool, toolName string) error {
	if sudoRequired && os.Geteuid() != 0 {
		if err := elevationGuard(sudoRequired, toolName); err != nil {
			return fmt.Errorf("unzip: %w", err)
		}
		sudoBin := run.ElevationPrefix()[0]
		res := rn.Run(ctx, sudoBin, "unzip", "-o", src, "-d", dest)
		if err := run.CheckResult(res, "unzip"); err != nil {
			return err
		}
	} else {
		res := rn.Run(ctx, "unzip", "-o", src, "-d", dest)
		if err := run.CheckResult(res, "unzip"); err != nil {
			return err
		}
	}
	return nil
}

func installDeb(ctx context.Context, src string, rn run.Runner, sudoRequired bool, toolName string) error {
	// Guard: dpkg must exist on the system. Host/distribution compatibility is
	// enforced by the executor before this mutation boundary; this check remains
	// necessary for minimal Debian-family/Termux environments where the package
	// format is compatible but dpkg itself is unavailable.
	if !run.LookPath(ctx, rn, "dpkg") {
		return fmt.Errorf("cannot install .deb package: dpkg not found (a compatible dpkg-based target is required; consider adding a native method fallback)")
	}
	var sudoBin string
	if sudoRequired && os.Geteuid() != 0 {
		if err := elevationGuard(sudoRequired, toolName); err != nil {
			return fmt.Errorf("dpkg: %w", err)
		}
		sudoBin = run.ElevationPrefix()[0]
	}
	runCmd := func(args ...string) run.Result {
		if sudoBin != "" {
			args = append([]string{sudoBin}, args...)
		}
		return rn.Run(ctx, args[0], args[1:]...)
	}

	// Try dpkg -i directly.
	res := runCmd("dpkg", "-i", src)
	if res.Err == nil && res.ExitCode == 0 {
		return nil
	}
	// dpkg -i may fail due to missing dependencies. Run apt-get install -f
	// to fix them, then try dpkg -i again.
	// Check which apt variant is available (apt-get preferred, apt fallback).
	aptCmd := "apt-get"
	if !run.LookPath(ctx, rn, "apt-get") {
		if !run.LookPath(ctx, rn, "apt") {
			return fmt.Errorf("neither apt-get nor apt found to fix dependencies")
		}
		aptCmd = "apt"
	}
	fixRes := runCmd(aptCmd, "install", "-f", "-y")
	if fixRes.Err != nil || fixRes.ExitCode != 0 {
		stderr := strings.TrimSpace(string(fixRes.Stderr))
		if res.Err != nil {
			return fmt.Errorf("dpkg: %w (apt-get -f install also failed: %s)", res.Err, stderr)
		}
		return fmt.Errorf("dpkg: exited %d (apt-get -f install also failed: exit %d: %s)", res.ExitCode, fixRes.ExitCode, stderr)
	}

	// Retry dpkg -i after fixing deps.
	res2 := runCmd("dpkg", "-i", src)
	if res2.Err != nil {
		return fmt.Errorf("dpkg (after apt-get -f install): %w", res2.Err)
	}
	if res2.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res2.Stderr))
		return fmt.Errorf("dpkg (after apt-get -f install): exited %d: %s", res2.ExitCode, stderr)
	}
	return nil
}

// copyBinary installs src as a single executable file inside destDir. When
// sudoRequired is set and the process isn't already root, os.WriteFile can't
// help — an unprivileged process has no way to write into a root-owned
// directory — so the copy is done via an elevated `install`, mirroring how
// extractTar/extractZip/installDeb already shell out through
// run.ElevationPrefix() instead of touching the filesystem directly.
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

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("copy: read %s: %w", src, err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(destDir, ".depengine-binary-*")
	if err != nil {
		return fmt.Errorf("copy: stage %s: %w", dest, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
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

// validateArchiveSafety inspects an archive's member paths and rejects any
// entry that would escape dest once extracted. Supported without any new
// dependency because the Go standard library already implements these
// formats:
//
//   - .zip            → archive/zip
//   - .tar            → archive/tar
//   - .tar.gz / .tgz  → archive/tar + compress/gzip
//   - .tar.bz2        → archive/tar + compress/bzip2
//
// .tar.xz and .tar.zst have no decompressor in the standard library, so this
// check is skipped for those two extensions and extraction proceeds via the
// system `tar` binary as before, retaining whatever protections it ships
// with (modern GNU tar refuses ".." members by default; behavior on older or
// busybox tar varies, which is exactly why this function exists for the
// formats it *can* check).
func validateArchiveSafety(src, dest, ext string) error {
	switch ext {
	case ".zip":
		return validateZipSafety(src, dest)
	case ".tar", ".tar.gz", ".tgz", ".tar.bz2":
		return validateTarSafety(src, dest, ext)
	default:
		return nil
	}
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

// validateZipSafety opens src as a zip archive and checks every member name.
// If src can't be opened/parsed as a zip, it returns nil — extraction is
// left to `unzip`, which will report a more specific error for a genuinely
// corrupt file.
func validateZipSafety(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return nil
	}
	defer r.Close()

	for _, f := range r.File {
		if err := safeJoin(dest, f.Name); err != nil {
			return fmt.Errorf("unsafe zip entry: %w", err)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe zip entry: symlink %q is not supported", f.Name)
		}
	}
	return nil
}

// validateTarSafety opens src as a (optionally gzip/bzip2-compressed) tar
// archive and checks every member name, plus the link target of any
// symlink/hardlink entry. If src can't be opened/decompressed/parsed, it
// returns nil — extraction is left to `tar`, which will report a more
// specific error for a genuinely corrupt file.
func validateTarSafety(src, dest, ext string) error {
	f, err := os.Open(src)
	if err != nil {
		return nil
	}
	defer f.Close()

	var r io.Reader = f
	switch ext {
	case ".tar.gz", ".tgz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil
		}
		defer gz.Close()
		r = gz
	case ".tar.bz2":
		r = bzip2.NewReader(f)
	}

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		if err := safeJoin(dest, hdr.Name); err != nil {
			return fmt.Errorf("unsafe tar entry: %w", err)
		}
		if (hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink) && hdr.Linkname != "" {
			linkTarget := hdr.Linkname
			if path.IsAbs(linkTarget) {
				return fmt.Errorf("unsafe tar entry: %q links outside destination to absolute path %q", hdr.Name, linkTarget)
			}
			if hdr.Typeflag == tar.TypeSymlink {
				linkTarget = path.Join(path.Dir(hdr.Name), linkTarget) // #nosec G305 -- Normalize the archive-relative target; safeJoin below checks containment.
			}
			if err := safeJoin(dest, linkTarget); err != nil {
				return fmt.Errorf("unsafe tar entry: %q link target escapes destination: %w", hdr.Name, err)
			}
		}
	}
	return nil
}
