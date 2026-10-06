package httpdownload

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestInstallArchiveRejectsUnownedExistingDestinationBeforeMutation(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "payload")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dest, "foreign")
	if err := os.WriteFile(sentinel, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{"extract_to": dest, "sudo_required": false}}

	err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, run.OSExecRunner{})
	if err == nil || !strings.Contains(err.Error(), "ownership marker is missing") {
		t.Fatalf("installArchive() error = %v, want missing ownership rejection", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "foreign" { // #nosec G304 -- sentinel is inside this test's temporary directory.
		t.Fatalf("unowned destination changed before rejection: %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(dest + ".depengine-backup"); !os.IsNotExist(statErr) {
		t.Fatalf("backup created for unowned destination: %v", statErr)
	}
}

func TestInstallArchiveRejectsForeignStaleBackupBeforeMutation(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "payload")
	backup := dest + ".depengine-backup"
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(backup, "foreign")
	if err := os.WriteFile(sentinel, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{"extract_to": dest, "sudo_required": false}}

	err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, run.OSExecRunner{})
	if err == nil || !strings.Contains(err.Error(), "backup") || !strings.Contains(err.Error(), "ownership marker is missing") {
		t.Fatalf("installArchive() error = %v, want stale backup ownership rejection", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "foreign" { // #nosec G304 -- sentinel is inside this test's temporary directory.
		t.Fatalf("foreign stale backup changed before rejection: %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("destination created despite stale foreign backup: %v", statErr)
	}
}

func TestInstallArchiveReplacesMatchingOwnedDestination(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "payload")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Kind: "http", Label: "stable", Config: map[string]any{"extract_to": dest, "sudo_required": false}}
	tool := &config.Tool{Name: "demo"}
	owner, err := expectedArchiveOwnership(tool, mc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveOwnership(dest, owner); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))

	if err := installArchive(context.Background(), archive, ".tar.gz", tool, mc, run.OSExecRunner{}); err != nil {
		t.Fatalf("installArchive() error = %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(dest, "bin", "demo")); readErr != nil || string(got) != "new" { // #nosec G304 -- dest is a test-owned temporary path.
		t.Fatalf("replacement payload = %q, %v; want new", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "old")); !os.IsNotExist(statErr) {
		t.Fatalf("old payload survived replacement: %v", statErr)
	}
	if err := verifyArchiveOwnership(dest, owner); err != nil {
		t.Fatalf("replacement ownership marker invalid: %v", err)
	}
}

func TestInstallArchiveRootProcessNormalizesSystemPayloadRootMode(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 {
		t.Skip("requires a Unix root test process")
	}
	root := t.TempDir()
	dest := filepath.Join(root, "payload")
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{"extract_to": dest, "sudo_required": true}}

	if err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, run.OSExecRunner{}); err != nil {
		t.Fatalf("installArchive() error = %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("root-owned committed payload mode = %04o, want 0755", info.Mode().Perm())
	}
}
