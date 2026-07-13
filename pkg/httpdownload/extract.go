package httpdownload

import (
	"context"
	"fmt"
	"os"
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
	fixRes := runCmd("apt-get", "install", "-f", "-y")
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
