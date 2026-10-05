package state

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPathFallsBackToPrivatePerUserTempRoot(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("OS resolves a home directory independently of HOME")
	}

	path := DefaultPath()
	rel, err := filepath.Rel(os.Getenv("TMPDIR"), path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "depengine-"+current.Uid+string(filepath.Separator)+"state"+string(filepath.Separator)+"depengine"+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)+".local"+string(filepath.Separator)+"state") {
		t.Fatalf("state fallback path is not isolated under a per-user temp root: %q", path)
	}
	root := filepath.Join(os.Getenv("TMPDIR"), strings.Split(rel, string(filepath.Separator))[0])
	info, err := os.Stat(root) // #nosec G703 -- root is the expected test-owned child beneath its isolated TMPDIR.
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("fallback root permissions = %04o, want owner-only", info.Mode().Perm())
	}
}
