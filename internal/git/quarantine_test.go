package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
)

func quarantineMethod(extractTo, binary string) *config.MethodCandidate {
	cfg := map[string]any{"extract_to": extractTo}
	if binary != "" {
		cfg["binary"] = binary
	}
	return &config.MethodCandidate{Kind: "git", Config: cfg}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- test-owned temporary directory.
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
}

func requireQuarantine(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("replacement quarantine is disabled on Windows")
	}
}

func TestQuarantineSharedBinaryRestoresOnRollback(t *testing.T) {
	requireQuarantine(t)
	extractTo := filepath.Join(t.TempDir(), "bin")
	target := filepath.Join(extractTo, "depengine")
	writeFile(t, target, "old-binary")
	adapter := NewGitAdapter()
	commit, rollback, err := adapter.QuarantineForReplacement(context.Background(), nil, &config.Tool{Name: "depengine"}, quarantineMethod(extractTo, "depengine"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target still present after quarantine: %v", err)
	}
	if body, err := os.ReadFile(target + replacementBackupSuffix); err != nil || string(body) != "old-binary" { // #nosec G304 -- target is a test-owned path.
		t.Fatalf("backup = %q, %v", body, err)
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "old-binary" { // #nosec G304 -- target is a test-owned path.
		t.Fatalf("restored target = %q, %v", body, err)
	}
	if _, err := os.Stat(target + replacementBackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup survived rollback: %v", err)
	}
	_ = commit // rollback already closed the transaction
}

func TestQuarantineCommitRemovesBackupAfterInstall(t *testing.T) {
	requireQuarantine(t)
	extractTo := filepath.Join(t.TempDir(), "bin")
	target := filepath.Join(extractTo, "depengine")
	writeFile(t, target, "old-binary")
	adapter := NewGitAdapter()
	commit, _, err := adapter.QuarantineForReplacement(context.Background(), nil, &config.Tool{Name: "depengine"}, quarantineMethod(extractTo, "depengine"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, "new-binary")
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "new-binary" { // #nosec G304 -- target is a test-owned path.
		t.Fatalf("installed target = %q, %v", body, err)
	}
	if _, err := os.Stat(target + replacementBackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup survived commit: %v", err)
	}
}

func TestQuarantineDirectoryTarget(t *testing.T) {
	requireQuarantine(t)
	extractTo := filepath.Join(t.TempDir(), "depengine")
	writeFile(t, filepath.Join(extractTo, "depengine"), "old-binary")
	adapter := NewGitAdapter()
	_, rollback, err := adapter.QuarantineForReplacement(context.Background(), nil, &config.Tool{Name: "depengine"}, quarantineMethod(extractTo, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extractTo); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory still present after quarantine: %v", err)
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(extractTo, "depengine")); err != nil || string(body) != "old-binary" { // #nosec G304 -- fixture-controlled path; no untrusted runtime input crosses this test boundary.
		t.Fatalf("restored directory = %q, %v", body, err)
	}
}

func TestQuarantineManagedPathsRollBackPartially(t *testing.T) {
	requireQuarantine(t)
	root := t.TempDir()
	first := filepath.Join(root, "one")
	second := filepath.Join(root, "two")
	writeFile(t, first, "one")
	writeFile(t, second, "two")
	mc := &config.MethodCandidate{Kind: "git", Config: map[string]any{"managed_paths": []any{first, second}}}
	adapter := NewGitAdapter()
	_, rollback, err := adapter.QuarantineForReplacement(context.Background(), nil, &config.Tool{Name: "demo"}, mc)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still present after quarantine: %v", path, err)
		}
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{first: "one", second: "two"} {
		if body, err := os.ReadFile(path); err != nil || string(body) != want { // #nosec G304 -- fixture-controlled path; no untrusted runtime input crosses this test boundary.
			t.Fatalf("restored %s = %q, %v", path, body, err)
		}
	}
}

func TestQuarantineAdoptsOrphanBackup(t *testing.T) {
	requireQuarantine(t)
	extractTo := filepath.Join(t.TempDir(), "bin")
	target := filepath.Join(extractTo, "depengine")
	writeFile(t, target+replacementBackupSuffix, "old-binary")
	adapter := NewGitAdapter()
	commit, rollback, err := adapter.QuarantineForReplacement(context.Background(), nil, &config.Tool{Name: "depengine"}, quarantineMethod(extractTo, "depengine"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "old-binary" { // #nosec G304 -- target is a test-owned path.
		t.Fatalf("adopted backup = %q, %v", body, err)
	}
	_ = commit
}

func TestQuarantineUnsupportedWithoutTarget(t *testing.T) {
	adapter := NewGitAdapter()
	_, _, err := adapter.QuarantineForReplacement(context.Background(), nil, &config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: "git", Config: map[string]any{}})
	if !errors.Is(err, exec.ErrQuarantineUnsupported) {
		t.Fatalf("error = %v, want ErrQuarantineUnsupported", err)
	}
}

func TestRemoveClearsReplacementBackup(t *testing.T) {
	extractTo := filepath.Join(t.TempDir(), "bin")
	target := filepath.Join(extractTo, "depengine")
	writeFile(t, target, "binary")
	writeFile(t, target+replacementBackupSuffix, "old")
	adapter := NewGitAdapter()
	if err := adapter.Remove(context.Background(), nil, &config.Tool{Name: "depengine"}, quarantineMethod(extractTo, "depengine")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{target, target + replacementBackupSuffix} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived removal: %v", path, err)
		}
	}
}
