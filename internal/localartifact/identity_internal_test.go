package localartifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChecksumVerifiedRegularFileRejectsDifferentOpenedIdentity(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checksumVerifiedRegularFile(second, info); err == nil {
		t.Fatal("checksumVerifiedRegularFile accepted a different opened file identity")
	}
}

func TestInstallRawMaterializesAlreadyVerifiedOpenFile(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source")
	if err := os.WriteFile(sourcePath, []byte("verified"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	f, opened, err := openVerifiedRegularFile(sourcePath, info)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	oldPath := filepath.Join(dir, "old")
	if err := os.Rename(sourcePath, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}

	expected, err := checksumOpenFile(f)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "installed")
	if err := installRaw(f, opened, expected, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "verified" {
		t.Fatalf("installed bytes = %q, want already-verified open file", got)
	}
}

func TestSnapshotVerifiedSourceRejectsInPlaceMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.tar")
	original := []byte("original archive bytes")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := openVerifiedRegularFile(path, info)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	expected, err := checksumOpenFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("mutated archive bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, cleanup, err := snapshotVerifiedSource(f, expected, dir); err == nil {
		cleanup()
		t.Fatal("snapshotVerifiedSource accepted source mutated in place after checksum")
	}
}
