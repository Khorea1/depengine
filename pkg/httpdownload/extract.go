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
	"os/exec"
	"path/filepath"
	"strings"

	"depengine/pkg/run"
)

// Extract decompresses src into dest based on the file extension.
// Delegates to external tools (tar, unzip, dpkg) for v0.1 to avoid
// adding Go archive-library dependencies. Go stdlib archive support
// may replace this in a future version.
func Extract(ctx context.Context, src, dest, ext string, rn run.Runner, sudoRequired bool) error {
	// Ensure destination exists.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("extract: mkdir %s: %w", dest, err)
	}

	// Reject archives containing a "zip slip" / path-traversal member (e.g.
	// "../../etc/cron.d/evil" or an absolute path, or a symlink pointing
	// outside dest) BEFORE handing the file to the system tar/unzip binary.
	// depengine downloads archives from arbitrary schema-declared URLs and
	// frequently extracts them with sudo, so a malicious or compromised
	// upstream release asset must not be able to write outside dest. This
	// check reads the archive with the Go standard library only (no new
	// dependency, no extra subprocess call) and is a no-op if the file can't
	// be parsed — in that case the real extraction command below still runs
	// and reports its own, more specific error.
	if err := validateArchiveSafety(src, dest, ext); err != nil {
		return fmt.Errorf("extract: refusing unsafe archive: %w", err)
	}

	switch ext {
	case ".tar.gz", ".tgz":
		return extractTar(ctx, src, dest, []string{"xzf"}, rn, sudoRequired)
	case ".tar.bz2":
		return extractTar(ctx, src, dest, []string{"xjf"}, rn, sudoRequired)
	case ".tar.xz":
		return extractTar(ctx, src, dest, []string{"xJf"}, rn, sudoRequired)
	case ".tar.zst":
		return extractTar(ctx, src, dest, []string{"--zstd", "-xf"}, rn, sudoRequired)
	case ".tar":
		return extractTar(ctx, src, dest, []string{"xf"}, rn, sudoRequired)
	case ".zip":
		return extractZip(ctx, src, dest, rn, sudoRequired)
	case ".deb":
		return installDeb(ctx, src, rn, sudoRequired)
	default:
		// Treat as a plain binary — copy and chmod.
		return copyBinary(src, dest)
	}
}

func extractTar(ctx context.Context, src, dest string, flags []string, rn run.Runner, sudoRequired bool) error {
	args := append(flags, src, "-C", dest)
	if sudoRequired && os.Geteuid() != 0 {
		res := rn.Run(ctx, "sudo", append([]string{"tar"}, args...)...)
		if res.Err != nil {
			return fmt.Errorf("tar: %w", res.Err)
		}
		if res.ExitCode != 0 {
			stderr := strings.TrimSpace(string(res.Stderr))
			return fmt.Errorf("tar: exited %d: %s", res.ExitCode, stderr)
		}
	} else {
		res := rn.Run(ctx, "tar", args...)
		if res.Err != nil {
			return fmt.Errorf("tar: %w", res.Err)
		}
		if res.ExitCode != 0 {
			stderr := strings.TrimSpace(string(res.Stderr))
			return fmt.Errorf("tar: exited %d: %s", res.ExitCode, stderr)
		}
	}
	return nil
}

func extractZip(ctx context.Context, src, dest string, rn run.Runner, sudoRequired bool) error {
	if sudoRequired && os.Geteuid() != 0 {
		res := rn.Run(ctx, "sudo", "unzip", "-o", src, "-d", dest)
		if res.Err != nil {
			return fmt.Errorf("unzip: %w", res.Err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("unzip: exited %d", res.ExitCode)
		}
	} else {
		res := rn.Run(ctx, "unzip", "-o", src, "-d", dest)
		if res.Err != nil {
			return fmt.Errorf("unzip: %w", res.Err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("unzip: exited %d", res.ExitCode)
		}
	}
	return nil
}

func installDeb(ctx context.Context, src string, rn run.Runner, sudoRequired bool) error {
	// Guard: dpkg must exist on the system.
	if _, err := exec.LookPath("dpkg"); err != nil {
		return fmt.Errorf("cannot install .deb package: dpkg not found (this system is not Debian-based; consider adding a native method fallback)")
	}
	runCmd := func(args ...string) run.Result {
		if sudoRequired && os.Geteuid() != 0 {
			args = append([]string{"sudo"}, args...)
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
	if _, err := exec.LookPath("apt-get"); err != nil {
		if _, err2 := exec.LookPath("apt"); err2 != nil {
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

func copyBinary(src, destDir string) error {
	dest := filepath.Join(destDir, filepath.Base(src))
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("copy: read %s: %w", src, err)
	}
	if err := os.WriteFile(dest, input, 0o755); err != nil {
		return fmt.Errorf("copy: write %s: %w", dest, err)
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
// path to exist.
func safeJoin(dest, name string) error {
	if name == "" {
		return fmt.Errorf("empty entry name")
	}
	if filepath.IsAbs(name) {
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
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("unsafe tar entry: %q links outside destination to absolute path %q", hdr.Name, hdr.Linkname)
			}
			if err := safeJoin(dest, filepath.Join(filepath.Dir(hdr.Name), hdr.Linkname)); err != nil {
				return fmt.Errorf("unsafe tar entry: %q link target escapes destination: %w", hdr.Name, err)
			}
		}
	}
	return nil
}
