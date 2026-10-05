//go:build unix

package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/privatepath"
)

func TestFallbackResolutionFailureDoesNotUseWorkingDirectory(t *testing.T) {
	tmp := t.TempDir()
	blockedTemp := filepath.Join(tmp, "not-a-directory")
	if err := os.WriteFile(blockedTemp, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("TMPDIR", blockedTemp)
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("OS resolves a home directory independently of HOME")
	}

	if got := DefaultPath(); got != "" {
		t.Fatalf("DefaultPath() = %q, want empty on unresolved fallback", got)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without a private fallback root")
	}
	if err := Save(&State{}); err == nil {
		t.Fatal("Save() succeeded without a private fallback root")
	}
	if _, err := LoadLocked(); err == nil {
		t.Fatal("LoadLocked() succeeded without a private fallback root")
	}
	if _, err := os.Stat("state.json"); !os.IsNotExist(err) {
		t.Fatalf("relative state file exists or could not be checked: %v", err)
	}
	if _, err := os.Stat("state.json.lock"); !os.IsNotExist(err) {
		t.Fatalf("relative lock file exists or could not be checked: %v", err)
	}

	t.Setenv("TMPDIR", ".")
	if got := privatepath.TempRootPath(); got != "" {
		t.Fatalf("TempRootPath() = %q for relative TMPDIR, want empty", got)
	}
	if got := DefaultPath(); got != "" {
		t.Fatalf("DefaultPath() = %q for relative TMPDIR, want empty", got)
	}
	root := filepath.Join(".", fmt.Sprintf("depengine-%d", os.Getuid()))
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("relative fallback root was created in working directory: %v", err)
	}
}
