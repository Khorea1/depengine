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

		inside := filepath.Join(root, "inside")
		if err := os.WriteFile(inside, []byte("inside"), 0o644); err != nil {
			t.Fatal(err)
		}
		insideLink := filepath.Join(root, "inside-link")
		if err := os.Symlink(inside, insideLink); err != nil {
			t.Fatal(err)
		}
		if _, err := localartifact.Resolve(root, "inside-link", ""); err == nil {
			t.Fatal("in-project final symlink unexpectedly succeeded")
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
	for _, name := range []string{"tool.7z", "tool.tar.xz", "tool.tar.zst", "tool.tar.bz2", "tool.bz2"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := localartifact.Resolve(root, name, "")
		var unsupported *artifactcontract.UnsupportedArchiveError
		if !errors.As(err, &unsupported) {
			t.Fatalf("Resolve(%q) error = %T %v, want UnsupportedArchiveError", name, err, err)
		}
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

func TestResolveRejectsParentSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require privileges on some Windows environments")
	}
	root := t.TempDir()

	insideDir := filepath.Join(root, "real-vendor")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(insideDir, "tool"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(insideDir, filepath.Join(root, "vendor")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := localartifact.Resolve(root, "vendor/tool", ""); err == nil {
		t.Fatal("in-project parent symlink unexpectedly succeeded")
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "tool"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-vendor")); err != nil {
		t.Fatal(err)
	}
	if _, err := localartifact.Resolve(root, "outside-vendor/tool", ""); err == nil {
		t.Fatal("escaping parent symlink unexpectedly succeeded")
	}
}

func TestClassifyProjectPathUsesOfflineFormatsOnly(t *testing.T) {
	for _, tt := range []struct {
		path string
		kind plan.ArtifactKind
	}{
		{path: "vendor/tool", kind: plan.ArtifactRaw},
		{path: "vendor/tool.zip", kind: plan.ArtifactArchive},
		{path: "vendor/tool.tar", kind: plan.ArtifactArchive},
		{path: "vendor/tool.tar.gz", kind: plan.ArtifactArchive},
		{path: "vendor/tool.tgz", kind: plan.ArtifactArchive},
	} {
		got, err := localartifact.ClassifyProjectPath(tt.path)
		if err != nil {
			t.Fatalf("ClassifyProjectPath(%q): %v", tt.path, err)
		}
		if got != tt.kind {
			t.Fatalf("ClassifyProjectPath(%q) = %q, want %q", tt.path, got, tt.kind)
		}
	}
	for _, path := range []string{"../tool", "vendor/tool.tar.xz", "vendor/setup.exe"} {
		if _, err := localartifact.ClassifyProjectPath(path); err == nil {
			t.Fatalf("ClassifyProjectPath(%q) unexpectedly succeeded", path)
		}
	}
}

func TestVerifyRegularFileChecksumRejectsDriftAndSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool")
	if err := os.WriteFile(path, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyRegularFileChecksum(path, resolved.Artifact.Checksum); err != nil {
		t.Fatalf("VerifyRegularFileChecksum(): %v", err)
	}
	if err := os.WriteFile(path, []byte("drift"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyRegularFileChecksum(path, resolved.Artifact.Checksum); err == nil {
		t.Fatal("drifted file unexpectedly verified")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err := os.Symlink(path, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := localartifact.VerifyRegularFileChecksum(link, resolved.Artifact.Checksum); err == nil {
			t.Fatal("symlink unexpectedly verified")
		}
	}
}

func TestInstallRejectsParentSymlinkIntroducedAfterResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require privileges on some Windows environments")
	}
	root := t.TempDir()
	vendor := filepath.Join(root, "vendor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("payload")
	if err := os.WriteFile(filepath.Join(vendor, "tool"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "vendor/tool", "")
	if err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "tool"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(vendor, vendor+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, vendor); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err == nil {
		t.Fatal("Install() accepted parent symlink introduced after resolution")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after source revalidation failure: %v", err)
	}
}

func TestVerifyRegularFileStateDetectsPermissionDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not stable desired-state identity on Windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "tool")
	if err := os.WriteFile(path, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyRegularFileState(path, resolved.Artifact.Checksum, resolved.Mode); err != nil {
		t.Fatalf("initial state verification failed: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.VerifyRegularFileState(path, resolved.Artifact.Checksum, resolved.Mode); err == nil {
		t.Fatal("permission drift unexpectedly accepted")
	}
	// Checksum-only callers retain their documented weaker behavior.
	if err := localartifact.VerifyRegularFileChecksum(path, resolved.Artifact.Checksum); err != nil {
		t.Fatalf("checksum-only verification should ignore mode: %v", err)
	}
}
