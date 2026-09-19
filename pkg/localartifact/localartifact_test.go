package localartifact_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	artifactcontract "github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/localartifact"
	"github.com/Khorea1/depengine/pkg/plan"
)

func TestResolveRawArtifactProducesPortableIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vendor", "tool")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("offline payload"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := localartifact.Resolve(root, "vendor/tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Artifact.Kind != plan.ArtifactRaw {
		t.Fatalf("kind = %q, want raw", got.Artifact.Kind)
	}
	if got.Artifact.LocalPath != "vendor/tool" {
		t.Fatalf("local path = %q", got.Artifact.LocalPath)
	}
	if !strings.HasPrefix(got.Artifact.Checksum, "sha256:") {
		t.Fatalf("checksum = %q", got.Artifact.Checksum)
	}
	if strings.Contains(got.Artifact.LocalPath, root) {
		t.Fatalf("portable identity contains machine root: %q", got.Artifact.LocalPath)
	}
}

func TestResolveArchiveAndChecksum(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vendor", "tool.tar.gz")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("archive bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := localartifact.Resolve(root, "vendor/tool.tar.gz", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact.Kind != plan.ArtifactArchive {
		t.Fatalf("kind = %q, want archive", first.Artifact.Kind)
	}
	if _, err := localartifact.Resolve(root, "vendor/tool.tar.gz", first.Artifact.Checksum); err != nil {
		t.Fatalf("verified resolve: %v", err)
	}
	if _, err := localartifact.Resolve(root, "vendor/tool.tar.gz", "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("checksum mismatch unexpectedly succeeded")
	}
}

func TestResolveRejectsEscapesSymlinksAndInstallers(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := localartifact.Resolve(root, "../outside", ""); err == nil {
		t.Fatal("path escape unexpectedly succeeded")
	}

	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, err := localartifact.Resolve(root, "link", ""); err == nil {
			t.Fatal("symlink unexpectedly succeeded")
		}
	}

	installer := filepath.Join(root, "setup.exe")
	if err := os.WriteFile(installer, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := localartifact.Resolve(root, "setup.exe", "")
	var forbidden *artifactcontract.ForbiddenExtensionError
	if !errors.As(err, &forbidden) {
		t.Fatalf("error = %T %v, want ForbiddenExtensionError", err, err)
	}
}

func TestResolveRejectsUnsupportedArchiveAndInvalidChecksum(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.7z")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := localartifact.Resolve(root, "tool.7z", "")
	var unsupported *artifactcontract.UnsupportedArchiveError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T %v, want UnsupportedArchiveError", err, err)
	}

	raw := filepath.Join(root, "tool")
	if err := os.WriteFile(raw, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, checksum := range []string{"md5:abcd", "sha256:abcd", "sha256:" + strings.Repeat("z", 64)} {
		if _, err := localartifact.Resolve(root, "tool", checksum); err == nil {
			t.Fatalf("checksum %q unexpectedly accepted", checksum)
		}
	}
}

func TestResolveRejectsParentSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "tool"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "vendor")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := localartifact.Resolve(root, "vendor/tool", ""); err == nil {
		t.Fatal("expected parent symlink escape rejection")
	}
}
