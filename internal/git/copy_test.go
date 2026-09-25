package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyArtifactCopiesDirectoryContents(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "tool"), []byte("binary"), 0o751); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), src, ".", dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "sub", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("copied content = %q, want binary", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dst, "sub", "tool"))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o751); got != want {
			t.Fatalf("copied mode = %o, want %o", got, want)
		}
	}
}

func TestCopyArtifactCopiesSingleFile(t *testing.T) {
	srcDir := t.TempDir()
	dst := t.TempDir()
	src := filepath.Join(srcDir, "tool")
	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), src, ".", dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "tool")); err != nil || string(got) != "binary" {
		t.Fatalf("copied file = %q, %v", got, err)
	}
}

func TestCopyArtifactReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}

	srcDir := t.TempDir()
	dst := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(filepath.Join(srcDir, "tool"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "tool")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), srcDir, ".", dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "old" {
		t.Fatalf("symlink target was modified: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "tool")); err != nil || string(got) != "new" {
		t.Fatalf("destination = %q, %v", got, err)
	}
}

func TestCopyArtifactRejectsDestinationDirectorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}

	src := t.TempDir()
	dst := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "tool"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "bin")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), src, ".", dst); err == nil {
		t.Fatal("expected destination directory symlink to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "tool")); !os.IsNotExist(err) {
		t.Fatalf("copy escaped destination root: %v", err)
	}
}

func TestCopyArtifactFromRootRejectsIntermediateSourceSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}

	root := t.TempDir()
	dst := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "tool"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), root, filepath.Join("linked", "tool"), dst); err == nil {
		t.Fatal("expected intermediate source symlink escape to be rejected")
	}
	if _, err := os.Stat(filepath.Join(dst, "tool")); !os.IsNotExist(err) {
		t.Fatalf("escaped source was copied: %v", err)
	}
}

func TestCopyArtifactFromRootAllowsContainedIntermediateSourceSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}

	root := t.TempDir()
	dst := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "tool"), []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), root, filepath.Join("linked", "tool"), dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("copied content = %q, want binary", got)
	}
}

func TestCopyArtifactRejectsEscapingSourceSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}

	src := t.TempDir()
	dst := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "tool")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), src, ".", dst); err == nil {
		t.Fatal("expected escaping source symlink to be rejected")
	}
	if _, err := os.Lstat(filepath.Join(dst, "tool")); !os.IsNotExist(err) {
		t.Fatalf("escaping symlink copied: %v", err)
	}
}

func TestCopyArtifactPreservesContainedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}

	src := t.TempDir()
	dst := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "lib"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "lib", "tool"), []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "lib", "tool"), filepath.Join(src, "bin", "tool")); err != nil {
		t.Fatal(err)
	}

	if err := copyArtifactFromRoot(context.Background(), src, ".", dst); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(dst, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("..", "lib", "tool"); target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
}

func TestCopyArtifactHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "tool"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyArtifactFromRoot(ctx, src, ".", t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestArtifactPath(t *testing.T) {
	cloneDir := t.TempDir()
	for _, artifact := range []string{"/", ".", "dist/tool"} {
		path, err := artifactPath(cloneDir, artifact)
		if err != nil {
			t.Fatalf("artifactPath(%q): %v", artifact, err)
		}
		rel, err := filepath.Rel(cloneDir, path)
		if err != nil || rel == ".." || filepath.IsAbs(rel) {
			t.Fatalf("artifactPath(%q) escaped clone: %q", artifact, path)
		}
	}

	if _, err := artifactPath(cloneDir, "../../outside"); err == nil {
		t.Fatal("expected parent traversal to be rejected")
	}
}
