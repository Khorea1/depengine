package localartifact_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/pkg/localartifact"
)

func TestInstallRawOfflineAndAtomicReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "vendor", "tool")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "vendor/tool", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("installed content = %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("installed mode = %o, want executable", info.Mode().Perm())
		}
	}
}

func TestInstallZipRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bad.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("bad"))
	_ = zw.Close()
	_ = f.Close()
	resolved, err := localartifact.Resolve(root, "bad.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	destRoot := t.TempDir()
	dest := filepath.Join(destRoot, "payload")
	if err := localartifact.Install(resolved, dest); err == nil {
		t.Fatal("archive traversal unexpectedly installed")
	}
	if _, err := os.Stat(filepath.Join(destRoot, "escape")); !os.IsNotExist(err) {
		t.Fatalf("escape path exists or stat failed unexpectedly: %v", err)
	}
}

func TestInstallTarGzExtractsRegularFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("binary")
	if err := tw.WriteHeader(&tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.tar.gz", "")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "payload")
	if err := localartifact.Install(resolved, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("content = %q", got)
	}
}

func TestInstallRejectsArchiveBackendNotAvailableOffline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool.tar.xz")
	if err := os.WriteFile(path, []byte("not actually xz"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool.tar.xz", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := localartifact.Install(resolved, filepath.Join(t.TempDir(), "payload")); err == nil {
		t.Fatal("tar.xz unexpectedly installed without extraction backend")
	}
}

func TestInstallRejectsArtifactChangedAfterResolve(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool")
	if err := os.WriteFile(path, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := localartifact.Install(resolved, destination); err == nil {
		t.Fatal("Install() accepted artifact changed after resolution")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after checksum rejection: %v", err)
	}
}

func TestInstallPreservesUnrelatedLegacyBackupPath(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "tool")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := localartifact.Resolve(root, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(destination, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyBackup := destination + ".depengine-backup"
	if err := os.WriteFile(legacyBackup, []byte("owned by user"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := localartifact.Install(resolved, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(legacyBackup)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "owned by user" {
		t.Fatalf("legacy backup path changed: %q", got)
	}
}
