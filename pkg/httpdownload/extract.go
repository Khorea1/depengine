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
		return extractTar(ctx, src, dest, "xzf", rn)
	case ".tar.bz2":
		return extractTar(ctx, src, dest, "xjf", rn)
	case ".tar.xz":
		return extractTar(ctx, src, dest, "xJf", rn)
	case ".tar.zst":
		return extractTar(ctx, src, dest, "--zstd -xf", rn)
	case ".tar":
		return extractTar(ctx, src, dest, "xf", rn)
	case ".zip":
		return extractZip(ctx, src, dest, rn)
	case ".deb":
		return installDeb(ctx, src, rn, sudoRequired)
	default:
		// Treat as a plain binary — copy and chmod.
		return copyBinary(src, dest)
	}
}

func extractTar(ctx context.Context, src, dest, flags string, rn run.Runner) error {
	res := rn.Run(ctx, "tar", flags, src, "-C", dest)
	if res.Err != nil {
		return fmt.Errorf("tar: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("tar: exited %d: %s", res.ExitCode, stderr)
	}
	return nil
}

func extractZip(ctx context.Context, src, dest string, rn run.Runner) error {
	res := rn.Run(ctx, "unzip", "-o", src, "-d", dest)
	if res.Err != nil {
		return fmt.Errorf("unzip: %w", res.Err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("unzip: exited %d", res.ExitCode)
	}
	return nil
}

func installDeb(ctx context.Context, src string, rn run.Runner, sudoRequired bool) error {
	cmd := []string{"dpkg", "-i", src}
	if sudoRequired && os.Geteuid() != 0 {
		cmd = append([]string{"sudo"}, cmd...)
	}
	res := rn.Run(ctx, cmd[0], cmd[1:]...)
	if res.Err != nil {
		return fmt.Errorf("dpkg: %w", res.Err)
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(string(res.Stderr))
		return fmt.Errorf("dpkg: exited %d: %s", res.ExitCode, stderr)
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
